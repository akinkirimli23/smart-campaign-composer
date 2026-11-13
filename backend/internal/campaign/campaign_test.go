package campaign_test

import (
	"context"
	"errors"
	"strings"
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

var texts = content.Single("tr", content.Content{
	SMS:   "Merhaba {{first_name}}, %30 indirim!",
	Email: content.Email{Subject: "{{first_name}}, fırsat", Body: "Merhaba"},
})

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

	created, err := store.Create(ctx, "brief", exampleRule, texts, refNow, time.Time{})
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

	// The queue-wide totals (what /metrics reports) are the same rows.
	totals, err := store.Totals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if totals.Campaigns != 1 || totals.Messages["sms"]["pending"] != 222 || totals.Messages["email"]["pending"] != 254 {
		t.Errorf("totals = %+v", totals)
	}

	// A second campaign with the same rule is a separate campaign: the
	// uniqueness is per campaign, not global.
	again, err := store.Create(ctx, "brief", exampleRule, texts, refNow, time.Time{})
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
	_, err := campaign.Store{DB: pool}.Create(ctx, "brief", impossible, texts, refNow, time.Time{})
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
	_, err := campaign.Store{DB: pool}.Create(context.Background(), "brief", bad, texts, refNow, time.Time{})
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

// A send time before "now" (or none at all) means at once; a later one is
// stored and becomes every message's earliest attempt time.
func TestCreateSendAt(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 300, 7, refNow); err != nil {
		t.Fatal(err)
	}
	store := campaign.Store{DB: pool}
	rule := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	}}

	for name, tt := range map[string]struct{ sendAt, want time.Time }{
		"none":   {time.Time{}, refNow},
		"past":   {refNow.Add(-time.Hour), refNow},
		"future": {refNow.Add(72 * time.Hour), refNow.Add(72 * time.Hour)},
	} {
		created, err := store.Create(ctx, "brief", rule, texts, refNow, tt.sendAt)
		if err != nil {
			t.Fatal(err)
		}
		var earliest, latest time.Time
		if err := pool.QueryRow(ctx, `SELECT min(next_attempt_at), max(next_attempt_at) FROM messages WHERE campaign_id = $1`,
			created.ID).Scan(&earliest, &latest); err != nil {
			t.Fatal(err)
		}
		if !created.SendAt.Equal(tt.want) || !earliest.Equal(tt.want) || !latest.Equal(tt.want) {
			t.Errorf("%s: send_at %v, messages due %v..%v, want %v", name, created.SendAt, earliest, latest, tt.want)
		}
		st, err := store.Status(ctx, created.ID)
		if err != nil || !st.SendAt.Equal(tt.want) || st.CancelledAt != nil {
			t.Errorf("%s: status %+v, %v", name, st, err)
		}
	}
}

