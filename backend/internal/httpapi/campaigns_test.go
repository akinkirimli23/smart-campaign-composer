package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/metrics"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

type fakeCampaigns struct {
	created int
	rule    segment.Rule
	content content.Content
	asOf    time.Time
	sendAt  time.Time
	err     error

	cancelled int
}

func (f *fakeCampaigns) Create(_ context.Context, _ string, rule segment.Rule, c content.Content, asOf, sendAt time.Time) (campaign.Created, error) {
	if f.err != nil {
		return campaign.Created{}, f.err
	}
	f.created++
	f.rule, f.content, f.asOf, f.sendAt = rule, c, asOf, sendAt
	return campaign.Created{ID: 7, AsOf: asOf, SendAt: sendAt, Queued: map[string]int{"sms": 222, "email": 254}}, nil
}

func (f *fakeCampaigns) Cancel(_ context.Context, id int64, now time.Time) (campaign.Status, error) {
	if id != 7 {
		return campaign.Status{}, campaign.ErrNotFound
	}
	f.cancelled++
	return campaign.Status{ID: 7, CancelledAt: &now, Done: true, Channels: map[string]campaign.Counts{"sms": {Total: 222, Cancelled: 222}}}, nil
}

func (f *fakeCampaigns) Status(_ context.Context, id int64) (campaign.Status, error) {
	if id != 7 {
		return campaign.Status{}, campaign.ErrNotFound
	}
	return campaign.Status{ID: 7, Channels: map[string]campaign.Counts{"sms": {Total: 222, Sent: 200, Pending: 22}}}, nil
}

func (f *fakeCampaigns) List(_ context.Context, now time.Time) ([]campaign.Summary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []campaign.Summary{
		{ID: 9, Brief: "haftaya", SendAt: now.Add(7 * 24 * time.Hour), State: campaign.StateScheduled, Messages: campaign.Counts{Total: 18, Pending: 18}},
		{ID: 7, Brief: "bitti", SendAt: now.Add(-time.Hour), State: campaign.StateDone, Messages: campaign.Counts{Total: 476, Sent: 476}},
	}, nil
}

func (f *fakeCampaigns) Totals(context.Context) (campaign.Totals, error) {
	return campaign.Totals{Campaigns: 2, Messages: map[string]map[string]int{
		"sms": {"sent": 200, "pending": 22}, "email": {"sent": 254},
	}}, nil
}

func campaignRequest(t *testing.T, store *fakeCampaigns, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	srv := New(Options{DB: fakePinger{}, Campaigns: store, VarLengths: testLengths, Now: func() time.Time { return fixedNow }})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON: %s", rec.Body)
	}
	return rec, out
}

const (
	okRule    = `{"match":"all","conditions":[{"type":"attribute","field":"city","op":"in","values":["İstanbul"]}]}`
	okContent = `{"sms":"Merhaba {{first_name}}, %30 indirim!","email":{"subject":"Fırsat","body":"Merhaba"}}`
)

func approve(rule, c string) string {
	return `{"brief":"b","rule":` + rule + `,"content":` + c + `}`
}

func TestCampaignCreate(t *testing.T) {
	store := &fakeCampaigns{}
	rec, body := campaignRequest(t, store, http.MethodPost, "/api/campaigns", approve(okRule, okContent))
	if rec.Code != http.StatusCreated || body["id"] != float64(7) {
		t.Fatalf("status %d, body %v", rec.Code, body)
	}
	if body["queued"].(map[string]any)["sms"] != float64(222) {
		t.Errorf("queued = %v", body["queued"])
	}
	if store.created != 1 || store.rule.Conditions[0].Values[0] != "İstanbul" ||
		store.content.SMS != "Merhaba {{first_name}}, %30 indirim!" || !store.asOf.Equal(fixedNow) {
		t.Errorf("store got rule %+v, content %+v, asOf %v", store.rule, store.content, store.asOf)
	}
}

