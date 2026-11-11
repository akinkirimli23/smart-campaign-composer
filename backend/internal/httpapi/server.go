// Package httpapi exposes the HTTP API consumed by the Vue frontend.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the subset of the database pool the health checks need.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options are the Server's dependencies. Each is an interface so handlers can
// be tested without a database or an LLM.
type Options struct {
	DB        Pinger
	Segments  SegmentGenerator
	Audience  AudienceCounter
	Content   ContentGenerator
	Campaigns CampaignStore
	// VarLengths supplies the worst-case length of each template variable.
	VarLengths VarLengthSource
	// Now is the clock used as the "as of" time for audience queries.
	// Defaults to time.Now; tests pin it.
	Now func() time.Time
}

type Server struct {
	db         Pinger
	segments   SegmentGenerator
	audience   AudienceCounter
	content    ContentGenerator
	varLengths VarLengthSource
	campaigns  CampaignStore
	now        func() time.Time
	mux        *http.ServeMux
}

func New(o Options) *Server {
	if o.Now == nil {
		o.Now = time.Now
	}
	s := &Server{
		db: o.DB, segments: o.Segments, audience: o.Audience,
		content: o.Content, varLengths: o.VarLengths, campaigns: o.Campaigns,
		now: o.Now, mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Liveness: the process is up and serving HTTP. Never touches dependencies,
	// so an orchestrator does not restart us just because Postgres is slow.
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// Readiness: dependencies are reachable and we can do useful work.
	s.mux.HandleFunc("GET /readyz", s.handleReady)

	s.mux.HandleFunc("POST /api/segments/generate", s.handleSegmentGenerate)
	s.mux.HandleFunc("POST /api/segments/count", s.handleSegmentCount)
	s.mux.HandleFunc("POST /api/content/generate", s.handleContentGenerate)
	s.mux.HandleFunc("POST /api/campaigns", s.handleCampaignCreate)
	s.mux.HandleFunc("GET /api/campaigns/{id}", s.handleCampaignStatus)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{"database": "ok"}
	status := http.StatusOK
	if err := s.db.Ping(ctx); err != nil {
		checks["database"] = err.Error()
		status = http.StatusServiceUnavailable
	}
	body := map[string]any{"status": "ok", "checks": checks}
	if status != http.StatusOK {
		body["status"] = "unavailable"
	}
	writeJSON(w, status, body)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(rec, r)

	level := slog.LevelInfo
	if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		// Polled every few seconds by the container healthcheck; logging these
		// at info would drown out real traffic.
		level = slog.LevelDebug
	}
	slog.Log(r.Context(), level, "http",
		"method", r.Method, "path", r.URL.Path, "status", rec.status,
		"dur_ms", float64(time.Since(start).Microseconds())/1000)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush lets handlers that type-assert http.Flusher stream through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer, so handlers
// can use SetWriteDeadline, EnableFullDuplex etc. through the recorder.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}
