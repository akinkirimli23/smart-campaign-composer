package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/audience"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// SegmentGenerator turns a brief into a validated rule (or "unsupported").
type SegmentGenerator interface {
	Generate(ctx context.Context, brief string) (segment.Outcome, error)
}

// AudienceCounter counts the users a rule selects as of a point in time.
type AudienceCounter interface {
	Count(ctx context.Context, rule segment.Rule, asOf time.Time) (audience.Counts, error)
}

const (
	maxBodyBytes    = 64 << 10
	generateTimeout = 90 * time.Second
)

type segmentResponse struct {
	Status      string               `json:"status"` // "ok" | "unsupported"
	Rule        *segment.Rule        `json:"rule,omitempty"`
	Description *segment.Description `json:"description,omitempty"`
	Assumptions []segment.Assumption `json:"assumptions,omitempty"`
	Audience    *audience.Counts     `json:"audience,omitempty"`
	AsOf        *time.Time           `json:"as_of,omitempty"`
	Attempts    int                  `json:"attempts,omitempty"`
	Unsupported *segment.Unsupported `json:"unsupported,omitempty"`
}

// POST /api/segments/generate {"brief": "..."}
func (s *Server) handleSegmentGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Brief string `json:"brief"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	brief := strings.TrimSpace(req.Brief)
	switch n := utf8.RuneCountInString(brief); {
	case n == 0:
		writeError(w, http.StatusBadRequest, "invalid_request", "brief boş olamaz", nil)
		return
	case n > segment.MaxBriefRunes:
		writeError(w, http.StatusBadRequest, "invalid_request", "brief en fazla 2000 karakter olabilir", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), generateTimeout)
	defer cancel()

	out, err := s.segments.Generate(ctx, brief)
	var invalid *segment.InvalidOutputError
	switch {
	case errors.As(err, &invalid):
		// The model never produced a usable rule. Tell the client why instead of
		// guessing; nothing was run against the database.
		writeError(w, http.StatusUnprocessableEntity, "llm_output_invalid",
			"Model geçerli bir kitle kuralı üretemedi", invalid.Errors)
		return
	case err != nil:
		slog.Error("segment generation failed", "err", err)
		writeError(w, http.StatusBadGateway, "llm_unavailable", "Model şu anda yanıt veremiyor", nil)
		return
	}

	if out.Unsupported != nil {
		writeJSON(w, http.StatusOK, segmentResponse{Status: "unsupported", Unsupported: out.Unsupported, Attempts: out.Attempts})
		return
	}
	s.writeSegment(w, r, *out.Rule, out.Assumptions, out.Attempts)
}

// GET /api/segments/options: the allowed values and bounds of a rule, for the
// rule editor. The lists are the validator's own, so the form cannot offer a
// value the backend would reject.
func (s *Server) handleSegmentOptions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, segment.RuleOptions())
}

// POST /api/segments/count {"rule": {...}}
// Used when the marketer edits the proposed rule: the edited rule goes through
// the same validator as the model's output.
func (s *Server) handleSegmentCount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule json.RawMessage `json:"rule"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Rule == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "rule alanı zorunlu", nil)
		return
	}
	rule, err := segment.Parse(req.Rule)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "rule_invalid", "Kitle kuralı geçersiz", err)
		return
	}
	s.writeSegment(w, r, rule, nil, 0)
}

func (s *Server) writeSegment(w http.ResponseWriter, r *http.Request, rule segment.Rule, assumptions []segment.Assumption, attempts int) {
	asOf := s.now().UTC()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	counts, err := s.audience.Count(ctx, rule, asOf)
	if err != nil {
		slog.Error("audience count failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Kitle sayısı hesaplanamadı", nil)
		return
	}
	desc := segment.Describe(rule)
	writeJSON(w, http.StatusOK, segmentResponse{
		Status: "ok", Rule: &rule, Description: &desc, Assumptions: assumptions,
		Audience: &counts, AsOf: &asOf, Attempts: attempts,
	})
}

// readJSON decodes a bounded JSON body; on failure it writes the error response.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request", "istek gövdesi çok büyük", nil)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "istek gövdesi geçerli JSON değil", nil)
		return false
	}
	return true
}

// writeError sends the API's single error shape. details is usually a
// segment.ValidationError (a list of {path, message}).
func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	body := map[string]any{"error": map[string]any{"code": code, "message": message}}
	if details != nil {
		body["error"].(map[string]any)["details"] = details
	}
	writeJSON(w, status, body)
}
