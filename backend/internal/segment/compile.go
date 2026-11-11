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

	var q Query
	arg := func(v any) string {
		q.Args = append(q.Args, v)
		return fmt.Sprintf("$%d", len(q.Args))
	}

	parts := make([]string, 0, len(r.Conditions))
	for _, c := range r.Conditions {
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
			case c.Exists && c.MinCount > 1:
				counted := strings.Replace(sub, "SELECT 1", "SELECT count(*)", 1)
				parts = append(parts, "("+counted+") >= "+arg(c.MinCount)+"::bigint")
			case c.Exists:
				parts = append(parts, "EXISTS ("+sub+")")
			default:
				parts = append(parts, "NOT EXISTS ("+sub+")")
			}
		case c.Type == TypeSignup:
			cutoff := asOf.AddDate(0, 0, -c.Days)
			if c.Op == OpWithin {
				parts = append(parts, "(u.signup_at >= "+arg(cutoff)+" AND u.signup_at <= "+arg(asOf)+")")
			} else {
				parts = append(parts, "u.signup_at < "+arg(cutoff))
			}
		case c.Type == TypeMetric:
			// Both metrics are aggregates over purchase events up to asOf; a user
			// without purchases has spent 0 in 0 orders.
			aggregate := "count(*)"
			if c.Metric == MetricTotalSpent {
				aggregate = "COALESCE(sum(e.amount), 0)"
			}
			sub := "SELECT " + aggregate + " FROM events e WHERE e.user_id = u.id" +
				" AND e.event_type = " + arg("purchase") +
				" AND e.occurred_at <= " + arg(asOf)
			if c.WithinDays > 0 {
				sub += " AND e.occurred_at >= " + arg(asOf.AddDate(0, 0, -c.WithinDays))
			}
			cmp := " >= "
			if c.Op == OpLTE {
				cmp = " <= "
			}
			parts = append(parts, "("+sub+")"+cmp+arg(c.Value)+"::bigint")
		case c.Field == FieldAge:
			// Age is approximated from the birth year: asOf year minus birth year.
			parts = append(parts, "u.birth_year BETWEEN "+arg(asOf.Year()-c.Max)+" AND "+arg(asOf.Year()-c.Min))
		default:
			column := setFields[c.Field].column // Validate guarantees the key exists
			if c.Op == OpIn {
				parts = append(parts, column+" = ANY("+arg(c.Values)+")")
			} else {
				parts = append(parts, column+" <> ALL("+arg(c.Values)+")")
			}
		}
	}

	joiner := " AND "
	if r.Match == MatchAny {
		joiner = " OR "
	}
	q.Where = "(" + strings.Join(parts, joiner) + ")"
	return q, nil
}