// Approval re-validates what the browser sends. Text the marketer edited by
// hand gets the same checks as the model's output, and nothing is queued when
// any of them fails.
func TestCampaignCreateRevalidates(t *testing.T) {
	longSMS := `{"sms":"` + strings.Repeat("x", 151) + `{{first_name}}","email":{"subject":"s","body":"b"}}`
	tests := []struct {
		name, body, code, path, message string
		status                          int
	}{
		{"sms over the limit after personalisation", approve(okRule, longSMS), "content_invalid", "sms", "161 karakter, limit 160", 422},
		{"unknown variable", approve(okRule, `{"sms":"Merhaba {{nickname}}","email":{"subject":"s","body":"b"}}`), "content_invalid", "sms", "bilinmeyen bir değişken", 422},
		{"subject over the limit", approve(okRule, `{"sms":"a","email":{"subject":"`+strings.Repeat("ş", 81)+`","body":"b"}}`), "content_invalid", "email.subject", "81 karakter, limit 80", 422},
		{"extra content field", approve(okRule, `{"sms":"a","email":{"subject":"s","body":"b"},"push":"x"}`), "content_invalid", "push", "bilinmeyen alan", 422},
		{"city outside the set", approve(`{"match":"all","conditions":[{"type":"attribute","field":"city","op":"in","values":["Atlantis"]}]}`, okContent), "rule_invalid", "conditions[0].values[0]", "", 422},
		{"sql in the rule", approve(`{"match":"all","conditions":[{"type":"attribute","field":"city; DROP TABLE users","op":"in","values":["İstanbul"]}]}`, okContent), "rule_invalid", "conditions[0].field", "", 422},
		{"missing content", `{"brief":"b","rule":` + okRule + `}`, "invalid_request", "", "", 400},
		{"missing rule", `{"brief":"b","content":` + okContent + `}`, "invalid_request", "", "", 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeCampaigns{}
			rec, body := campaignRequest(t, store, http.MethodPost, "/api/campaigns", tt.body)
			errBody := body["error"].(map[string]any)
			if rec.Code != tt.status || errBody["code"] != tt.code {
				t.Fatalf("status %d, error %v", rec.Code, errBody)
			}
			if store.created != 0 {
				t.Error("a campaign was created from invalid input")
			}
			if tt.path == "" {
				return
			}
			first := errBody["details"].([]any)[0].(map[string]any)
			if first["path"] != tt.path || !strings.Contains(first["message"].(string), tt.message) {
				t.Errorf("details = %v", errBody["details"])
			}
		})
	}
}

func TestCampaignCreateEmptyAudience(t *testing.T) {
	rec, body := campaignRequest(t, &fakeCampaigns{err: campaign.ErrEmptyAudience}, http.MethodPost, "/api/campaigns", approve(okRule, okContent))
	if rec.Code != http.StatusUnprocessableEntity || body["error"].(map[string]any)["code"] != "audience_empty" {
		t.Errorf("status %d, body %v", rec.Code, body)
	}
}

func TestCampaignStatus(t *testing.T) {
	store := &fakeCampaigns{}
	rec, body := campaignRequest(t, store, http.MethodGet, "/api/campaigns/7", "")
	if rec.Code != http.StatusOK || body["channels"].(map[string]any)["sms"].(map[string]any)["sent"] != float64(200) {
		t.Errorf("status %d, body %v", rec.Code, body)
	}
	for path, want := range map[string]int{"/api/campaigns/8": 404, "/api/campaigns/abc": 400, "/api/campaigns/0": 400} {
		if rec, _ := campaignRequest(t, store, http.MethodGet, path, ""); rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}
}

// progressStore reports a different status on every read, like a campaign
// that is being sent.
type progressStore struct {
	fakeCampaigns
	steps []campaign.Status
	reads int
}

func (p *progressStore) Status(_ context.Context, id int64) (campaign.Status, error) {
	if id != 7 {
		return campaign.Status{}, campaign.ErrNotFound
	}
	st := p.steps[min(p.reads, len(p.steps)-1)]
	p.reads++
	return st, nil
}

