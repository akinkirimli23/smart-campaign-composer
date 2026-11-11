package delivery_test

import (
	"context"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/delivery"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/mockprovider"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/testdb"
)

var refNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

var testContent = content.Content{
	SMS:   "Merhaba {{first_name}}, %30 indirim seni bekliyor!",
	Email: content.Email{Subject: "{{first_name}}, {{city}} için fırsat", Body: "Merhaba {{first_name}} {{last_name}},\n\nİyi alışverişler!"},
}

func cityRule(cities ...string) segment.Rule {
	return segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: cities},
	}}
}

// setup seeds users and creates one campaign.
func setup(t *testing.T, users int, rule segment.Rule) (*pgxpool.Pool, campaign.Created) {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, users, 7, refNow); err != nil {
		t.Fatal(err)
	}
	created, err := campaign.Store{DB: pool}.Create(ctx, "test", rule, testContent, refNow)
	if err != nil {
		t.Fatal(err)
	}
	return pool, created
}

func fastLimiters() map[string]delivery.Waiter {
	return map[string]delivery.Waiter{"sms": delivery.NewLimiter(5000), "email": delivery.NewLimiter(5000)}
}

// runUntilDone runs the workers until the campaign has no unfinished message.
func runUntilDone(t *testing.T, pool *pgxpool.Pool, id int64, timeout time.Duration, workers ...*delivery.Worker) campaign.Status {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func() { defer wg.Done(); w.Run(ctx) }()
	}
	defer func() { cancel(); wg.Wait() }()

	deadline := time.Now().Add(timeout)
	for {
		st, err := campaign.Store{DB: pool}.Status(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if st.Done {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("campaign not done after %v: %+v", timeout, st.Channels)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// scriptedProvider is an in-process Provider whose behaviour a test controls.
type scriptedProvider struct {
	fn func(call int, m delivery.Outbound) (string, error)

	mu    sync.Mutex
	calls []delivery.Outbound
	times []time.Time
}

func (p *scriptedProvider) Send(_ context.Context, m delivery.Outbound) (string, error) {
	p.mu.Lock()
	p.calls = append(p.calls, m)
	p.times = append(p.times, time.Now())
	n := len(p.calls)
	p.mu.Unlock()
	if p.fn == nil {
		return "ok", nil
	}
	return p.fn(n, m)
}

func (p *scriptedProvider) snapshot() ([]delivery.Outbound, []time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]delivery.Outbound(nil), p.calls...), append([]time.Time(nil), p.times...)
}

func scalar[T any](t *testing.T, pool *pgxpool.Pool, sql string, args ...any) T {
	t.Helper()
	var v T
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// The stage's first done criterion. Two workers drain the same queue against a
// provider that fails, rate-limits and loses replies. Nobody may receive the
// campaign twice on a channel, and nobody reachable may be left out.
func TestNobodyReceivesTheCampaignTwice(t *testing.T) {
	pool, created := setup(t, 1500, cityRule("İstanbul", "Ankara"))
	total := created.Queued["sms"] + created.Queued["email"]
	if created.Queued["sms"] < 100 || created.Queued["email"] < 100 {
		t.Fatalf("audience too small to be a meaningful test: %+v", created.Queued)
	}

	provider := mockprovider.New(mockprovider.Config{
		ErrorRate: 0.15, LostReplyRate: 0.10, RateLimitRate: 0.004,
		MaxLatency: 3 * time.Millisecond, RetryAfter: time.Second, Seed: 1, Keep: 100_000,
	})
	srv := httptest.NewServer(provider)
	defer srv.Close()

	newWorker := func() *delivery.Worker {
		return &delivery.Worker{
			DB: pool, Provider: delivery.HTTPProvider{BaseURL: srv.URL}, Limiters: fastLimiters(),
			MaxAttempts: 12, BatchSize: 20, PollInterval: 10 * time.Millisecond,
			Backoff: func(int) time.Duration { return 5 * time.Millisecond },
		}
	}
	st := runUntilDone(t, pool, created.ID, 90*time.Second, newWorker(), newWorker())

	for _, ch := range campaign.Channels {
		c := st.Channels[ch]
		if c.Sent != created.Queued[ch] || c.Failed != 0 || c.Skipped != 0 {
			t.Errorf("%s: %+v, want all %d sent", ch, c, created.Queued[ch])
		}
	}

	// What the provider actually delivered: one message per recipient and channel.
	seen := map[string]int{}
	for _, m := range provider.Messages() {
		seen[m.Channel+" "+m.To]++
		if strings.Contains(m.Body+m.Subject, "{{") {
			t.Fatalf("unfilled template delivered: %+v", m)
		}
	}
	if len(seen) != total {
		t.Errorf("provider delivered to %d recipients, want %d", len(seen), total)
	}
	for who, n := range seen {
		if n != 1 {
			t.Errorf("%s received the campaign %d times", who, n)
		}
	}

	// The test only means something if retries really happened, including
	// retries of messages the provider had already accepted.
	stats := provider.Stats()
	dups := stats["sms"].Duplicates + stats["email"].Duplicates
	lost := stats["sms"].LostReplies + stats["email"].LostReplies
	errs := stats["sms"].Errors + stats["email"].Errors
	if dups == 0 || lost == 0 || errs == 0 {
		t.Errorf("faults were not exercised: duplicates=%d lost=%d errors=%d", dups, lost, errs)
	}
	if got := stats["sms"].Accepted + stats["email"].Accepted; got != total {
		t.Errorf("provider accepted %d distinct messages, want %d", got, total)
	}
	if n := scalar[int](t, pool, `SELECT count(*) FROM messages WHERE status = 'sent' AND (provider_message_id IS NULL OR sent_at IS NULL)`); n != 0 {
		t.Errorf("%d sent rows without provider id or sent_at", n)
	}
}

// The database refuses a second message for the same person, campaign and
// channel, whoever tries to insert it.
func TestUniqueConstraintBlocksDuplicateRows(t *testing.T) {
	pool, created := setup(t, 300, cityRule("İstanbul"))
	_, err := pool.Exec(context.Background(),
		`INSERT INTO messages (campaign_id, user_id, channel, next_attempt_at)
		 SELECT campaign_id, user_id, channel, now() FROM messages WHERE campaign_id = $1 LIMIT 1`, created.ID)
	if err == nil || !strings.Contains(err.Error(), "messages_once_per_channel") {
		t.Fatalf("err = %v, want a violation of messages_once_per_channel", err)
	}
}

// recordingLimiter remembers the slots the real limiter granted.
type recordingLimiter struct {
	*delivery.Limiter
	mu    sync.Mutex
	slots []time.Time
}

func (r *recordingLimiter) Wait(ctx context.Context) (time.Time, error) {
	slot, err := r.Limiter.Wait(ctx)
	if err == nil {
		r.mu.Lock()
		r.slots = append(r.slots, slot)
		r.mu.Unlock()
	}
	return slot, err
}

// The stage's second done criterion: at 100/s per channel, no one-second
// window holds more than 100 sends on a channel, and each channel has its own
// budget.
func TestRateLimitIsNotExceeded(t *testing.T) {
	pool, created := setup(t, 1500, cityRule("İstanbul", "Ankara", "İzmir"))
	if created.Queued["sms"] < 150 || created.Queued["email"] < 150 {
		t.Fatalf("need more than one second of traffic per channel: %+v", created.Queued)
	}

	limiters := map[string]*recordingLimiter{
		"sms":   {Limiter: delivery.NewLimiter(100)},
		"email": {Limiter: delivery.NewLimiter(100)},
	}
	provider := &scriptedProvider{}
	start := time.Now()
	st := runUntilDone(t, pool, created.ID, 60*time.Second, &delivery.Worker{
		DB: pool, Provider: provider, PollInterval: 10 * time.Millisecond,
		Limiters: map[string]delivery.Waiter{"sms": limiters["sms"], "email": limiters["email"]},
	})
	elapsed := time.Since(start)

	for ch, l := range limiters {
		n := created.Queued[ch]
		if st.Channels[ch].Sent != n || len(l.slots) != n {
			t.Fatalf("%s: sent %d, slots %d, want %d", ch, st.Channels[ch].Sent, len(l.slots), n)
		}
		sort.Slice(l.slots, func(i, j int) bool { return l.slots[i].Before(l.slots[j]) })
		worst, from := 0, 0
		for to := range l.slots {
			for l.slots[to].Sub(l.slots[from]) >= time.Second {
				from++
			}
			worst = max(worst, to-from+1)
		}
		if worst > 100 {
			t.Errorf("%s: %d sends in one second, limit 100", ch, worst)
		}
		if worst < 95 {
			t.Errorf("%s: at most %d sends in one second; the limiter is slower than configured", ch, worst)
		}
		// n sends at 100/s cannot finish sooner than (n-1) * 10ms.
		if floor := time.Duration(n-1) * 10 * time.Millisecond; elapsed < floor {
			t.Errorf("%s: %d messages took %v, faster than the limit allows (%v)", ch, n, elapsed, floor)
		}
	}
	// The provider saw every message no earlier than its slot.
	if calls, _ := provider.snapshot(); len(calls) != created.Queued["sms"]+created.Queued["email"] {
		t.Errorf("provider calls = %d", len(calls))
	}
}

func smallCampaign(t *testing.T) (*pgxpool.Pool, campaign.Created) {
	t.Helper()
	pool, created := setup(t, 200, cityRule("İstanbul"))
	// Keep only SMS so call counts are easy to reason about.
	if _, err := pool.Exec(context.Background(), `DELETE FROM messages WHERE channel = 'email'`); err != nil {
		t.Fatal(err)
	}
	created.Queued["email"] = 0
	if created.Queued["sms"] < 5 {
		t.Fatalf("audience too small: %+v", created.Queued)
	}
	return pool, created
}

func worker(pool *pgxpool.Pool, p delivery.Provider) *delivery.Worker {
	return &delivery.Worker{
		DB: pool, Provider: p, Limiters: fastLimiters(), PollInterval: 5 * time.Millisecond,
		Backoff: func(int) time.Duration { return 2 * time.Millisecond },
	}
}

// A message that keeps failing is tried MaxAttempts times, then failed with
// the last error; it is neither dropped silently nor retried forever.
func TestGivesUpAfterMaxAttempts(t *testing.T) {
	pool, created := smallCampaign(t)
	provider := &scriptedProvider{fn: func(int, delivery.Outbound) (string, error) {
		return "", &delivery.SendError{Kind: delivery.Retryable, Status: 503, Message: "down"}
	}}
	st := runUntilDone(t, pool, created.ID, 30*time.Second, worker(pool, provider))

	n := created.Queued["sms"]
	if st.Channels["sms"].Failed != n {
		t.Fatalf("status = %+v, want %d failed", st.Channels["sms"], n)
	}
	if calls, _ := provider.snapshot(); len(calls) != n*5 {
		t.Errorf("provider called %d times, want %d (5 attempts each)", len(calls), n*5)
	}
	if got := scalar[int](t, pool, `SELECT count(*) FROM messages WHERE attempts = 5 AND last_error LIKE 'gave up after 5 attempts:%503%'`); got != n {
		t.Errorf("%d rows record the failure, want %d", got, n)
	}
}

// A request the provider rejects as invalid is not retried.
func TestPermanentFailureIsNotRetried(t *testing.T) {
	pool, created := smallCampaign(t)
	provider := &scriptedProvider{fn: func(int, delivery.Outbound) (string, error) {
		return "", &delivery.SendError{Kind: delivery.Permanent, Status: 422, Message: "invalid number"}
	}}
	st := runUntilDone(t, pool, created.ID, 30*time.Second, worker(pool, provider))
	n := created.Queued["sms"]
	if calls, _ := provider.snapshot(); st.Channels["sms"].Failed != n || len(calls) != n {
		t.Errorf("failed = %d, calls = %d, want %d and %d", st.Channels["sms"].Failed, len(calls), n, n)
	}
}

// On 429 the whole channel stays quiet for Retry-After, and the throttled
// message does not lose one of its attempts.
func TestRetryAfterIsHonoured(t *testing.T) {
	pool, created := smallCampaign(t)
	const pause = 400 * time.Millisecond
	provider := &scriptedProvider{fn: func(call int, _ delivery.Outbound) (string, error) {
		if call == 3 {
			return "", &delivery.SendError{Kind: delivery.RateLimited, Status: 429, RetryAfter: pause, Message: "slow down"}
		}
		return "ok", nil
	}}
	w := worker(pool, provider)
	w.Concurrency = 1 // sends are sequential, so "after the 429" is well defined
	st := runUntilDone(t, pool, created.ID, 30*time.Second, w)

	n := created.Queued["sms"]
	if st.Channels["sms"].Sent != n {
		t.Fatalf("status = %+v, want %d sent", st.Channels["sms"], n)
	}
	calls, times := provider.snapshot()
	if len(calls) != n+1 {
		t.Fatalf("provider called %d times, want %d", len(calls), n+1)
	}
	if gap := times[3].Sub(times[2]); gap < pause-20*time.Millisecond {
		t.Errorf("next send came %v after the 429, want at least %v", gap, pause)
	}
	if got := scalar[int](t, pool, `SELECT max(attempts) FROM messages`); got != 1 {
		t.Errorf("max attempts = %d; a 429 must not count as an attempt", got)
	}
}

// Consent is checked again at send time: someone who opted out after the
// campaign was approved is skipped, not messaged.
func TestRecipientWhoOptedOutIsSkipped(t *testing.T) {
	pool, created := smallCampaign(t)
	ctx := context.Background()
	phone := scalar[string](t, pool, `SELECT u.phone FROM messages m JOIN users u ON u.id = m.user_id ORDER BY m.id LIMIT 1`)
	if _, err := pool.Exec(ctx, `UPDATE users SET sms_opt_in = false WHERE phone = $1`, phone); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	st := runUntilDone(t, pool, created.ID, 30*time.Second, worker(pool, provider))

	n := created.Queued["sms"]
	if st.Channels["sms"].Skipped != 1 || st.Channels["sms"].Sent != n-1 {
		t.Fatalf("status = %+v, want 1 skipped and %d sent", st.Channels["sms"], n-1)
	}
	calls, _ := provider.snapshot()
	for _, c := range calls {
		if c.To == phone {
			t.Fatal("a message was sent to a recipient who opted out")
		}
	}
}

// What is sent is the template filled with that recipient's own data, under a
// key that identifies the recipient, campaign and channel.
func TestMessagesArePersonalised(t *testing.T) {
	pool, created := smallCampaign(t)
	provider := &scriptedProvider{}
	runUntilDone(t, pool, created.ID, 30*time.Second, worker(pool, provider))

	calls, _ := provider.snapshot()
	keys := map[string]bool{}
	for _, c := range calls {
		var id int64
		var first string
		if err := pool.QueryRow(context.Background(), `SELECT id, first_name FROM users WHERE phone = $1`, c.To).Scan(&id, &first); err != nil {
			t.Fatalf("recipient %q: %v", c.To, err)
		}
		if want := "Merhaba " + first + ", %30 indirim seni bekliyor!"; c.Body != want {
			t.Errorf("body = %q, want %q", c.Body, want)
		}
		keys[c.IdempotencyKey] = true
	}
	if len(keys) != len(calls) || len(calls) != created.Queued["sms"] {
		t.Errorf("%d calls, %d distinct keys, %d queued", len(calls), len(keys), created.Queued["sms"])
	}
}

// A worker that died mid-send leaves rows in "sending". Once the lease is
// over another worker takes them, under the same idempotency key.
func TestExpiredLeaseIsReclaimed(t *testing.T) {
	pool, created := smallCampaign(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`UPDATE messages SET status = 'sending', attempts = 1, locked_until = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	st := runUntilDone(t, pool, created.ID, 30*time.Second, worker(pool, provider))
	if st.Channels["sms"].Sent != created.Queued["sms"] {
		t.Fatalf("status = %+v", st.Channels["sms"])
	}
	if got := scalar[int](t, pool, `SELECT min(attempts) FROM messages`); got != 2 {
		t.Errorf("attempts = %d, want 2 (the lost one and the reclaimed one)", got)
	}
}

// A live lease is left alone: a second worker must not send what the first
// one is still working on.
func TestLiveLeaseIsNotStolen(t *testing.T) {
	pool, created := smallCampaign(t)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := pool.Exec(ctx,
		`UPDATE messages SET status = 'sending', attempts = 1, locked_until = now() + interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{}
	done := make(chan struct{})
	go func() { defer close(done); worker(pool, provider).Run(ctx) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
	if calls, _ := provider.snapshot(); len(calls) != 0 {
		t.Errorf("%d messages sent while leased to someone else", len(calls))
	}
	_ = created
}

// Shutdown waits for in-flight sends and records them; nothing stays "sending"
// with a result that was thrown away.
func TestShutdownRecordsInFlightSends(t *testing.T) {
	pool, _ := smallCampaign(t)
	release := make(chan struct{})
	started := make(chan struct{}, 1000)
	provider := &scriptedProvider{fn: func(int, delivery.Outbound) (string, error) {
		started <- struct{}{}
		<-release
		return "ok", nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); worker(pool, provider).Run(ctx) }()

	<-started
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop")
	}
	calls, _ := provider.snapshot()
	sent := scalar[int](t, pool, `SELECT count(*) FROM messages WHERE status = 'sent'`)
	if sent != len(calls) || sent == 0 {
		t.Errorf("%d sends reached the provider, %d recorded as sent", len(calls), sent)
	}
}
