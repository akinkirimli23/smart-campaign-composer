package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/campaign"
)

// WebhookSecretHeader carries the secret the provider shares with us.
const WebhookSecretHeader = "X-Webhook-Secret"

// Unsubscriber records that a recipient no longer wants a channel.
type Unsubscriber interface {
	Unsubscribe(ctx context.Context, campaignID, userID int64, channel string, now time.Time) (bool, error)
}

// POST /api/provider/events {"type": "unsubscribed", "reference": "campaign-7-user-12-sms"}
//
// The provider calls this when something happens to a message after it was
// delivered. The only event so far: the recipient unsubscribed (texted STOP,
// clicked the link in the email). reference is the idempotency key we sent
// with the message, handed back: it names the campaign, the person and the
// channel exactly, so nobody is looked up by phone number or address.
//
// The caller must present the shared secret when one is configured. Repeating
// an event is harmless.
func (s *Server) handleProviderEvent(w http.ResponseWriter, r *http.Request) {
	if s.webhookSecret != "" &&
		subtle.ConstantTimeCompare([]byte(r.Header.Get(WebhookSecretHeader)), []byte(s.webhookSecret)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "geçersiz webhook anahtarı", nil)
		return
	}
	var req struct {
		Type      string `json:"type"`
		Reference string `json:"reference"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Type != "unsubscribed" {
		writeError(w, http.StatusBadRequest, "invalid_request", "bilinmeyen olay türü", nil)
		return
	}
	campaignID, userID, channel, ok := parseReference(req.Reference)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "reference tanınmadı", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	added, err := s.campaigns.Unsubscribe(ctx, campaignID, userID, channel, s.now())
	switch {
	case errors.Is(err, campaign.ErrNotDelivered):
		// Not a message of ours that was delivered: nothing to unsubscribe from.
		writeError(w, http.StatusNotFound, "not_found", "Bu referansla teslim edilmiş bir mesaj yok", nil)
	case err != nil:
		slog.Error("unsubscribe", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Olay kaydedilemedi", nil)
	default:
		if added {
			s.metrics.Inc(Unsubscribes, "channel", channel)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "recorded": added})
	}
}

// Unsubscribes counts recipients who unsubscribed, by channel.
const Unsubscribes = "scc_unsubscribes_total"

// parseReference reads an idempotency key back: "campaign-7-user-12-sms".
// Anything that does not have exactly this shape is refused.
func parseReference(ref string) (campaignID, userID int64, channel string, ok bool) {
	if len(ref) > 80 {
		return 0, 0, "", false
	}
	if n, err := fmt.Sscanf(ref, "campaign-%d-user-%d-%s", &campaignID, &userID, &channel); err != nil || n != 3 {
		return 0, 0, "", false
	}
	if campaignID <= 0 || userID <= 0 || fmt.Sprintf("campaign-%d-user-%d-%s", campaignID, userID, channel) != ref {
		return 0, 0, "", false
	}
	return campaignID, userID, channel, true
}
