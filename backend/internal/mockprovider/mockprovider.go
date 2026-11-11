// Package mockprovider imitates an SMS / email provider's HTTP API, including
// the ways a real one misbehaves: latency, 5xx errors, 429 with Retry-After,
// and responses that are lost after the message was accepted. Nothing is ever
// sent to a real person.
//
//	POST /v1/sms/messages     {"to": "+90...", "body": "..."}
//	POST /v1/email/messages   {"to": "a@b", "subject": "...", "body": "..."}
//	     header Idempotency-Key: required; a repeated key returns the original
//	     message id and delivers nothing new
//	GET  /v1/messages?limit=N recently accepted messages, newest first
//	GET  /stats               counters per channel
//	GET  /healthz
package mockprovider

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Config sets how often and how the provider misbehaves. Rates are
// probabilities per request in [0, 1].
type Config struct {
	ErrorRate     float64       // answer 500 / 503 without accepting
	RateLimitRate float64       // answer 429 with Retry-After
	LostReplyRate float64       // accept the message, then answer 504
	MinLatency    time.Duration // every request takes between Min and Max
	MaxLatency    time.Duration
	RetryAfter    time.Duration // advertised on 429, rounded up to whole seconds
	Seed          uint64        // same seed, same sequence of faults
	Keep          int           // accepted messages remembered for inspection; 0 means 200
}

// Message is one accepted message.
type Message struct {
	ID         string    `json:"id"`
	Channel    string    `json:"channel"`
	To         string    `json:"to"`
	Subject    string    `json:"subject,omitempty"`
	Body       string    `json:"body"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// Stats counts what happened on one channel.
type Stats struct {
	Accepted    int `json:"accepted"`     // distinct messages delivered
	Duplicates  int `json:"duplicates"`   // repeated idempotency keys, not delivered again
	RateLimited int `json:"rate_limited"` // 429 responses
	Errors      int `json:"errors"`       // 5xx responses without delivery
	LostReplies int `json:"lost_replies"` // delivered, but the client was told 504
	Rejected    int `json:"rejected"`     // invalid requests (4xx)
}

const (
	maxSMS    = 160
	maxBodyKB = 64
)

type Server struct {
	cfg Config
	mux *http.ServeMux

	mu     sync.Mutex
	rng    *rand.Rand
	byKey  map[string]string // idempotency key -> message id
	recent []Message
	stats  map[string]*Stats
	nextID int
}

func New(cfg Config) *Server {
	if cfg.Keep <= 0 {
		cfg.Keep = 200
	}
	s := &Server{
		cfg:   cfg,
		mux:   http.NewServeMux(),
		rng:   rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15)),
		byKey: map[string]string{},
		stats: map[string]*Stats{"sms": {}, "email": {}},
	}
	s.mux.HandleFunc("POST /v1/{channel}/messages", s.handleSend)
	s.mux.HandleFunc("GET /v1/messages", s.handleRecent)
	s.mux.HandleFunc("GET /stats", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, s.Stats()) })
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Stats returns a copy of the counters.
func (s *Server) Stats() map[string]Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Stats{}
	for ch, st := range s.stats {
		out[ch] = *st
	}
	return out
}

// Messages returns every accepted message still remembered, oldest first.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.recent...)
}

type fault int

const (
	faultNone fault = iota
	faultError
	faultRateLimit
	faultLostReply
)

// roll decides this request's latency and fault. Callers hold s.mu.
func (s *Server) roll() (time.Duration, fault) {
	latency := s.cfg.MinLatency
	if spread := s.cfg.MaxLatency - s.cfg.MinLatency; spread > 0 {
		latency += time.Duration(s.rng.Int64N(int64(spread)))
	}
	switch p := s.rng.Float64(); {
	case p < s.cfg.RateLimitRate:
		return latency, faultRateLimit
	case p < s.cfg.RateLimitRate+s.cfg.ErrorRate:
		return latency, faultError
	case p < s.cfg.RateLimitRate+s.cfg.ErrorRate+s.cfg.LostReplyRate:
		return latency, faultLostReply
	}
	return latency, faultNone
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	if channel != "sms" && channel != "email" {
		writeError(w, http.StatusNotFound, "unknown channel")
		return
	}
	reject := func(status int, msg string) {
		s.mu.Lock()
		s.stats[channel].Rejected++
		s.mu.Unlock()
		writeError(w, status, msg)
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		reject(http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}
	var in struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyKB<<10)).Decode(&in); err != nil {
		reject(http.StatusBadRequest, "body is not valid JSON")
		return
	}
	switch {
	case strings.TrimSpace(in.To) == "":
		reject(http.StatusUnprocessableEntity, "to is required")
		return
	case strings.TrimSpace(in.Body) == "":
		reject(http.StatusUnprocessableEntity, "body is required")
		return
	case channel == "sms" && utf8.RuneCountInString(in.Body) > maxSMS:
		reject(http.StatusUnprocessableEntity, fmt.Sprintf("sms body exceeds %d characters", maxSMS))
		return
	case channel == "email" && strings.TrimSpace(in.Subject) == "":
		reject(http.StatusUnprocessableEntity, "subject is required")
		return
	case strings.Contains(in.Body, "{{") || strings.Contains(in.Subject, "{{"):
		// A real provider would happily send this; the mock catches a sender
		// that forgot to fill its templates.
		reject(http.StatusUnprocessableEntity, "unfilled template variable")
		return
	}

	s.mu.Lock()
	latency, f := s.roll()
	s.mu.Unlock()
	select {
	case <-r.Context().Done():
		return
	case <-time.After(latency):
	}

	s.mu.Lock()
	st := s.stats[channel]
	// A replayed key returns the original result, whatever else would happen.
	if id, seen := s.byKey[key]; seen {
		st.Duplicates++
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "duplicate": true})
		return
	}
	switch f {
	case faultRateLimit:
		st.RateLimited++
		s.mu.Unlock()
		secs := int((s.cfg.RetryAfter + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	case faultError:
		st.Errors++
		status := http.StatusInternalServerError
		if s.rng.IntN(2) == 0 {
			status = http.StatusServiceUnavailable
		}
		s.mu.Unlock()
		writeError(w, status, "temporary provider failure")
		return
	}

	s.nextID++
	msg := Message{
		ID: fmt.Sprintf("mock-%s-%06d", channel, s.nextID), Channel: channel,
		To: in.To, Subject: in.Subject, Body: in.Body, AcceptedAt: time.Now().UTC(),
	}
	s.byKey[key] = msg.ID
	s.recent = append(s.recent, msg)
	if len(s.recent) > s.cfg.Keep {
		s.recent = s.recent[len(s.recent)-s.cfg.Keep:]
	}
	st.Accepted++
	if f == faultLostReply {
		st.LostReplies++
	}
	s.mu.Unlock()

	if f == faultLostReply {
		// The message went out, the confirmation did not. Only the idempotency
		// key keeps the sender's retry from delivering it twice.
		writeError(w, http.StatusGatewayTimeout, "upstream timeout")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": msg.ID})
}

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, s.cfg.Keep)
	}
	all := s.Messages()
	out := make([]Message, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, all[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
