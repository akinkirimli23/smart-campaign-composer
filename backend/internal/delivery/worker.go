package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/metrics"
)

// Waiter is the rate limiter of one channel.
type Waiter interface {
	Wait(ctx context.Context) (time.Time, error)
	PauseUntil(t time.Time)
}

// DB is the part of pgxpool.Pool the worker needs.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Worker drains the messages table. Retry policy (DECISIONS 21):
//
//   - network errors, timeouts and 5xx are retried with exponential backoff and
//     jitter, MaxAttempts sends in total, then the message is failed;
//   - 429 pauses the whole channel for Retry-After and does not use up an attempt;
//   - other 4xx fail at once: the same request would be rejected again;
//   - every attempt carries the same idempotency key.
//
// A claimed row is leased, not locked: no transaction is held open across the
// HTTP call. If the process dies, the lease expires and another worker picks
// the row up.
type Worker struct {
	DB       DB
	Provider Provider
	// Limiters holds one limiter per channel; the keys are the channels served.
	Limiters map[string]Waiter

	MaxAttempts  int           // default 5
	BatchSize    int           // rows claimed per query, default 50
	Concurrency  int           // in-flight sends per channel, default 32
	Lease        time.Duration // default 1 minute
	SendTimeout  time.Duration // default 5 seconds
	PollInterval time.Duration // idle wait between claims, default 500ms
	// Backoff returns the wait before the next attempt, given the number of
	// attempts made so far. Default: DefaultBackoff.
	Backoff func(attempts int) time.Duration
	Now     func() time.Time
	// Metrics counts provider requests by outcome; nil disables it.
	Metrics *metrics.Registry
}

// ProviderRequests is the counter of calls to the provider, labelled by
// channel and outcome (ok, error, rate_limited, rejected).
const ProviderRequests = "scc_provider_requests_total"

const (
	defaultRetryAfter = 2 * time.Second // 429 without a usable Retry-After
	maxRetryAfter     = time.Minute     // never let a provider park the queue for longer
)

// DefaultBackoff is exponential (1s, 2s, 4s, ... capped at 1 minute) with
// jitter: between half and all of the step, so retries of messages that
// failed together do not come back together.
func DefaultBackoff(attempts int) time.Duration {
	step := time.Second << min(max(attempts-1, 0), 6)
	if step > time.Minute {
		step = time.Minute
	}
	return step/2 + rand.N(step/2+1)
}

func (w *Worker) defaults() {
	if w.MaxAttempts <= 0 {
		w.MaxAttempts = 5
	}
	if w.BatchSize <= 0 {
		w.BatchSize = 50
	}
	if w.Concurrency <= 0 {
		w.Concurrency = 32
	}
	if w.Lease <= 0 {
		w.Lease = time.Minute
	}
	if w.SendTimeout <= 0 {
		w.SendTimeout = 5 * time.Second
	}
	if w.PollInterval <= 0 {
		w.PollInterval = 500 * time.Millisecond
	}
	if w.Backoff == nil {
		w.Backoff = DefaultBackoff
	}
	if w.Now == nil {
		w.Now = time.Now
	}
}

// Run serves every channel until ctx is cancelled, then waits for in-flight
// sends to be recorded.
func (w *Worker) Run(ctx context.Context) {
	w.defaults()
	var wg sync.WaitGroup
	for channel, limiter := range w.Limiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.runChannel(ctx, channel, limiter)
		}()
	}
	wg.Wait()
}

func (w *Worker) runChannel(ctx context.Context, channel string, limiter Waiter) {
	var inflight sync.WaitGroup
	defer inflight.Wait()
	slots := make(chan struct{}, w.Concurrency)

	for ctx.Err() == nil {
		batch, err := w.claim(ctx, channel)
		if err != nil && ctx.Err() == nil {
			slog.Error("claim messages", "channel", channel, "err", err)
		}
		if len(batch) == 0 {
			select {
			case <-ctx.Done():
			case <-time.After(w.PollInterval):
			}
			continue
		}
		for _, m := range batch {
			// Take a send slot before the rate-limit slot: a message that waits
			// for a free sender must still be held back by a later pause (429).
			slots <- struct{}{}
			if _, err := limiter.Wait(ctx); err != nil {
				<-slots
				return // shutting down; unsent rows are reclaimed when their lease expires
			}
			// A long pause may have eaten the lease. Sending now could overlap
			// with whoever reclaims the row, so leave it to them.
			if w.Now().Add(w.SendTimeout).After(m.leaseUntil) {
				<-slots
				continue
			}
			inflight.Add(1)
			go func() {
				defer func() { <-slots; inflight.Done() }()
				w.deliver(ctx, limiter, m)
			}()
		}
	}
}