func smsStatus(sent, pending int) campaign.Status {
	return campaign.Status{ID: 7, Done: pending == 0, Channels: map[string]campaign.Counts{
		"sms": {Total: sent + pending, Sent: sent, Pending: pending},
	}}
}

// The live stream sends the status on connect, then only when it changes, and
// ends with "done" once nothing is pending.
func TestCampaignEventsStream(t *testing.T) {
	store := &progressStore{steps: []campaign.Status{
		smsStatus(0, 10), smsStatus(0, 10), smsStatus(4, 6), smsStatus(4, 6), smsStatus(10, 0),
	}}
	srv := New(Options{DB: fakePinger{}, Campaigns: store, StatusInterval: time.Millisecond})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/campaigns/7/events", nil))

	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	var got []string
	for _, e := range parseSSE(t, rec.Body.String()) {
		if e.name == "status" {
			sms := e.data["channels"].(map[string]any)["sms"].(map[string]any)
			got = append(got, "sent="+strconv.Itoa(int(sms["sent"].(float64))))
		} else {
			got = append(got, e.name)
		}
	}
	if want := "sent=0 sent=4 sent=10 done"; strings.Join(got, " ") != want {
		t.Errorf("events = %v, want %s", got, want)
	}
}

// A finished campaign answers with its final status and closes at once.
func TestCampaignEventsAlreadyDone(t *testing.T) {
	store := &progressStore{steps: []campaign.Status{smsStatus(10, 0)}}
	srv := New(Options{DB: fakePinger{}, Campaigns: store, StatusInterval: time.Hour})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/campaigns/7/events", nil))
	events := parseSSE(t, rec.Body.String())
	if len(events) != 2 || events[0].name != "status" || events[1].name != "done" {
		t.Errorf("events = %+v", events)
	}
}

// The stream stops when the client goes away instead of polling forever.
func TestCampaignEventsStopOnDisconnect(t *testing.T) {
	store := &progressStore{steps: []campaign.Status{smsStatus(0, 10)}}
	srv := New(Options{DB: fakePinger{}, Campaigns: store, StatusInterval: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/campaigns/7/events", nil).WithContext(ctx))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop after the client disconnected")
	}
}

