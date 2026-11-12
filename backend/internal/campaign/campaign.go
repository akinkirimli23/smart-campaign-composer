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
	SendAt time.Time      `json:"send_at"`
	Queued map[string]int `json:"queued"` // messages per channel
}

// Create stores an approved campaign and enqueues one message per reachable
// recipient and channel, all in one transaction: either the whole audience is
// queued or nothing is.
//
// The recipients are frozen here, as of asOf, also for a campaign that is sent
// later (DECISIONS 27). Texts are not: the worker fills the templates at send
// time (DECISIONS 22). rule and c must already be validated; Compile validates
// the rule again.
//
// sendAt is when delivery may start; the zero value, or a time before asOf,
// means at once. Scheduling needs no scheduler: the queue already holds every
// message back until its next_attempt_at.
func (s Store) Create(ctx context.Context, brief string, rule segment.Rule, c content.Content, asOf, sendAt time.Time) (Created, error) {
	if sendAt.Before(asOf) {
		sendAt = asOf
	}
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

	created := Created{AsOf: asOf.UTC(), SendAt: sendAt.UTC(), Queued: map[string]int{}}
	err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO campaigns (brief, rule, content, as_of, send_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			brief, ruleJSON, contentJSON, created.AsOf, created.SendAt).Scan(&created.ID); err != nil {
			return fmt.Errorf("insert campaign: %w", err)
		}

		// The compiled WHERE uses $1..$n; the campaign id and the time follow.
		// Only package constants and the compiled fragment are concatenated.
		args := append(append([]any{}, q.Args...), created.ID, created.SendAt)
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
	// Cancelled: not sent because the campaign was cancelled first.
	Cancelled int `json:"cancelled"`
}

// Status is the delivery progress of a campaign.
type Status struct {
	ID        int64     `json:"id"`
	Brief     string    `json:"brief"`
	CreatedAt time.Time `json:"created_at"`
	// SendAt is when delivery starts (or started).
	SendAt time.Time `json:"send_at"`
	// CancelledAt is set once the campaign was cancelled.
	CancelledAt *time.Time        `json:"cancelled_at,omitempty"`
	Channels    map[string]Counts `json:"channels"`
	// Done: no message is waiting or in flight.
	Done bool `json:"done"`
}

func (s Store) Status(ctx context.Context, id int64) (Status, error) {
	st := Status{ID: id, Channels: map[string]Counts{}}
	err := s.DB.QueryRow(ctx, `SELECT brief, created_at, send_at, cancelled_at FROM campaigns WHERE id = $1`, id).
		Scan(&st.Brief, &st.CreatedAt, &st.SendAt, &st.CancelledAt)
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
		case "cancelled":
			c.Cancelled = n
		}
		st.Channels[channel] = c
	}
	return st, rows.Err()
}

// Cancel stops a campaign: every message still waiting becomes "cancelled"
// and will never be sent. Messages already sent stay sent; one that is with
// the provider right now finishes, and if it has to be retried the worker
// cancels it instead (see delivery.Worker). Cancelling twice is harmless.
func (s Store) Cancel(ctx context.Context, id int64, now time.Time) (Status, error) {
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE campaigns SET cancelled_at = COALESCE(cancelled_at, $2) WHERE id = $1`, id, now.UTC())
		if err != nil {
			return fmt.Errorf("cancel campaign: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// Only rows nobody holds. A row a worker has leased is left to that
		// worker, which sees cancelled_at when it records the outcome.
		if _, err := tx.Exec(ctx,
			`UPDATE messages SET status = 'cancelled', updated_at = $2 WHERE campaign_id = $1 AND status = 'pending'`,
			id, now.UTC()); err != nil {
			return fmt.Errorf("cancel messages: %w", err)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx, id)
}

// The states a campaign can be in, as shown in the campaign list.
const (
	StateScheduled = "scheduled" // approved, waiting for its send time
	StateSending   = "sending"   // delivery under way (or retrying)
	StateDone      = "done"      // every message is final
	StateCancelled = "cancelled" // cancelled by the marketer
)

// Summary is one row of the campaign list: a campaign with its message
// counts added up over both channels.
type Summary struct {
	ID          int64      `json:"id"`
	Brief       string     `json:"brief"`
	CreatedAt   time.Time  `json:"created_at"`
	SendAt      time.Time  `json:"send_at"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	State       string     `json:"state"`
	Messages    Counts     `json:"messages"`
}

// MaxListed bounds the campaign list. There is no paging: the list is for
// keeping an eye on recent and scheduled campaigns, not an archive.
const MaxListed = 100

// List returns the most recent campaigns, newest first, with their state as
// of now. The counts come from the same rows Status reads.
func (s Store) List(ctx context.Context, now time.Time) ([]Summary, error) {
	rows, err := s.DB.Query(ctx, `
SELECT c.id, c.brief, c.created_at, c.send_at, c.cancelled_at,
       count(m.id),
       count(*) FILTER (WHERE m.status = 'pending'),
       count(*) FILTER (WHERE m.status = 'sending'),
       count(*) FILTER (WHERE m.status = 'sent'),
       count(*) FILTER (WHERE m.status = 'failed'),
       count(*) FILTER (WHERE m.status = 'skipped'),
       count(*) FILTER (WHERE m.status = 'cancelled')
FROM campaigns c
LEFT JOIN messages m ON m.campaign_id = c.id
GROUP BY c.id
ORDER BY c.id DESC
LIMIT $1`, MaxListed)
	if err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	defer rows.Close()

	list := []Summary{}
	for rows.Next() {
		var c Summary
		m := &c.Messages
		if err := rows.Scan(&c.ID, &c.Brief, &c.CreatedAt, &c.SendAt, &c.CancelledAt,
			&m.Total, &m.Pending, &m.Sending, &m.Sent, &m.Failed, &m.Skipped, &m.Cancelled); err != nil {
			return nil, err
		}
		c.State = stateOf(c, now)
		list = append(list, c)
	}
	return list, rows.Err()
}

func stateOf(c Summary, now time.Time) string {
	m := c.Messages
	switch {
	case c.CancelledAt != nil:
		return StateCancelled
	case m.Pending+m.Sending == 0:
		return StateDone
	// Nothing has left the queue yet and the start time is still ahead.
	case c.SendAt.After(now) && m.Pending == m.Total:
		return StateScheduled
	default:
		return StateSending
	}
}

// Totals is the state of the whole queue, across campaigns.
type Totals struct {
	Campaigns int
	Messages  map[string]map[string]int // channel -> status -> count
}

// Statuses a message can be in, in lifecycle order.
var Statuses = []string{"pending", "sending", "sent", "failed", "skipped", "cancelled"}

// Totals counts every message by channel and status. It reads the same rows
// Status does, so per-campaign numbers add up to these.
func (s Store) Totals(ctx context.Context) (Totals, error) {
	t := Totals{Messages: map[string]map[string]int{}}
	for _, ch := range Channels {
		t.Messages[ch] = map[string]int{}
	}
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM campaigns`).Scan(&t.Campaigns); err != nil {
		return Totals{}, fmt.Errorf("count campaigns: %w", err)
	}
	rows, err := s.DB.Query(ctx, `SELECT channel, status, count(*) FROM messages GROUP BY channel, status`)
	if err != nil {
		return Totals{}, fmt.Errorf("count messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var channel, status string
		var n int
		if err := rows.Scan(&channel, &status, &n); err != nil {
			return Totals{}, err
		}
		if t.Messages[channel] == nil {
			t.Messages[channel] = map[string]int{}
		}
		t.Messages[channel][status] = n
	}
	return t, rows.Err()
}
