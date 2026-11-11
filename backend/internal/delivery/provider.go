package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Outbound is one message handed to the provider.
type Outbound struct {
	// IdempotencyKey is the same for every attempt of a message. A provider
	// that already accepted the key must not deliver again; this is what makes
	// a retry after a lost response safe.
	IdempotencyKey string
	Channel        string // "sms" or "email"
	To             string
	Subject        string // email only
	Body           string
}

// Provider delivers one message and returns the provider's id for it.
type Provider interface {
	Send(ctx context.Context, m Outbound) (providerID string, err error)
}

type ErrorKind int

const (
	// Retryable: the provider or the network failed; trying again may work.
	Retryable ErrorKind = iota
	// RateLimited: the provider told us to slow down (HTTP 429).
	RateLimited
	// Permanent: the request itself was rejected; retrying cannot help.
	Permanent
)

// SendError classifies a failed send for the retry policy.
type SendError struct {
	Kind       ErrorKind
	Status     int           // HTTP status, 0 for transport errors
	RetryAfter time.Duration // RateLimited only; 0 when the provider gave none
	Message    string
}

func (e *SendError) Error() string {
	if e.Status == 0 {
		return e.Message
	}
	return fmt.Sprintf("provider status %d: %s", e.Status, e.Message)
}

// HTTPProvider talks to the SMS / email provider's HTTP API.
type HTTPProvider struct {
	BaseURL string
	Client  *http.Client // nil means http.DefaultClient; the worker sets per-send deadlines
}

func (p HTTPProvider) Send(ctx context.Context, m Outbound) (string, error) {
	payload := map[string]string{"to": m.To, "body": m.Body}
	if m.Channel == "email" {
		payload["subject"] = m.Subject
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", &SendError{Kind: Permanent, Message: err.Error()}
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/v1/" + m.Channel + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", &SendError{Kind: Permanent, Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", m.IdempotencyKey)

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		// Timeouts and connection errors: we do not know whether the provider
		// got the message. Retrying with the same key is the safe answer.
		return "", &SendError{Kind: Retryable, Message: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var out struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &out) != nil || out.ID == "" {
			return "", &SendError{Kind: Retryable, Status: resp.StatusCode, Message: "response has no message id"}
		}
		return out.ID, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", &SendError{Kind: RateLimited, Status: resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")), Message: "rate limited"}
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout:
		return "", &SendError{Kind: Retryable, Status: resp.StatusCode, Message: snippet(raw)}
	default:
		return "", &SendError{Kind: Permanent, Status: resp.StatusCode, Message: snippet(raw)}
	}
}

// parseRetryAfter reads the delay-seconds form of Retry-After. The HTTP-date
// form and garbage yield 0; the worker then falls back to its own delay.
func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