// claimed is a leased message together with what is needed to send it.
type claimed struct {
	id, campaignID, userID int64
	channel                string
	attempts               int
	leaseUntil             time.Time
	firstName, lastName    string
	city                   string
	email, phone           *string
	reachable              bool
	cancelled              bool // the campaign was cancelled
	content                content.Content
}

// claim leases up to BatchSize due messages of a channel: pending ones whose
// time has come and "sending" ones whose lease ran out (their worker died).
// SKIP LOCKED lets several workers claim at once without waiting on, or
// double-claiming, each other's rows. Reachability is re-evaluated here, at
// send time, with the same definition the audience count uses.
func (w *Worker) claim(ctx context.Context, channel string) ([]claimed, error) {
	now := w.Now().UTC()
	lease := now.Add(w.Lease)
	reachable := audience.SMSReachable
	if channel == "email" {
		reachable = audience.EmailReachable
	}
	rows, err := w.DB.Query(ctx, `
WITH due AS (
    SELECT id FROM messages
    WHERE channel = $1
      AND ((status = 'pending' AND next_attempt_at <= $2) OR (status = 'sending' AND locked_until < $2))
    ORDER BY next_attempt_at
    LIMIT $3
    FOR UPDATE SKIP LOCKED
), leased AS (
    UPDATE messages m
    SET status = 'sending', attempts = m.attempts + 1, locked_until = $4, updated_at = $2
    FROM due WHERE m.id = due.id
    RETURNING m.id, m.campaign_id, m.user_id, m.attempts
)
SELECT l.id, l.campaign_id, l.user_id, l.attempts,
       u.first_name, u.last_name, u.city, u.email, u.phone, `+reachable+`, c.content,
       c.cancelled_at IS NOT NULL
FROM leased l
JOIN users u ON u.id = l.user_id
JOIN campaigns c ON c.id = l.campaign_id
ORDER BY l.id`, channel, now, w.BatchSize, lease)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var batch []claimed
	for rows.Next() {
		m := claimed{channel: channel, leaseUntil: lease}
		var raw []byte
		if err := rows.Scan(&m.id, &m.campaignID, &m.userID, &m.attempts,
			&m.firstName, &m.lastName, &m.city, &m.email, &m.phone, &m.reachable, &raw, &m.cancelled); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &m.content); err != nil {
			return nil, fmt.Errorf("campaign %d content: %w", m.campaignID, err)
		}
		batch = append(batch, m)
	}
	return batch, rows.Err()
}

// outbound fills the campaign's templates for this recipient.
func (m claimed) outbound() (Outbound, error) {
	values := map[string]string{"first_name": m.firstName, "last_name": m.lastName, "city": m.city}
	out := Outbound{
		// Stable across attempts and across workers: the provider sees one
		// logical message however many times we have to send it.
		IdempotencyKey: fmt.Sprintf("campaign-%d-user-%d-%s", m.campaignID, m.userID, m.channel),
		Channel:        m.channel,
	}
	var err error
	if m.channel == "sms" {
		out.To = *m.phone
		if out.Body, err = content.Render(m.content.SMS, values); err != nil {
			return Outbound{}, err
		}
		// Validation measured the worst case over the users that existed then
		// (DECISIONS 19). A longer name added since must not slip through.
		if n := utf8.RuneCountInString(out.Body); n > content.MaxSMS {
			return Outbound{}, fmt.Errorf("sms is %d characters after personalisation, limit %d", n, content.MaxSMS)
		}
		return out, nil
	}
	out.To = *m.email
	if out.Subject, err = content.Render(m.content.Email.Subject, values); err != nil {
		return Outbound{}, err
	}
	if n := utf8.RuneCountInString(out.Subject); n > content.MaxEmailSubject {
		return Outbound{}, fmt.Errorf("subject is %d characters after personalisation, limit %d", n, content.MaxEmailSubject)
	}
	out.Body, err = content.Render(m.content.Email.Body, values)
	return out, err
}

