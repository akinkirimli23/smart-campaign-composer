// Package llm is the seam between the application and a language model.
//
// Callers describe what they want as a Request with a JSON schema and get JSON
// text back. They never trust that text: each caller parses and validates it
// and, on failure, appends the errors to the conversation and asks again.
// Keeping the model behind this interface is what lets every test run without
// an API key.
package llm

import (
	"context"
	"errors"
	"sync"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role Role
	Text string
	// Raw is the provider's own representation of an assistant turn, as returned
	// in Response.Raw. A client replays it unchanged when present, so
	// provider-specific blocks survive a retry.
	Raw any
}

type Request struct {
	// Task names the kind of work (e.g. "segment.generate"). Real clients ignore
	// it; the mock uses it to pick a canned behaviour.
	Task       string
	System     string
	Messages   []Message
	SchemaName string
	Schema     map[string]any // JSON schema the reply must follow
}

type Response struct {
	Text string // the model's JSON reply
	Raw  any    // see Message.Raw
}

// Client completes one request. Implementations must be safe for concurrent use.
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// Scripted is a Client for tests: it returns Replies in order and records every
// request, so a test can assert on what was sent back to the model after a
// rejected reply.
type Scripted struct {
	Replies []string

	mu       sync.Mutex
	requests []Request
}

func (s *Scripted) Complete(_ context.Context, req Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.requests)
	s.requests = append(s.requests, req)
	if n >= len(s.Replies) {
		return Response{}, errors.New("llm.Scripted: no reply left")
	}
	return Response{Text: s.Replies[n]}, nil
}

// Requests returns the requests received so far.
func (s *Scripted) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}
