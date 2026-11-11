package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

type fakeCampaigns struct {
	created int
	rule    segment.Rule
	content content.Content
	asOf    time.Time
	err     error
}

func (f *fakeCampaigns) Create(_ context.Context, _ string, rule segment.Rule, c content.Content, asOf time.Time) (campaign.Created, error) {
	if f.err != nil {
		return campaign.Created{}, f.err
	}
	f.created++
	f.rule, f.content, f.asOf = rule, c, asOf
	return campaign.Created{ID: 7, AsOf: asOf, Queued: map[string]int{"sms": 222, "email": 254}}, nil
}

func (f *fakeCampaigns) Status(_ context.Context, id int64) (campaign.Status, error) {
	if id != 7 {
		return campaign.Status{}, campaign.ErrNotFound
	}
	return campaign.Status{ID: 7, Channels: map[string]campaign.Counts{"sms": {Total: 222, Sent: 200, Pending: 22}}}, nil
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
