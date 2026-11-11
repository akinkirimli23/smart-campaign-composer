package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/mockllm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

var fixedNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// fakeCounter records what it was asked to count.
type fakeCounter struct {
	calls int
	rule  segment.Rule
	asOf  time.Time
}

func (f *fakeCounter) Count(_ context.Context, rule segment.Rule, asOf time.Time) (audience.Counts, error) {
	f.calls++
	f.rule, f.asOf = rule, asOf
	return audience.Counts{Total: 381, SMS: 222, Email: 254, Unreachable: 58}, nil
}

func post(t *testing.T, model llm.Client, counter *fakeCounter, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	srv := New(Options{
		DB: fakePinger{}, Segments: segment.Generator{LLM: model}, Audience: counter,
		Now: func() time.Time { return fixedNow },
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response is not JSON: %s", rec.Body)
	}
	return rec, decoded
}

func TestGenerateEndpoint(t *testing.T) {
	counter := &fakeCounter{}
	rec, body := post(t, mockllm.Client{}, counter, "/api/segments/generate",
		`{"brief":"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."}`)
	if rec.Code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if got := body["audience"].(map[string]any)["total"]; got != float64(381) {
		t.Errorf("audience.total = %v", got)
	}
	// The description comes from the rule, not from the model.
	conds := body["description"].(map[string]any)["conditions"].([]any)
	if len(conds) != 3 || conds[2] != "Son 30 günde hiç satın alma yapmadı" {
		t.Errorf("description = %v", conds)
	}
	if len(body["assumptions"].([]any)) != 1 {
		t.Errorf("assumptions = %v", body["assumptions"])
	}
	if !counter.asOf.Equal(fixedNow) || len(counter.rule.Conditions) != 3 {
		t.Errorf("counter got rule %+v as of %v", counter.rule, counter.asOf)
	}
}

func TestGenerateEndpointUnsupported(t *testing.T) {
	counter := &fakeCounter{}
	rec, body := post(t, mockllm.Client{}, counter, "/api/segments/generate", `{"brief":"En çok harcayan 100 müşteriye yaz"}`)
	if rec.Code != http.StatusOK || body["status"] != "unsupported" || body["rule"] != nil {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if counter.calls != 0 {
		t.Error("nothing must be counted without a rule")
	}
}

func TestGenerateEndpointErrors(t *testing.T) {
	bad := `{"result":{"kind":"rule","rule":{"match":"all","conditions":[{"type":"attribute","field":"city","op":"in","values":["Paris"]}]},"assumptions":[]}}`
	tests := []struct {
		name       string
		model      llm.Client
		body       string
		wantStatus int
		wantCode   string
	}{
		{"empty brief", mockllm.Client{}, `{"brief":"  "}`, http.StatusBadRequest, "invalid_request"},
		{"brief too long", mockllm.Client{}, `{"brief":"` + strings.Repeat("ş", 2001) + `"}`, http.StatusBadRequest, "invalid_request"},
		{"not json", mockllm.Client{}, `brief=x`, http.StatusBadRequest, "invalid_request"},
		{"body too large", mockllm.Client{}, `{"brief":"` + strings.Repeat("a", maxBodyBytes) + `"}`, http.StatusRequestEntityTooLarge, "invalid_request"},
		{"model never valid", &llm.Scripted{Replies: []string{bad, bad, bad}}, `{"brief":"Paris'tekilere yaz"}`, http.StatusUnprocessableEntity, "llm_output_invalid"},
		{"model unreachable", &llm.Scripted{}, `{"brief":"İstanbul'dakilere yaz"}`, http.StatusBadGateway, "llm_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := &fakeCounter{}
			rec, body := post(t, tt.model, counter, "/api/segments/generate", tt.body)
			errBody, _ := body["error"].(map[string]any)
			if rec.Code != tt.wantStatus || errBody["code"] != tt.wantCode {
				t.Fatalf("status %d, body %s", rec.Code, rec.Body)
			}
			if counter.calls != 0 {
				t.Error("no query may run when generation fails")
			}
		})
	}
}

func TestCountEndpoint(t *testing.T) {
	counter := &fakeCounter{}
	rec, body := post(t, nil, counter, "/api/segments/count",
		`{"rule":{"match":"all","conditions":[{"type":"attribute","field":"city","op":"in","values":["Ankara"]}]}}`)
	if rec.Code != http.StatusOK || body["status"] != "ok" || counter.calls != 1 {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}

	// A hand-edited rule gets the same validation as the model's output.
	counter = &fakeCounter{}
	rec, body = post(t, nil, counter, "/api/segments/count",
		`{"rule":{"match":"all","conditions":[{"type":"attribute","field":"city","op":"in","values":["Istanbul' OR 1=1 --"]}]}}`)
	errBody, _ := body["error"].(map[string]any)
	if rec.Code != http.StatusUnprocessableEntity || errBody["code"] != "rule_invalid" || counter.calls != 0 {
		t.Fatalf("status %d, body %s, calls %d", rec.Code, rec.Body, counter.calls)
	}
	details := errBody["details"].([]any)[0].(map[string]any)
	if details["path"] != "conditions[0].values[0]" {
		t.Errorf("details = %v", details)
	}

	rec, _ = post(t, nil, &fakeCounter{}, "/api/segments/count", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing rule: status %d", rec.Code)
	}
}

// Every value the editor offers is accepted by the validator, and nothing the
// validator accepts is missing from the editor.
func TestSegmentOptionsMatchTheValidator(t *testing.T) {
	srv := New(Options{DB: fakePinger{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/segments/options", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var opts segment.Options
	if err := json.Unmarshal(rec.Body.Bytes(), &opts); err != nil {
		t.Fatal(err)
	}
	if len(opts.Cities) != 15 || len(opts.Categories) != 6 || len(opts.Languages) != 2 || len(opts.Genders) != 2 ||
		len(opts.Events) != 2 || len(opts.Metrics) != 2 {
		t.Fatalf("unexpected option counts: %+v", opts)
	}
	if opts.Limits.MaxWithinDays != 365 || opts.Limits.MaxConditions != 10 || opts.Limits.MaxSpent != 1000000 {
		t.Errorf("limits = %+v", opts.Limits)
	}
	labels := map[string]string{}
	for _, group := range [][]segment.Option{opts.Languages, opts.Genders, opts.Categories, opts.Events, opts.Metrics} {
		for _, o := range group {
			labels[o.Value] = o.Label
		}
	}
	for value, want := range map[string]string{"en": "İngilizce", "female": "Kadın", "electronics": "Elektronik", "home": "Ev",
		"cart_add": "Sepete ürün ekleme", "total_spent": "Toplam harcama (TL)"} {
		if labels[value] != want {
			t.Errorf("label of %q = %q, want %q", value, labels[value], want)
		}
	}

	// One rule that uses a value from every list, at the extremes of the limits.
	for _, city := range opts.Cities[:14] { // all 15 would be "filters nothing"
		rule := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
			{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{city.Value}},
			{Type: segment.TypeAttribute, Field: "language", Op: segment.OpIn, Values: []string{opts.Languages[0].Value}},
			{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpNotIn, Values: []string{opts.Genders[1].Value}},
			{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: opts.Limits.MinAge, Max: opts.Limits.MaxAge},
			{Type: segment.TypeEvent, Event: opts.Events[0].Value, Category: opts.Categories[5].Value,
				WithinDays: opts.Limits.MaxWithinDays, Exists: true, MinCount: opts.Limits.MaxMinCount},
			{Type: segment.TypeSignup, Op: segment.OpOlderThan, Days: opts.Limits.MaxSignupDays},
			{Type: segment.TypeMetric, Metric: opts.Metrics[0].Value, Op: segment.OpLTE, Value: opts.Limits.MaxSpent},
			{Type: segment.TypeMetric, Metric: opts.Metrics[1].Value, Op: segment.OpGTE, Value: opts.Limits.MaxOrders, WithinDays: opts.Limits.MinWithinDays},
		}}
		if err := rule.Validate(); err != nil {
			t.Fatalf("a rule built from the options was rejected (%s): %v", city.Value, err)
		}
	}
}