// Cancel touches only what is still waiting.
func TestCancelKeepsWhatWasAlreadySent(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 300, 7, refNow); err != nil {
		t.Fatal(err)
	}
	store := campaign.Store{DB: pool}
	rule := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	}}
	created, err := store.Create(ctx, "brief", rule, texts, refNow, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Create(ctx, "brief", rule, texts, refNow, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE messages SET status = 'sent', sent_at = now()
WHERE id IN (SELECT id FROM messages WHERE campaign_id = $1 AND channel = 'sms' ORDER BY id LIMIT 4)`, created.ID); err != nil {
		t.Fatal(err)
	}

	st, err := store.Cancel(ctx, created.ID, refNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	sms := st.Channels["sms"]
	if sms.Sent != 4 || sms.Cancelled != created.Queued["sms"]-4 || sms.Pending != 0 ||
		st.Channels["email"].Cancelled != created.Queued["email"] || !st.Done {
		t.Fatalf("after cancel: %+v", st)
	}
	// Another campaign is not affected.
	if o, err := store.Status(ctx, other.ID); err != nil || o.Channels["sms"].Pending != other.Queued["sms"] || o.CancelledAt != nil {
		t.Errorf("other campaign: %+v, %v", o, err)
	}
	totals, err := store.Totals(ctx)
	if err != nil || totals.Messages["sms"]["cancelled"] != created.Queued["sms"]-4 {
		t.Errorf("totals: %+v, %v", totals, err)
	}
}

// The list shows every campaign, newest first, in the state a marketer would
// call it: scheduled, sending, done or cancelled.
func TestList(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 300, 7, refNow); err != nil {
		t.Fatal(err)
	}
	store := campaign.Store{DB: pool}
	if list, err := store.List(ctx, refNow); err != nil || len(list) != 0 || list == nil {
		t.Fatalf("empty database: %v, %v (want an empty, non-nil list)", list, err)
	}

	rule := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	}}
	create := func(brief string, sendAt time.Time) campaign.Created {
		t.Helper()
		c, err := store.Create(ctx, brief, rule, texts, refNow, sendAt)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	sending := create("şimdi gidiyor", time.Time{})
	finished := create("bitti", time.Time{})
	halfHour := create("yarım saat sonra", refNow.Add(30*time.Minute))
	nextWeek := create("haftaya", refNow.Add(7*24*time.Hour))
	cancelled := create("vazgeçildi", refNow.Add(48*time.Hour))

	if _, err := pool.Exec(ctx, `UPDATE messages SET status = 'sent', sent_at = now() WHERE campaign_id = $1`, finished.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE messages SET status = 'sent', sent_at = now()
WHERE id IN (SELECT id FROM messages WHERE campaign_id = $1 ORDER BY id LIMIT 5)`, sending.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Cancel(ctx, cancelled.ID, refNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	list, err := store.List(ctx, refNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id    int64
		brief string
		state string
	}
	var got []row
	for _, c := range list {
		got = append(got, row{c.ID, c.Brief, c.State})
	}
	want := []row{
		{cancelled.ID, "vazgeçildi", campaign.StateCancelled},
		{nextWeek.ID, "haftaya", campaign.StateScheduled},
		{halfHour.ID, "yarım saat sonra", campaign.StateScheduled},
		{finished.ID, "bitti", campaign.StateDone},
		{sending.ID, "şimdi gidiyor", campaign.StateSending},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d campaigns, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// The counts are the campaign's messages over both channels, and agree with Status.
	total := sending.Queued["sms"] + sending.Queued["email"]
	last := list[len(list)-1].Messages
	if last.Total != total || last.Sent != 5 || last.Pending != total-5 {
		t.Errorf("sending campaign: %+v, want total %d, 5 sent", last, total)
	}
	st, err := store.Status(ctx, nextWeek.ID)
	if err != nil || st.Brief != "haftaya" ||
		st.Channels["sms"].Pending+st.Channels["email"].Pending != list[1].Messages.Pending {
		t.Errorf("status %+v vs list %+v (%v)", st, list[1], err)
	}

	// Once the half-hour campaign's time has passed it is no longer "scheduled",
	// even before the worker has picked anything up.
	later, err := store.List(ctx, refNow.Add(31*time.Minute))
	if err != nil || later[2].ID != halfHour.ID || later[2].State != campaign.StateSending || later[1].State != campaign.StateScheduled {
		t.Errorf("after 31 minutes: %+v, %v", later, err)
	}
}

// A campaign keeps one set of texts per language and reports which
// languages it has, the default first.
func TestCampaignKeepsEveryLanguage(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 300, 7, refNow); err != nil {
		t.Fatal(err)
	}
	store := campaign.Store{DB: pool}
	rule := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	}}
	both := content.Localized{Default: "tr", Variants: map[string]content.Content{
		"tr": texts.Variants["tr"],
		"en": {SMS: "Hi {{first_name}}, 30% off!", Email: content.Email{Subject: "An offer", Body: "Hi"}},
	}}

	created, err := store.Create(ctx, "brief", rule, both, refNow, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var def, english string
	err = pool.QueryRow(ctx, `SELECT content->>'default', content->'variants'->'en'->>'sms' FROM campaigns WHERE id = $1`, created.ID).
		Scan(&def, &english)
	if err != nil || def != "tr" || english != "Hi {{first_name}}, 30% off!" {
		t.Fatalf("stored default %q, english sms %q (err %v)", def, english, err)
	}
	st, err := store.Status(ctx, created.ID)
	if err != nil || len(st.Languages) != 2 || st.Languages[0] != "tr" || st.Languages[1] != "en" {
		t.Errorf("languages = %v, err = %v", st.Languages, err)
	}

	single, err := store.Create(ctx, "brief", rule, texts, refNow, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := store.Status(ctx, single.ID); err != nil || len(st.Languages) != 1 || st.Languages[0] != "tr" {
		t.Errorf("languages = %v, err = %v", st.Languages, err)
	}
}

// The recipient list is the same rows the dashboard counts: every message
// once, the ones that need attention first, addresses masked.
func TestRecipients(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	if _, err := seed.Load(ctx, pool, 300, 7, refNow); err != nil {
		t.Fatal(err)
	}
	store := campaign.Store{DB: pool}
	rule := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
	}}
	created, err := store.Create(ctx, "brief", rule, texts, refNow, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	total := created.Queued["sms"] + created.Queued["email"]
	if total < 12 {
		t.Fatalf("audience too small: %+v", created.Queued)
	}

	// Give a few messages an outcome, as the worker would.
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(n int) []int64 {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT id FROM messages WHERE campaign_id = $1 AND status = 'pending' ORDER BY id DESC LIMIT $2`, created.ID, n)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out = append(out, id)
		}
		return out
	}
	exec(`UPDATE messages SET status = 'sent', attempts = 1, sent_at = $2 WHERE id = ANY($1)`, ids(4), refNow)
	exec(`UPDATE messages SET status = 'skipped', attempts = 1, last_error = 'recipient no longer reachable on this channel' WHERE id = ANY($1)`, ids(1))
	exec(`UPDATE messages SET status = 'failed', attempts = 5, last_error = 'gave up after 5 attempts: 503' WHERE id = ANY($1)`, ids(2))

	all, err := store.Recipients(ctx, created.ID, "", campaign.MaxRecipientPage, 0)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != total || len(all.Recipients) != min(total, campaign.MaxRecipientPage) {
		t.Fatalf("total %d, page %d, queued %d", all.Total, len(all.Recipients), total)
	}
	// Failed, then skipped, then waiting; delivered ones come last.
	got := []string{all.Recipients[0].Status, all.Recipients[1].Status, all.Recipients[2].Status, all.Recipients[3].Status}
	if got[0] != "failed" || got[1] != "failed" || got[2] != "skipped" || got[3] != "pending" {
		t.Errorf("order = %v", got)
	}
	if first := all.Recipients[0]; first.Attempts != 5 || first.Error != "gave up after 5 attempts: 503" || first.Name == " " || first.Language == "" {
		t.Errorf("failed recipient = %+v", first)
	}
	for _, r := range all.Recipients {
		if !strings.Contains(r.Address, "•") || len([]rune(r.Address)) < 5 {
			t.Fatalf("address not masked: %+v", r)
		}
		if r.Channel == "email" && !strings.HasSuffix(r.Address, "@example.com") {
			t.Fatalf("email address lost its domain: %+v", r)
		}
	}

	// One status at a time; the total is that of the filter.
	sent, err := store.Recipients(ctx, created.ID, "sent", 2, 0)
	if err != nil || sent.Total != 4 || len(sent.Recipients) != 2 || sent.Recipients[0].SentAt == nil {
		t.Fatalf("sent = %+v, err = %v", sent, err)
	}
	// Pages do not overlap, and a page past the end is empty but still says how many there are.
	next, err := store.Recipients(ctx, created.ID, "sent", 2, 2)
	if err != nil || len(next.Recipients) != 2 || next.Recipients[0].UserID == sent.Recipients[0].UserID && next.Recipients[0].Channel == sent.Recipients[0].Channel {
		t.Errorf("second page = %+v, err = %v", next, err)
	}
	past, err := store.Recipients(ctx, created.ID, "sent", 2, 10)
	if err != nil || len(past.Recipients) != 0 || past.Total != 4 {
		t.Errorf("past the end = %+v, err = %v", past, err)
	}

	if _, err := store.Recipients(ctx, created.ID+99, "", 10, 0); !errors.Is(err, campaign.ErrNotFound) {
		t.Errorf("unknown campaign: err = %v", err)
	}
	if _, err := store.Recipients(ctx, created.ID, "sent'; DROP TABLE users; --", 10, 0); !errors.Is(err, campaign.ErrUnknownStatus) {
		t.Errorf("status outside the set: err = %v", err)
	}
}

func TestMasking(t *testing.T) {
	for in, want := range map[string]string{
		"+905321234567": "+90••••••••67",
		"12345":         "•••••",
		"":              "",
	} {
		if got := campaign.MaskPhone(in); got != want {
			t.Errorf("MaskPhone(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"ayse.yilmaz.12@example.com": "a•••@example.com",
		"şule@example.com":           "ş•••@example.com",
		"not-an-address":             "•••",
		"@example.com":               "•••",
	} {
		if got := campaign.MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
