package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProviderRequest(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	id, err := HTTPProvider{BaseURL: srv.URL + "/"}.Send(context.Background(), Outbound{
		IdempotencyKey: "campaign-1-user-2-email", Channel: "email", To: "a@example.com", Subject: "Konu", Body: "Gövde",
	})
	if err != nil || id != "abc" {
		t.Fatalf("id = %q, err = %v", id, err)
	}
	if gotPath != "/v1/email/messages" || gotKey != "campaign-1-user-2-email" {
		t.Errorf("path = %q, key = %q", gotPath, gotKey)
	}
	if gotBody["to"] != "a@example.com" || gotBody["subject"] != "Konu" || gotBody["body"] != "Gövde" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestHTTPProviderClassifiesFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter string
		body       string
		kind       ErrorKind
		wait       time.Duration
	}{
		{"server error", 500, "", `{"error":"boom"}`, Retryable, 0},
		{"unavailable", 503, "", ``, Retryable, 0},
		{"gateway timeout", 504, "", ``, Retryable, 0},
		{"rate limited", 429, "3", ``, RateLimited, 3 * time.Second},
		{"rate limited without header", 429, "", ``, RateLimited, 0},
		{"rate limited with a date", 429, "Wed, 11 Nov 2026 12:00:00 GMT", ``, RateLimited, 0},
		{"bad request", 400, "", `{"error":"to is required"}`, Permanent, 0},
		{"unprocessable", 422, "", ``, Permanent, 0},
		{"success without an id", 200, "", `{}`, Retryable, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			_, err := HTTPProvider{BaseURL: srv.URL}.Send(context.Background(), Outbound{Channel: "sms", To: "+90", Body: "x"})
			var sendErr *SendError
			if !errors.As(err, &sendErr) {
				t.Fatalf("err = %v, want *SendError", err)
			}
			if sendErr.Kind != tt.kind || sendErr.RetryAfter != tt.wait || sendErr.Status != tt.status {
				t.Errorf("got %+v", sendErr)
			}
		})
	}
}

// A dead connection or a timeout leaves delivery unknown: retry (with the
// same idempotency key) rather than give up or assume success.
func TestHTTPProviderTransportErrorsAreRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := HTTPProvider{BaseURL: url}.Send(context.Background(), Outbound{Channel: "sms", To: "+90", Body: "x"})
	var sendErr *SendError
	if !errors.As(err, &sendErr) || sendErr.Kind != Retryable || sendErr.Status != 0 {
		t.Fatalf("connection refused: %v", err)
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer slow.Close()
	defer close(release) // runs first: Close waits for the handler to return
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = HTTPProvider{BaseURL: slow.URL}.Send(ctx, Outbound{Channel: "sms", To: "+90", Body: "x"})
	if !errors.As(err, &sendErr) || sendErr.Kind != Retryable {
		t.Fatalf("timeout: %v", err)
	}
}
