package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/metrics"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// CampaignStore creates campaigns and reports their delivery progress.
type CampaignStore interface {
	Create(ctx context.Context, brief string, rule segment.Rule, c content.Content, asOf time.Time) (campaign.Created, error)
	Status(ctx context.Context, id int64) (campaign.Status, error)
	Totals(ctx context.Context) (campaign.Totals, error)
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
	if st, ok := s.campaignStatus(w, r); ok {
		writeJSON(w, http.StatusOK, st)
	}
}

// campaignStatus loads the status of the campaign in the path; on failure it
// writes the JSON error response.
func (s *Server) campaignStatus(w http.ResponseWriter, r *http.Request) (campaign.Status, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz kampanya numarası", nil)
		return campaign.Status{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	st, err := s.campaigns.Status(ctx, id)
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Kampanya bulunamadı", nil)
		return campaign.Status{}, false
	case err != nil:
		slog.Error("campaign status", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Kampanya durumu okunamadı", nil)
		return campaign.Status{}, false
	}
	return st, true
}

// GET /api/campaigns/{id}/events: the live delivery status as server-sent
// events (DECISIONS 23).
//
//	status {"id":1,"channels":{...},"done":false}   on connect, then whenever the counts change
//	done   {}                                        every message is final; the stream ends
//
// The stream re-reads the same query GET /api/campaigns/{id} uses, so the
// dashboard, that endpoint and /metrics cannot disagree.
func (s *Server) handleCampaignEvents(w http.ResponseWriter, r *http.Request) {
	st, ok := s.campaignStatus(w, r)
	if !ok {
		return
	}
	stream := newEventStream(w)
	stream.send("status", st)

	ticker := time.NewTicker(s.statusTick)
	defer ticker.Stop()
	last := st
	for !last.Done {
		select {
		case <-r.Context().Done():
			return // client left, or the server is shutting down
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		next, err := s.campaigns.Status(ctx, st.ID)
		cancel()
		if err != nil {
			if r.Context().Err() == nil {
				slog.Error("campaign status stream", "err", err)
				stream.send("error", map[string]any{"code": "internal", "message": "Kampanya durumu okunamadı"})
			}
			return
		}
		if !sameCounts(last, next) {
			stream.send("status", next)
		}
		last = next
	}
	stream.send("done", map[string]any{})
}

func sameCounts(a, b campaign.Status) bool {
	if a.Done != b.Done || len(a.Channels) != len(b.Channels) {
		return false
	}
	for ch, c := range a.Channels {
		if b.Channels[ch] != c {
			return false
		}
	}
	return true
}

// GET /metrics: Prometheus text format. Queue gauges are read from the
// database on every scrape; counters are this process's.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	totals, err := s.campaigns.Totals(ctx)
	if err != nil {
		slog.Error("metrics", "err", err)
		http.Error(w, "metrics unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprint(w, "# HELP scc_campaigns Approved campaigns.\n# TYPE scc_campaigns gauge\n")
	metrics.WriteSample(w, "scc_campaigns", "", int64(totals.Campaigns))
	fmt.Fprint(w, "# HELP scc_messages Messages in the queue table by channel and status.\n# TYPE scc_messages gauge\n")
	for _, ch := range campaign.Channels {
		for _, status := range campaign.Statuses {
			metrics.WriteSample(w, "scc_messages", metrics.Labels("channel", ch, "status", status), int64(totals.Messages[ch][status]))
		}
	}
	s.metrics.WriteCounters(w)
}
