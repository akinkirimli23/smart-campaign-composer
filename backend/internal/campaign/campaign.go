// Package campaign stores approved campaigns and enqueues their messages.
package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// Channels a campaign is sent on.
const (
	ChannelSMS   = "sms"
	ChannelEmail = "email"
)

var Channels = []string{ChannelSMS, ChannelEmail}

var (
	// ErrEmptyAudience: the rule selects nobody who can be reached.
	ErrEmptyAudience = errors.New("campaign: audience is empty")
	ErrNotFound      = errors.New("campaign: not found")
)

// DB is the part of pgxpool.Pool the store needs.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct{ DB DB }

// Created reports what Create enqueued.
type Created struct {
	ID     int64          `json:"id"`
	AsOf   time.Time      `json:"as_of"`
	Queued map[string]int `json:"queued"` // messages per channel
}

// Create stores an approved campaign and enqueues one message per reachable
// recipient and channel, all in one transaction: either the whole audience is
// queued or nothing is.
//
// The recipients are frozen here, as of asOf. Texts are not: the worker fills
// the templates at send time (DECISIONS 22). rule and c must already be
// validated; Compile validates the rule again.
func (s Store) Create(ctx context.Context, brief string, rule segment.Rule, c content.Content, asOf time.Time) (Created, error) {
	q, err := segment.Compile(rule, asOf)
	if err != nil {
		return Created{}, err
	}
	ruleJSON, err := json.Marshal(rule)
	if err != nil {
		return Created{}, err
	}
	contentJSON, err := json.Marshal(c)
	if err != nil {
		return Created{}, err
	}

	created := Created{AsOf: asOf.UTC(), Queued: map[string]int{}}
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO campaigns (brief, rule, content, as_of) VALUES ($1, $2, $3, $4) RETURNING id`,
			brief, ruleJSON, contentJSON, created.AsOf).Scan(&created.ID); err != nil {
			return fmt.Errorf("insert campaign: %w", err)
		}

		// The compiled WHERE uses $1..$n; the campaign id and the time follow.
		// Only package constants and the compiled fragment are concatenated.
		args := append(append([]any{}, q.Args...), created.ID, created.AsOf)
		idArg, nowArg := fmt.Sprintf("$%d", len(q.Args)+1), fmt.Sprintf("$%d", len(q.Args)+2)
		total := 0
		for channel, reachable := range map[string]string{
			ChannelSMS:   audience.SMSReachable,
			ChannelEmail: audience.EmailReachable,
		} {
			tag, err := tx.Exec(ctx,
				"INSERT INTO messages (campaign_id, user_id, channel, next_attempt_at)"+
					" SELECT "+idArg+"::bigint, u.id, '"+channel+"', "+nowArg+"::timestamptz"+
					" FROM users u WHERE "+q.Where+" AND "+reachable, args...)
			if err != nil {
				return fmt.Errorf("enqueue %s: %w", channel, err)
			}
			created.Queued[channel] = int(tag.RowsAffected())
			total += created.Queued[channel]
		}
		if total == 0 {
			return ErrEmptyAudience // rolls the campaign row back
		}
		return nil
	})
	if err != nil {
		return Created{}, err
	}
	return created, nil
}

// Counts is the number of messages of one channel per status.
type Counts struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
	Sending int `json:"sending"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// Status is the delivery progress of a campaign.
type Status struct {
	ID        int64             `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Channels  map[string]Counts `json:"channels"`
	// Done: no message is waiting or in flight.
	Done bool `json:"done"`
}

func (s Store) Status(ctx context.Context, id int64) (Status, error) {
	st := Status{ID: id, Channels: map[string]Counts{}}
	err := s.DB.QueryRow(ctx, `SELECT created_at FROM campaigns WHERE id = $1`, id).Scan(&st.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, ErrNotFound
	}
	if err != nil {
		return Status{}, fmt.Errorf("load campaign: %w", err)
	}
	for _, ch := range Channels {
		st.Channels[ch] = Counts{}
	}

	rows, err := s.DB.Query(ctx,
		`SELECT channel, status, count(*) FROM messages WHERE campaign_id = $1 GROUP BY channel, status`, id)
	if err != nil {
		return Status{}, fmt.Errorf("count messages: %w", err)
	}
	defer rows.Close()
	st.Done = true
	for rows.Next() {
		var channel, status string
		var n int
		if err := rows.Scan(&channel, &status, &n); err != nil {
			return Status{}, err
		}
		c := st.Channels[channel]
		c.Total += n
		switch status {
		case "pending":
			c.Pending = n
			st.Done = false
		case "sending":
			c.Sending = n
			st.Done = false
		case "sent":
			c.Sent = n
		case "failed":
			c.Failed = n
		case "skipped":
			c.Skipped = n
		}
		st.Channels[channel] = c
	}
	return st, rows.Err()
}
