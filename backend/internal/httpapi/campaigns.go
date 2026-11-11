package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// CampaignStore creates campaigns and reports their delivery progress.
type CampaignStore interface {
	Create(ctx context.Context, brief string, rule segment.Rule, c content.Content, asOf time.Time) (campaign.Created, error)
	Status(ctx context.Context, id int64) (campaign.Status, error)
}

// POST /api/campaigns {"brief": "...", "rule": {...}, "content": {...}}
//
// This is the approval. Whatever the marketer saw or edited in the browser,
// the rule and the texts are validated again here, by the same code that
// checks the model's output, before anything is queued.
func (s *Server) handleCampaignCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Brief   string          `json:"brief"`
		Rule    json.RawMessage `json:"rule"`
		Content json.RawMessage `json:"content"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Rule == nil || req.Content == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "rule ve content alanları zorunlu", nil)
		return
	}
	brief := strings.TrimSpace(req.Brief)
	if utf8.RuneCountInString(brief) > segment.MaxBriefRunes {
		writeError(w, http.StatusBadRequest, "invalid_request", "brief en fazla 2000 karakter olabilir", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	rule, err := segment.Parse(req.Rule)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "rule_invalid", "Kitle kuralı geçersiz", err)
		return
	}
	max, err := s.varLengths.MaxLengths(ctx)
	if err != nil {
		slog.Error("variable lengths", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Değişken uzunlukları hesaplanamadı", nil)
		return
	}
	c, _, err := content.Parse(req.Content, max)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "content_invalid", "İçerik geçersiz", err)
		return
	}

	created, err := s.campaigns.Create(ctx, brief, rule, c, s.now())
	switch {
	case errors.Is(err, campaign.ErrEmptyAudience):
		writeError(w, http.StatusUnprocessableEntity, "audience_empty", "Bu kurala uyan ve ulaşılabilen kimse yok", nil)
	case err != nil:
		slog.Error("create campaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Kampanya oluşturulamadı", nil)
	default:
		writeJSON(w, http.StatusCreated, created)
	}
}

// GET /api/campaigns/{id}: message counts per channel and status.
func (s *Server) handleCampaignStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz kampanya numarası", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	st, err := s.campaigns.Status(ctx, id)
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Kampanya bulunamadı", nil)
	case err != nil:
		slog.Error("campaign status", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Kampanya durumu okunamadı", nil)
	default:
		writeJSON(w, http.StatusOK, st)
	}
}
