package segment

import (
	"fmt"
	"strings"
	"time"
)

// Query is a WHERE fragment over "users u" and its bind parameters.
type Query struct {
	Where string
	Args  []any
}

// Compile turns a rule into parameterised SQL evaluated as of asOf.
//
// The SQL text is assembled only from constants in this package: column names
// come from setFields, operators from a switch. Every value from the rule,
// including event names and the time window, is a bind parameter. Time is a
// parameter too (no now() in SQL), so the same rule and asOf always select the
// same users and tests can pin the clock.
func Compile(r Rule, asOf time.Time) (Query, error) {
	if err := r.Validate(); err != nil {
		return Query{}, err
	}
	asOf = asOf.UTC()
	if r.Everyone {
		// No condition, no parameter. Who can actually be sent to is decided
		// by the caller's reachability clause, as for every rule.
		return Query{Where: "TRUE"}, nil
	}

	var q Query
	arg := func(v any) string {
		q.Args = append(q.Args, v)
		return fmt.Sprintf("$%d", len(q.Args))
	}

	q.Where = "(" + join(r.Conditions, r.Match, func(c Condition) string {
		if c.Type == TypeGroup {
			// One level only: Validate has refused a group inside a group.
			return "(" + join(c.Conditions, c.Match, func(member Condition) string { return compileLeaf(member, asOf, arg) }) + ")"
		}
		return compileLeaf(c, asOf, arg)
	}) + ")"
	return q, nil
}

// join renders conditions and joins them with the SQL for match.
func join(conditions []Condition, match Match, render func(Condition) string) string {
	parts := make([]string, len(conditions))
	for i, c := range conditions {
		parts[i] = render(c)
	}
	if match == MatchAny {
		return strings.Join(parts, " OR ")
	}
	return strings.Join(parts, " AND ")
}

// compileLeaf turns one plain condition into a boolean SQL expression over
// "users u". arg registers a bind parameter and returns its placeholder.
func compileLeaf(c Condition, asOf time.Time, arg func(any) string) string {
	switch {
	case c.Type == TypeEvent:
		sub := "SELECT 1 FROM events e WHERE e.user_id = u.id" +
			" AND e.event_type = " + arg(c.Event) +
			" AND e.occurred_at >= " + arg(asOf.AddDate(0, 0, -c.WithinDays)) +
			" AND e.occurred_at <= " + arg(asOf)
		if c.Category != "" {
			sub += " AND e.category = " + arg(c.Category)
		}
		switch {
		case c.Exists && c.MaxCount > 0:
			// "At most N times" still means it happened: one to N, or MinCount to N.
			counted := strings.Replace(sub, "SELECT 1", "SELECT count(*)", 1)
			return "(" + counted + ") BETWEEN " + arg(max(c.MinCount, 1)) + "::bigint AND " + arg(c.MaxCount) + "::bigint"
		case c.Exists && c.MinCount > 1:
			counted := strings.Replace(sub, "SELECT 1", "SELECT count(*)", 1)
			return "(" + counted + ") >= " + arg(c.MinCount) + "::bigint"
		case c.Exists:
			return "EXISTS (" + sub + ")"
		default:
			return "NOT EXISTS (" + sub + ")"
		}
	case c.Type == TypeSignup:
		cutoff := asOf.AddDate(0, 0, -c.Days)
		if c.Op == OpWithin {
			return "(u.signup_at >= " + arg(cutoff) + " AND u.signup_at <= " + arg(asOf) + ")"
		} else {
			return "u.signup_at < " + arg(cutoff)
		}
	case c.Type == TypeMetric:
		// Metrics are aggregates over purchase events up to asOf; a user
		// without purchases has spent 0 in 0 orders and has no average.
		aggregate := "count(*)"
		switch c.Metric {
		case MetricTotalSpent:
			aggregate = "COALESCE(sum(e.amount), 0)"
		case MetricAvgOrderValue:
			aggregate = "avg(e.amount)"
		}
		sub := "SELECT " + aggregate + " FROM events e WHERE e.user_id = u.id" +
			" AND e.event_type = " + arg("purchase") +
			" AND e.occurred_at <= " + arg(asOf)
		if c.WithinDays > 0 {
			sub += " AND e.occurred_at >= " + arg(asOf.AddDate(0, 0, -c.WithinDays))
		}
		if c.Category != "" {
			sub += " AND e.category = " + arg(c.Category)
		}
		cmp := " >= "
		if c.Op == OpLTE {
			cmp = " <= "
		}
		expr := "(" + sub + ")" + cmp + arg(c.Value) + "::bigint"
		if c.Metric == MetricAvgOrderValue {
			// No purchases, no average: NULL must read as "does not match", also
			// inside an OR.
			return "COALESCE(" + expr + ", false)"
		}
		return expr
	case c.Field == FieldAge:
		// Age is approximated from the birth year: asOf year minus birth year.
		return "u.birth_year BETWEEN " + arg(asOf.Year()-c.Max) + " AND " + arg(asOf.Year()-c.Min)
	default:
		column := setFields[c.Field].column // Validate guarantees the key exists
		if c.Op == OpIn {
			return column + " = ANY(" + arg(c.Values) + ")"
		} else {
			return column + " <> ALL(" + arg(c.Values) + ")"
		}
	}
}