func TestCampaignEventsNotFound(t *testing.T) {
	rec, body := campaignRequest(t, &fakeCampaigns{}, http.MethodGet, "/api/campaigns/8/events", "")
	if rec.Code != http.StatusNotFound || body["error"].(map[string]any)["code"] != "not_found" {
		t.Errorf("status %d, body %v", rec.Code, body)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	registry := metrics.New()
	registry.Inc("scc_provider_requests_total", "channel", "sms", "outcome", "ok")
	registry.Inc("scc_provider_requests_total", "channel", "sms", "outcome", "ok")
	registry.Inc("scc_provider_requests_total", "channel", "email", "outcome", "rate_limited")
	srv := New(Options{DB: fakePinger{}, Campaigns: &fakeCampaigns{}, Metrics: registry})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE scc_campaigns gauge\nscc_campaigns 2\n",
		"# TYPE scc_messages gauge\n",
		`scc_messages{channel="sms",status="pending"} 22`,
		`scc_messages{channel="sms",status="sent"} 200`,
		`scc_messages{channel="sms",status="failed"} 0`,
		`scc_messages{channel="email",status="sent"} 254`,
		"# TYPE scc_provider_requests_total counter\n",
		`scc_provider_requests_total{channel="email",outcome="rate_limited"} 1`,
		`scc_provider_requests_total{channel="sms",outcome="ok"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output lacks %q:\n%s", want, body)
		}
	}
	// Every sample line is "name{labels} value" or "name value".
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if !strings.HasPrefix(line, "#") && len(strings.Fields(line)) != 2 {
			t.Errorf("malformed sample line %q", line)
		}
	}
}

// Scheduling: the time travels to the store; times in the past or too far
// ahead are refused before anything is queued.
func TestCampaignCreateScheduled(t *testing.T) {
	scheduled := func(sendAt string) string {
		return `{"brief":"b","rule":` + okRule + `,"content":` + okContent + `,"send_at":` + sendAt + `}`
	}
	store := &fakeCampaigns{}
	when := fixedNow.Add(48 * time.Hour)
	rec, body := campaignRequest(t, store, http.MethodPost, "/api/campaigns", scheduled(`"`+when.Format(time.RFC3339)+`"`))
	if rec.Code != http.StatusCreated || !store.sendAt.Equal(when) || body["send_at"] != when.Format(time.RFC3339) {
		t.Fatalf("status %d, sendAt %v, body %v", rec.Code, store.sendAt, body)
	}

	// No send_at, or null: at once (the store treats the zero time as "now").
	for _, payload := range []string{approve(okRule, okContent), scheduled(`null`)} {
		store := &fakeCampaigns{}
		if rec, _ := campaignRequest(t, store, http.MethodPost, "/api/campaigns", payload); rec.Code != http.StatusCreated || !store.sendAt.IsZero() {
			t.Errorf("immediate: status %d, sendAt %v", rec.Code, store.sendAt)
		}
	}

	for name, tt := range map[string]struct {
		sendAt string
		status int
		want   string
	}{
		"in the past":        {`"` + fixedNow.Add(-time.Hour).Format(time.RFC3339) + `"`, 422, "geçmişte"},
		"too far ahead":      {`"` + fixedNow.Add(31*24*time.Hour).Format(time.RFC3339) + `"`, 422, "30 gün"},
		"not a time":         {`"yarın sabah"`, 400, ""},
		"a few seconds late": {`"` + fixedNow.Add(-20*time.Second).Format(time.RFC3339) + `"`, 201, ""},
	} {
		store := &fakeCampaigns{}
		rec, body := campaignRequest(t, store, http.MethodPost, "/api/campaigns", scheduled(tt.sendAt))
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d (%v)", name, rec.Code, tt.status, body)
			continue
		}
		if tt.status >= 400 && store.created != 0 {
			t.Errorf("%s: a campaign was created", name)
		}
		if tt.want != "" && !strings.Contains(body["error"].(map[string]any)["message"].(string), tt.want) {
			t.Errorf("%s: body %v", name, body)
		}
	}
}

func TestCampaignCancel(t *testing.T) {
	store := &fakeCampaigns{}
	rec, body := campaignRequest(t, store, http.MethodPost, "/api/campaigns/7/cancel", "")
	if rec.Code != http.StatusOK || store.cancelled != 1 || body["cancelled_at"] == nil ||
		body["channels"].(map[string]any)["sms"].(map[string]any)["cancelled"] != float64(222) {
		t.Fatalf("status %d, body %v", rec.Code, body)
	}
	for path, want := range map[string]int{"/api/campaigns/8/cancel": 404, "/api/campaigns/x/cancel": 400} {
		if rec, _ := campaignRequest(t, store, http.MethodPost, path, ""); rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}
}

func TestCampaignList(t *testing.T) {
	rec, body := campaignRequest(t, &fakeCampaigns{}, http.MethodGet, "/api/campaigns", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	list := body["campaigns"].([]any)
	first := list[0].(map[string]any)
	if len(list) != 2 || first["id"] != float64(9) || first["state"] != "scheduled" || first["brief"] != "haftaya" ||
		first["messages"].(map[string]any)["pending"] != float64(18) {
		t.Errorf("list = %v", list)
	}
	if _, present := first["cancelled_at"]; present {
		t.Errorf("a campaign that was not cancelled has cancelled_at: %v", first)
	}

	rec, body = campaignRequest(t, &fakeCampaigns{err: errors.New("db down")}, http.MethodGet, "/api/campaigns", "")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "db down") {
		t.Errorf("status %d, body %v", rec.Code, body)
	}
}
