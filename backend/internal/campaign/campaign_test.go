package campaign_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/testdb"
)

var refNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

var exampleRule = segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
	{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	{Type: segment.TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true},
	{Type: segment.TypeEvent, Event: "purchase", WithinDays: 30, Exists: false},
}}

var texts = content.Content{
	SMS:   "Merhaba {{first_name}}, %30 indirim!",
	Email: content.Email{Subject: "{{first_name}}, fırsat", Body: "Merhaba"},
}

// What is queued is exactly what the audience count promised the marketer:
// the example brief queues 222 SMS and 254 emails, and nothing for the 58
// people who cannot be reached.
func TestCreateQueuesTheCountedAudience(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 5000, 42, refNow); err != nil {
		t.Fatal(err)
	}
	store := campaign.Store{DB: pool}

	created, err := store.Create(ctx, "brief", exampleRule, texts, refNow)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := audience.Counter{DB: pool}.Count(ctx, exampleRule, refNow)
	if err != nil {
		t.Fatal(err)
	}
	if created.Queued["sms"] != 222 || created.Queued["email"] != 254 ||
		created.Queued["sms"] != counts.SMS || created.Queued["email"] != counts.Email {
		t.Fatalf("queued %+v, counted %+v", created.Queued, counts)
	}

	// Nobody is queued on a channel they did not consent to or have no address for.
	var bad int
	err = pool.QueryRow(ctx, `
SELECT count(*) FROM messages m JOIN users u ON u.id = m.user_id
WHERE (m.channel = 'sms' AND NOT (u.sms_opt_in AND u.phone IS NOT NULL))
   OR (m.channel = 'email' AND NOT (u.email_opt_in AND u.email IS NOT NULL))`).Scan(&bad)
	if err != nil || bad != 0 {
		t.Fatalf("%d messages queued without consent or address (err %v)", bad, err)
	}

	st, err := store.Status(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Done || st.Channels["sms"] != (campaign.Counts{Total: 222, Pending: 222}) || st.Channels["email"].Pending != 254 {
		t.Errorf("status = %+v", st)
	}

	// A second campaign with the same rule is a separate campaign: the
	// uniqueness is per campaign, not global.
	again, err := store.Create(ctx, "brief", exampleRule, texts, refNow)
	if err != nil || again.ID == created.ID || again.Queued["sms"] != 222 {
		t.Errorf("second campaign: %+v, %v", again, err)
	}
}

// An audience nobody in can be reached leaves nothing behind.
func TestCreateEmptyAudience(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 200, 7, refNow); err != nil {
		t.Fatal(err)
	}
	impossible := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: 119, Max: 120},
	}}
	_, err := campaign.Store{DB: pool}.Create(ctx, "brief", impossible, texts, refNow)
	if !errors.Is(err, campaign.ErrEmptyAudience) {
		t.Fatalf("err = %v, want ErrEmptyAudience", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM campaigns`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d campaign rows left behind (err %v)", n, err)
	}
}

// An invalid rule never reaches SQL, also on this path.
func TestCreateValidatesTheRule(t *testing.T) {
	pool := testdb.New(t)
	bad := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"x'); DROP TABLE users; --"}},
	}}
	_, err := campaign.Store{DB: pool}.Create(context.Background(), "brief", bad, texts, refNow)
	var verr segment.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, want a validation error", err)
	}
}

func TestStatusNotFound(t *testing.T) {
	pool := testdb.New(t)
	if _, err := (campaign.Store{DB: pool}).Status(context.Background(), 12345); !errors.Is(err, campaign.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
