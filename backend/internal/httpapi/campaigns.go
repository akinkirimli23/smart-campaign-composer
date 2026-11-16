package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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
	Create(ctx context.Context, brief string, rule segment.Rule, c content.Localized, asOf, sendAt time.Time) (campaign.Created, error)
	Cancel(ctx context.Context, id int64, now time.Time) (campaign.Status, error)
	Status(ctx context.Context, id int64) (campaign.Status, error)
	Totals(ctx context.Context) (campaign.Totals, error)
	List(ctx context.Context, now time.Time) ([]campaign.Summary, error)
	Recipients(ctx context.Context, id int64, f campaign.RecipientFilter, limit, offset int) (campaign.RecipientPage, error)
	EachRecipient(ctx context.Context, id int64, f campaign.RecipientFilter, fn func(campaign.Recipient) error) error
	Unsubscriber
}

// MaxScheduleAhead bounds how far in the future a campaign may be scheduled;
// the same bound applies to a send time suggested from the brief.
const MaxScheduleAhead = segment.MaxScheduleAhead

// POST /api/campaigns {"brief": "...", "rule": {...}, "content": {...}, "send_at": "2026-11-14T09:00:00Z"}
//
// send_at is optional (RFC 3339); without it the campaign is sent at once.
// content is either one set of texts ({"sms": ..., "email": {...}}) or one set
// per language ({"default": "tr", "variants": {"tr": {...}, "en": {...}}});
// each recipient gets the set of their own language, or the default one when
// the campaign has none for them (DECISIONS 30).
//
// This is the approval. Whatever the marketer saw or edited in the browser,
// the rule and the texts are validated again here, by the same code that
// checks the model's output, before anything is queued.
func (s *Server) handleCampaignCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Brief   string          `json:"brief"`
		Rule    json.RawMessage `json:"rule"`
		Content json.RawMessage `json:"content"`
		SendAt  *time.Time      `json:"send_at"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Rule == nil || req.Content == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "rule ve content alanları zorunlu", nil)
		return
	}
	now := s.now()
	var sendAt time.Time
	if req.SendAt != nil {
		sendAt = *req.SendAt
		switch {
		// A minute of slack: the clock moved on while the marketer was reading.
		case sendAt.Before(now.Add(-time.Minute)):
			writeError(w, http.StatusUnprocessableEntity, "schedule_invalid", "Gönderim zamanı geçmişte olamaz", nil)
			return
		case sendAt.After(now.Add(MaxScheduleAhead)):
			writeError(w, http.StatusUnprocessableEntity, "schedule_invalid", "Gönderim en fazla 30 gün sonrasına zamanlanabilir", nil)
			return
		}
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
	c, _, err := content.ParseLocalized(req.Content, max)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "content_invalid", "İçerik geçersiz", err)
		return
	}

	created, err := s.campaigns.Create(ctx, brief, rule, c, now, sendAt)
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

// GET /api/campaigns: recent campaigns, newest first, each with its state
// (scheduled, sending, done, cancelled) and message counts.
func (s *Server) handleCampaignList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := s.campaigns.List(ctx, s.now())
	if err != nil {
		slog.Error("list campaigns", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Kampanyalar okunamadı", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": list})
}

// GET /api/campaigns/{id}: message counts per channel and status.
func (s *Server) handleCampaignStatus(w http.ResponseWriter, r *http.Request) {
	if st, ok := s.campaignStatus(w, r); ok {
		writeJSON(w, http.StatusOK, st)
	}
}

// POST /api/campaigns/{id}/cancel: stops whatever has not been sent yet and
// returns the campaign's status. Safe to repeat.
func (s *Server) handleCampaignCancel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz kampanya numarası", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	st, err := s.campaigns.Cancel(ctx, id, s.now())
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Kampanya bulunamadı", nil)
	case err != nil:
		slog.Error("cancel campaign", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Kampanya iptal edilemedi", nil)
	default:
		writeJSON(w, http.StatusOK, st)
	}
}

// GET /api/campaigns/{id}/recipients?status=failed&channel=sms&language=en&city=Ankara&q=ayşe&offset=0&limit=50
//
// The people behind the dashboard's numbers: who each message is for and what
// became of it, the ones that need attention first. Every filter is optional;
// q searches the recipient's name. Addresses are masked.
func (s *Server) handleCampaignRecipients(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz kampanya numarası", nil)
		return
	}
	query := r.URL.Query()
	limit, okLimit := pageNumber(query.Get("limit"))
	offset, okOffset := pageNumber(query.Get("offset"))
	if !okLimit || !okOffset {
		writeError(w, http.StatusBadRequest, "invalid_request", "limit ve offset negatif olmayan tam sayı olmalı", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	page, err := s.campaigns.Recipients(ctx, id, recipientFilter(query), limit, offset)
	switch {
	case errors.Is(err, campaign.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Kampanya bulunamadı", nil)
	case errors.Is(err, campaign.ErrInvalidFilter):
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz süzgeç değeri", nil)
	case err != nil:
		slog.Error("campaign recipients", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Alıcılar okunamadı", nil)
	default:
		writeJSON(w, http.StatusOK, page)
	}
}

// recipientFilter reads the list's filters from the query string.
func recipientFilter(query url.Values) campaign.RecipientFilter {
	return campaign.RecipientFilter{
		Status: query.Get("status"), Channel: query.Get("channel"), Language: query.Get("language"),
		City: query.Get("city"), Query: query.Get("q"), Suppression: query.Get("suppression"),
	}
}

var csvStatus = map[string]string{
	"pending": "Bekliyor", "sending": "Gönderiliyor", "sent": "Teslim edildi",
	"failed": "Başarısız", "skipped": "Atlandı", "cancelled": "İptal edildi",
}

var csvSuppression = map[string]string{"unsubscribed": "Abonelikten çıktı", "invalid_address": "Adres geçersiz"}

// handleCampaignRecipientsCSV is the recipient list as a file (DECISIONS 44):
// the same rows, the same filters and the same masked addresses as the list
// on screen, plus the text each delivered message carried. It holds nothing
// the screen does not show, so downloading it moves no address out of the
// system.
func (s *Server) handleCampaignRecipientsCSV(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz kampanya numarası", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Headers go out with the first row, so an unknown campaign or a bad
	// filter can still be answered as a JSON error.
	var out *csv.Writer
	start := func() {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="kampanya-%d-alicilar.csv"`, id))
		w.Header().Set("Cache-Control", "no-store")
		// The byte order mark is what makes Excel read the file as UTF-8.
		_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
		out = csv.NewWriter(w)
		_ = out.Write([]string{"Ad", "Kanal", "Adres (gizli)", "Dil", "Şehir", "Durum", "Deneme", "Gönderim zamanı (UTC)", "Hata", "Bundan sonra", "Konu", "Metin"})
	}
	err = s.campaigns.EachRecipient(ctx, id, recipientFilter(r.URL.Query()), func(rec campaign.Recipient) error {
		if out == nil {
			start()
		}
		sentAt := ""
		if rec.SentAt != nil {
			sentAt = rec.SentAt.UTC().Format("2006-01-02 15:04:05")
		}
		channel := "SMS"
		if rec.Channel == campaign.ChannelEmail {
			channel = "Email"
		}
		return out.Write([]string{
			csvCell(rec.Name), channel, rec.Address, rec.Language, csvCell(rec.City), csvStatus[rec.Status],
			strconv.Itoa(rec.Attempts), sentAt, csvCell(rec.Error), csvSuppression[rec.Suppression], csvCell(rec.Subject), csvCell(rec.Text),
		})
	})
	switch {
	case out != nil:
		// Rows are already on their way; a failure now can only be logged.
		if err != nil {
			slog.Error("campaign recipients csv", "err", err)
		}
		out.Flush()
	case errors.Is(err, campaign.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Kampanya bulunamadı", nil)
	case errors.Is(err, campaign.ErrInvalidFilter):
		writeError(w, http.StatusBadRequest, "invalid_request", "geçersiz süzgeç değeri", nil)
	case err != nil:
		slog.Error("campaign recipients csv", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Alıcılar okunamadı", nil)
	default:
		start() // no rows: a file with only the header
		out.Flush()
	}
}

// csvCell keeps a spreadsheet from running a cell as a formula: text written
// by a marketer or a model ("=...", "+...") is data. The apostrophe is the
// spreadsheets' own way of saying so.
func csvCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// pageNumber reads an optional non-negative integer query parameter.
func pageNumber(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil && n >= 0 && n <= 1_000_000
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
