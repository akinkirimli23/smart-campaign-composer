package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
)

// ContentGenerator writes validated campaign content, reporting progress.
type ContentGenerator interface {
	Generate(ctx context.Context, brief string, max content.VarLengths, emit func(content.Event)) (content.Result, error)
}

// VarLengthSource reports the longest value each template variable can take.
type VarLengthSource interface {
	MaxLengths(ctx context.Context) (content.VarLengths, error)
}

const contentTimeout = 3 * time.Minute // up to three model attempts

// contentResult is the payload of the final "result" event: the only content
// in the stream that passed validation.
type contentResult struct {
	Content   content.Content    `json:"content"`
	Lengths   content.Lengths    `json:"lengths"`
	Limits    content.Lengths    `json:"limits"`
	Variables content.VarLengths `json:"variables"`
	Attempts  int                `json:"attempts"`
}

// POST /api/content/generate {"brief": "..."}
//
// Replies with a server-sent event stream (DECISIONS 18):
//
//	attempt  {"attempt":1}                       a (new) attempt starts; clear the text shown so far
//	delta    {"field":"sms","text":"Mer"}        more text of one field, not validated yet
//	rejected {"attempt":1,"errors":[...]}        the attempt failed validation
//	result   {"content":{...},"lengths":{...}}   validated content; the stream ends
//	error    {"code":"...","message":"..."}      generation failed; the stream ends
//
// Problems found before the model is called are ordinary JSON error responses.
func (s *Server) handleContentGenerate(w http.ResponseWriter, r *http.Request) {
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
	case n > content.MaxBriefRunes:
		writeError(w, http.StatusBadRequest, "invalid_request", "brief en fazla 2000 karakter olabilir", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), contentTimeout)
	defer cancel()

	max, err := s.varLengths.MaxLengths(ctx)
	if err != nil {
		slog.Error("variable lengths", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Değişken uzunlukları hesaplanamadı", nil)
		return
	}

	stream := newEventStream(w)
	out, err := s.content.Generate(ctx, brief, max, func(e content.Event) {
		switch e.Kind {
		case content.EventAttempt:
			stream.send("attempt", map[string]any{"attempt": e.Attempt})
		case content.EventDelta:
			stream.send("delta", map[string]any{"field": e.Field, "text": e.Text})
		case content.EventRejected:
			stream.send("rejected", map[string]any{"attempt": e.Attempt, "errors": e.Errors})
		}
	})

	var invalid *content.InvalidOutputError
	switch {
	case errors.As(err, &invalid):
		stream.send("error", map[string]any{
			"code": "llm_output_invalid", "message": "Model limitlere uyan bir içerik üretemedi",
			"details": invalid.Errors, "attempts": invalid.Attempts,
		})
	case err != nil:
		if r.Context().Err() != nil {
			return // the client went away; nobody to tell
		}
		slog.Error("content generation failed", "err", err)
		stream.send("error", map[string]any{"code": "llm_unavailable", "message": "Model şu anda yanıt veremiyor"})
	default:
		stream.send("result", contentResult{
			Content: out.Content, Lengths: out.Lengths,
			Limits:    content.Lengths{SMS: content.MaxSMS, EmailSubject: content.MaxEmailSubject},
			Variables: max, Attempts: out.Attempts,
		})
	}
}

// eventStream writes server-sent events and flushes after each one.
type eventStream struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newEventStream(w http.ResponseWriter) *eventStream {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // tell a reverse proxy not to buffer the stream
	w.WriteHeader(http.StatusOK)
	s := &eventStream{w: w, rc: http.NewResponseController(w)}
	_ = s.rc.Flush()
	return s
}

// send writes one event. json.Marshal never emits a raw newline, so the
// payload always fits on a single data line. Write errors mean the client
// disconnected; the request context ends the generation.
func (s *eventStream) send(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("encode event", "event", event, "err", err)
		return
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return
	}
	_ = s.rc.Flush()
}