func (w *Worker) deliver(ctx context.Context, limiter Waiter, m claimed) {
	if m.attempts > w.MaxAttempts {
		// Only reachable through repeated lease expiry (a worker that keeps dying).
		w.finish(m, "failed", "", "gave up: lease expired repeatedly")
		return
	}
	if m.cancelled {
		// Normally Cancel has already moved waiting rows; this catches a row
		// reclaimed from a dead worker after the campaign was cancelled.
		w.finish(m, "cancelled", "", "")
		return
	}
	if !m.reachable {
		// Consent or address was withdrawn after approval.
		w.finish(m, "skipped", "", "recipient no longer reachable on this channel")
		return
	}
	out, err := m.outbound()
	if err != nil {
		w.finish(m, "failed", "", err.Error())
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, w.SendTimeout)
	providerID, err := w.Provider.Send(sendCtx, out)
	cancel()

	var sendErr *SendError
	count := func(outcome string) { w.Metrics.Inc(ProviderRequests, "channel", m.channel, "outcome", outcome) }
	switch {
	case err == nil:
		count("ok")
		w.finish(m, "sent", providerID, "")
	case ctx.Err() != nil:
		// We are shutting down and the send was cut short. Give the attempt back.
		w.retry(m, w.Now(), m.attempts-1, "interrupted by shutdown")
	case errors.As(err, &sendErr) && sendErr.Kind == Permanent:
		count("rejected")
		w.finish(m, "failed", "", err.Error())
	case errors.As(err, &sendErr) && sendErr.Kind == RateLimited:
		count("rate_limited")
		wait := sendErr.RetryAfter
		if wait <= 0 {
			wait = defaultRetryAfter
		}
		wait = min(wait, maxRetryAfter)
		until := w.Now().Add(wait)
		limiter.PauseUntil(until) // the provider's limit is per channel, not per message
		w.retry(m, until, m.attempts-1, err.Error())
	case m.attempts >= w.MaxAttempts:
		count("error")
		w.finish(m, "failed", "", fmt.Sprintf("gave up after %d attempts: %v", m.attempts, err))
	default:
		count("error")
		w.retry(m, w.Now().Add(w.Backoff(m.attempts)), m.attempts, err.Error())
	}
}

// finish records a final state. The WHERE clause is a fence: if the lease was
// lost and the row re-claimed (attempts moved on), this stale result is dropped.
func (w *Worker) finish(m claimed, status, providerID, lastError string) {
	now := w.Now().UTC()
	var sentAt *time.Time
	if status == "sent" {
		sentAt = &now
	}
	w.update(m, `
UPDATE messages
SET status = $3, provider_message_id = NULLIF($4, ''), last_error = NULLIF($5, ''),
    sent_at = $6, locked_until = NULL, updated_at = $7
WHERE id = $1 AND status = 'sending' AND attempts = $2`,
		m.id, m.attempts, status, providerID, lastError, sentAt, now)
}

// retry puts the message back in the queue for a later attempt, unless the
// campaign was cancelled while this attempt was in flight: then there is no
// later attempt and the message ends as cancelled.
func (w *Worker) retry(m claimed, at time.Time, attempts int, lastError string) {
	w.update(m, `
UPDATE messages
SET status = CASE WHEN (SELECT cancelled_at FROM campaigns WHERE id = messages.campaign_id) IS NULL
                  THEN 'pending' ELSE 'cancelled' END,
    attempts = $3, next_attempt_at = $4, last_error = $5,
    locked_until = NULL, updated_at = $6
WHERE id = $1 AND status = 'sending' AND attempts = $2`,
		m.id, m.attempts, attempts, at.UTC(), lastError, w.Now().UTC())
}

func (w *Worker) update(m claimed, sql string, args ...any) {
	// Not the worker's context: the outcome of a send must be recorded even
	// when shutdown has started, or the message would be sent again later.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := w.DB.Exec(ctx, sql, args...); err != nil {
		slog.Error("record send result", "message", m.id, "err", err)
	}
}
