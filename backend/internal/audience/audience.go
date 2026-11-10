// Package audience counts the users a segment rule selects, per channel.
package audience

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// Reachability per channel: consent AND an address, both required. These
// fragments are the single definition of "can be sent on this channel"; the
// sender must use them too, so counts shown before approval match what is sent.
const (
	SMSReachable   = "(u.sms_opt_in AND u.phone IS NOT NULL)"
	EmailReachable = "(u.email_opt_in AND u.email IS NOT NULL)"
)

// Counts is the size of an audience. SMS and Email overlap; Unreachable users
// match the rule but cannot be contacted on either channel.
type Counts struct {
	Total       int `json:"total"`
	SMS         int `json:"sms"`
	Email       int `json:"email"`
	Unreachable int `json:"unreachable"`
}

// Querier is the part of pgxpool.Pool the counter needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Counter struct{ DB Querier }

// Count evaluates the rule as of asOf. The rule is validated by Compile.
func (c Counter) Count(ctx context.Context, rule segment.Rule, asOf time.Time) (Counts, error) {
	q, err := segment.Compile(rule, asOf)
	if err != nil {
		return Counts{}, err
	}
	// Only package constants and the compiled WHERE (itself built from constants)
	// are concatenated here; every value is in q.Args.
	sql := "SELECT count(*)," +
		" count(*) FILTER (WHERE " + SMSReachable + ")," +
		" count(*) FILTER (WHERE " + EmailReachable + ")," +
		" count(*) FILTER (WHERE NOT " + SMSReachable + " AND NOT " + EmailReachable + ")" +
		" FROM users u WHERE " + q.Where

	var n Counts
	if err := c.DB.QueryRow(ctx, sql, q.Args...).Scan(&n.Total, &n.SMS, &n.Email, &n.Unreachable); err != nil {
		return Counts{}, fmt.Errorf("count audience: %w", err)
	}
	return n, nil
}
