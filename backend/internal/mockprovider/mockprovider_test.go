package mockprovider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func send(t *testing.T, s *Server, channel, key, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/"+channel+"/messages", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

const sms = `{"to":"+905551112233","body":"Merhaba Ayşe"}`

func TestAcceptsAndDeduplicates(t *testing.T) {
	s := New(Config{})
	rec, first := send(t, s, "sms", "k1", sms)
	if rec.Code != http.StatusAccepted || first["id"] == "" {
		t.Fatalf("status %d, body %v", rec.Code, first)
	}
	// Same key again, even with a different body: the original id, no new delivery.
	rec, second := send(t, s, "sms", "k1", `{"to":"+905551112233","body":"farklı"}`)
	if rec.Code != http.StatusOK || second["id"] != first["id"] || second["duplicate"] != true {
		t.Fatalf("replay: status %d, body %v", rec.Code, second)
	}
	_, other := send(t, s, "sms", "k2", sms)
	if other["id"] == first["id"] {
		t.Error("a new key must get a new id")
	}
	st := s.Stats()["sms"]
	if st.Accepted != 2 || st.Duplicates != 1 || len(s.Messages()) != 2 {
		t.Errorf("stats = %+v, messages = %d", st, len(s.Messages()))
	}
	if s.Messages()[0].Body != "Merhaba Ayşe" {
		t.Errorf("stored body = %q", s.Messages()[0].Body)
	}
}

func TestRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name, channel, key, body string
		status                   int
	}{
		{"no key", "sms", "", sms, 400},
		{"not json", "sms", "k", `to=1`, 400},
		{"no recipient", "sms", "k", `{"body":"x"}`, 422},
		{"no body", "sms", "k", `{"to":"+90"}`, 422},
		{"sms too long", "sms", "k", `{"to":"+90","body":"` + strings.Repeat("ş", 161) + `"}`, 422},
		{"email without subject", "email", "k", `{"to":"a@b.c","body":"x"}`, 422},
		{"unfilled variable", "sms", "k", `{"to":"+90","body":"Merhaba {{first_name}}"}`, 422},
		{"unknown channel", "push", "k", sms, 404},
	}
	s := New(Config{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec, _ := send(t, s, tt.channel, tt.key, tt.body); rec.Code != tt.status {
				t.Errorf("status = %d, want %d", rec.Code, tt.status)
			}
		})
	}
	if rec, _ := send(t, s, "sms", "ok", `{"to":"+90","body":"`+strings.Repeat("ş", 160)+`"}`); rec.Code != http.StatusAccepted {
		t.Errorf("160 characters must be accepted, got %d", rec.Code)
	}
	if got := len(s.Messages()); got != 1 {
		t.Errorf("%d messages stored, want 1", got)
	}
}

func TestFaults(t *testing.T) {
	t.Run("rate limit", func(t *testing.T) {
		s := New(Config{RateLimitRate: 1, RetryAfter: 1500 * time.Millisecond})
		rec, _ := send(t, s, "sms", "k", sms)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "2" {
			t.Errorf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
		}
		if len(s.Messages()) != 0 || s.Stats()["sms"].RateLimited != 1 {
			t.Error("a rate-limited request must not be delivered")
		}
	})
	t.Run("error", func(t *testing.T) {
		s := New(Config{ErrorRate: 1})
		rec, _ := send(t, s, "email", "k", `{"to":"a@b.c","subject":"s","body":"b"}`)
		if rec.Code < 500 || len(s.Messages()) != 0 {
			t.Errorf("status %d, messages %d", rec.Code, len(s.Messages()))
		}
	})
	// The dangerous one: delivered, but the caller is told it failed. The
	// retry with the same key must not deliver again.
	t.Run("lost reply", func(t *testing.T) {
		s := New(Config{LostReplyRate: 1})
		rec, _ := send(t, s, "sms", "k", sms)
		if rec.Code != http.StatusGatewayTimeout || len(s.Messages()) != 1 {
			t.Fatalf("status %d, messages %d", rec.Code, len(s.Messages()))
		}
		rec, out := send(t, s, "sms", "k", sms)
		if rec.Code != http.StatusOK || out["duplicate"] != true || len(s.Messages()) != 1 {
			t.Errorf("retry: status %d, body %v, messages %d", rec.Code, out, len(s.Messages()))
		}
	})
}

// The same seed produces the same sequence of faults.
func TestFaultsAreDeterministic(t *testing.T) {
	run := func() string {
		s := New(Config{ErrorRate: 0.3, RateLimitRate: 0.2, LostReplyRate: 0.1, Seed: 7})
		var codes []string
		for i := range 60 {
			rec, _ := send(t, s, "sms", "k"+string(rune('A'+i)), sms)
			codes = append(codes, http.StatusText(rec.Code))
		}
		return strings.Join(codes, ",")
	}
	a, b := run(), run()
	if a != b {
		t.Error("two runs with the same seed differ")
	}
	for _, want := range []string{"Accepted", "Too Many Requests", "Gateway Timeout"} {
		if !strings.Contains(a, want) {
			t.Errorf("no %q in 60 requests: %s", want, a)
		}
	}
}

func TestRecentAndStatsEndpoints(t *testing.T) {
	s := New(Config{Keep: 3})
	for _, k := range []string{"a", "b", "c", "d"} {
		send(t, s, "sms", k, `{"to":"+90`+k+`","body":"x"}`)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/messages?limit=2", nil))
	var out struct{ Messages []Message }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 2 || out.Messages[0].To != "+90d" || out.Messages[1].To != "+90c" {
		t.Errorf("recent = %+v", out.Messages)
	}
	if len(s.Messages()) != 3 {
		t.Errorf("kept %d messages, want 3", len(s.Messages()))
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if !strings.Contains(rec.Body.String(), `"accepted":4`) {
		t.Errorf("stats = %s", rec.Body)
	}
}
