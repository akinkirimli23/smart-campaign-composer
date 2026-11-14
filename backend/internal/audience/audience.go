// Package audience counts the users a segment rule selects, per channel.
package audience

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// Reachability per channel: consent AND an address, both required, and the
// address not suppressed (DECISIONS 35). These fragments are the single
// definition of "can be sent on this channel"; the sender must use them too,
// so counts shown before approval match what is sent.
const (
	smsConsent      = "(u.sms_opt_in AND u.phone IS NOT NULL)"
	emailConsent    = "(u.email_opt_in AND u.email IS NOT NULL)"
	smsSuppressed   = "EXISTS (SELECT 1 FROM suppressions sp WHERE sp.user_id = u.id AND sp.channel = 'sms')"
	emailSuppressed = "EXISTS (SELECT 1 FROM suppressions sp WHERE sp.user_id = u.id AND sp.channel = 'email')"
	// The same, for one reason: only used to tell the marketer why.
	smsBecause   = "EXISTS (SELECT 1 FROM suppressions sp WHERE sp.user_id = u.id AND sp.channel = 'sms' AND sp.reason = '%s')"
	emailBecause = "EXISTS (SELECT 1 FROM suppressions sp WHERE sp.user_id = u.id AND sp.channel = 'email' AND sp.reason = '%s')"

	SMSReachable   = "(" + smsConsent + " AND NOT " + smsSuppressed + ")"
	EmailReachable = "(" + emailConsent + " AND NOT " + emailSuppressed + ")"
)

// Counts is the size of an audience. SMS and Email overlap; Unreachable users
// match the rule but cannot be contacted on either channel.
type Counts struct {
	Total       int `json:"total"`
	SMS         int `json:"sms"`
	Email       int `json:"email"`
	Unreachable int `json:"unreachable"`
	// Suppressed counts the messages that are left out only because the
	// address was found invalid earlier: the person consented and has an
	// address on that channel, but the provider refused it for good.
	Suppressed int `json:"suppressed"`
	// Unsubscribed counts the messages that are left out because the person
	// asked not to be contacted on that channel any more.
	Unsubscribed int `json:"unsubscribed"`
}

// Reasons a consenting person with an address is still not sent to.
const (
	ReasonInvalidAddress = "invalid_address"
	ReasonUnsubscribed   = "unsubscribed"
)

// leftOut counts, as one SQL expression, the messages a reason keeps back.
func leftOut(reason string) string {
	return " count(*) FILTER (WHERE " + smsConsent + " AND " + fmt.Sprintf(smsBecause, reason) + ")" +
		" + count(*) FILTER (WHERE " + emailConsent + " AND " + fmt.Sprintf(emailBecause, reason) + ")"
}

// LanguageCount is the part of an audience that reads one language. Only
// people who can be reached on at least one channel are counted: these are
// the people who will actually receive a text in that language.
type LanguageCount struct {
	Language  string `json:"language"`
	Reachable int    `json:"reachable"`
}

// Querier is the part of pgxpool.Pool the counter needs.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
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
		" count(*) FILTER (WHERE NOT " + SMSReachable + " AND NOT " + EmailReachable + ")," +
		leftOut(ReasonInvalidAddress) + "," +
		leftOut(ReasonUnsubscribed) +
		" FROM users u WHERE " + q.Where

	var n Counts
	if err := c.DB.QueryRow(ctx, sql, q.Args...).Scan(&n.Total, &n.SMS, &n.Email, &n.Unreachable, &n.Suppressed, &n.Unsubscribed); err != nil {
		return Counts{}, fmt.Errorf("count audience: %w", err)
	}
	return n, nil
}

// Languages reports which languages the reachable part of the audience reads,
// largest group first (ties in alphabetical order). The first entry is the
// campaign's main language; the others are the languages it is localised into
// (DECISIONS 30). Languages nobody reachable reads are not listed, so no text
// is written that nobody would receive.
func (c Counter) Languages(ctx context.Context, rule segment.Rule, asOf time.Time) ([]LanguageCount, error) {
	q, err := segment.Compile(rule, asOf)
	if err != nil {
		return nil, err
	}
	sql := "SELECT u.language, count(*) FROM users u WHERE (" + q.Where + ")" +
		" AND (" + SMSReachable + " OR " + EmailReachable + ")" +
		" GROUP BY u.language ORDER BY count(*) DESC, u.language"
	rows, err := c.DB.Query(ctx, sql, q.Args...)
	if err != nil {
		return nil, fmt.Errorf("audience languages: %w", err)
	}
	defer rows.Close()
	out := []LanguageCount{}
	for rows.Next() {
		var l LanguageCount
		if err := rows.Scan(&l.Language, &l.Reachable); err != nil {
			return nil, fmt.Errorf("audience languages: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
