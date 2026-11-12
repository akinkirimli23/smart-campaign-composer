package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
)

// ContentGenerator writes validated campaign content, reporting progress.
type ContentGenerator interface {
	// GenerateIn writes the texts in one language ("" leaves it to the model).
	GenerateIn(ctx context.Context, brief, language string, max content.VarLengths, emit func(content.Event)) (content.Result, error)
	// Localize writes the texts in the other languages, all in one model call.
	Localize(ctx context.Context, brief string, source content.Content, from string, to []string,
		max content.VarLengths, emit func(content.Event)) (content.Localization, error)
}

// VarLengthSource reports the longest value each template variable can take.
type VarLengthSource interface {
	MaxLengths(ctx context.Context) (content.VarLengths, error)
}

const contentTimeout = 3 * time.Minute // up to three model attempts

var contentLimits = content.Lengths{SMS: content.MaxSMS, EmailSubject: content.MaxEmailSubject}

// contentResult is the payload of the final "result" event: the only content
// in the stream that passed validation.
type contentResult struct {
	Language  string             `json:"language,omitempty"`
	Content   content.Content    `json:"content"`
	Lengths   content.Lengths    `json:"lengths"`
	Limits    content.Lengths    `json:"limits"`
	Variables content.VarLengths `json:"variables"`
	Attempts  int                `json:"attempts"`
}

// localizeResult is the "result" event of a localisation: validated texts
// per target language.
type localizeResult struct {
	Variants  map[string]content.Content `json:"variants"`
	Lengths   map[string]content.Lengths `json:"lengths"`
	Limits    content.Lengths            `json:"limits"`
	Variables content.VarLengths         `json:"variables"`
	Attempts  int                        `json:"attempts"`
}

// POST /api/content/generate {"brief": "...", "language": "tr"}
//
// language is optional: the language to write in, normally the one most of
// the audience reads (DECISIONS 30).
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
		Brief    string `json:"brief"`
		Language string `json:"language"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	brief, ok := contentBrief(w, req.Brief)
	if !ok {
		return
	}
	if _, known := content.LanguageByCode(req.Language); req.Language != "" && !known {
		writeError(w, http.StatusBadRequest, "invalid_request", "bilinmeyen dil: "+req.Language, nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), contentTimeout)
	defer cancel()
	max, ok := s.maxLengths(ctx, w)
	if !ok {
		return
	}

	stream := newEventStream(w)
	out, err := s.content.GenerateIn(ctx, brief, req.Language, max, stream.progress)
	if stream.failed(r, err, "content generation failed") {
		return
	}
	stream.send("result", contentResult{
		Language: req.Language, Content: out.Content, Lengths: out.Lengths,
		Limits: contentLimits, Variables: max, Attempts: out.Attempts,
	})
}

// POST /api/content/localize {"brief": "...", "content": {...}, "from": "tr", "to": ["en"]}
//
// Writes the campaign texts in the languages in "to", starting from "content"
// (the texts in the language "from", as the marketer left them). However
// many languages are asked for, it is one model call (DECISIONS 30).
//
// The event stream is that of /api/content/generate, except that a delta
// names its language ({"language":"en","field":"sms","text":"Hi"}) and the
// result carries one content per language:
//
//	result {"variants":{"en":{...}},"lengths":{"en":{...}}, ...}
func (s *Server) handleContentLocalize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Brief   string          `json:"brief"`
		Content json.RawMessage `json:"content"`
		From    string          `json:"from"`
		To      []string        `json:"to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	brief, ok := contentBrief(w, req.Brief)
	if !ok {
		return
	}
	if _, known := content.LanguageByCode(req.From); !known {
		writeError(w, http.StatusBadRequest, "invalid_request", "from bilinen bir dil olmalı", nil)
		return
	}
	if len(req.To) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "to en az bir dil içermeli", nil)
		return
	}
	for i, code := range req.To {
		_, known := content.LanguageByCode(code)
		if !known || code == req.From || slices.Contains(req.To[:i], code) {
			writeError(w, http.StatusBadRequest, "invalid_request",
				"to yalnızca bilinen, birbirinden ve kaynak dilden farklı dilleri içerebilir", nil)
			return
		}
	}
	if req.Content == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "content alanı zorunlu", nil)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), contentTimeout)
	defer cancel()
	max, ok := s.maxLengths(ctx, w)
	if !ok {
		return
	}
	// The source is what the other languages are written from; a text that
	// is itself invalid is not worth a model call.
	source, _, err := content.Parse(req.Content, max)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "content_invalid", "İçerik geçersiz", err)
		return
	}

	stream := newEventStream(w)
	out, err := s.content.Localize(ctx, brief, source, req.From, req.To, max, stream.progress)
	if stream.failed(r, err, "content localisation failed") {
		return
	}
	stream.send("result", localizeResult{
		Variants: out.Variants, Lengths: out.Lengths,
		Limits: contentLimits, Variables: max, Attempts: out.Attempts,
	})
}

// contentBrief trims and bounds the brief; on failure it writes the error.
func contentBrief(w http.ResponseWriter, raw string) (string, bool) {
	brief := strings.TrimSpace(raw)
	switch n := utf8.RuneCountInString(brief); {
	case n == 0:
		writeError(w, http.StatusBadRequest, "invalid_request", "brief boş olamaz", nil)
		return "", false
	case n > content.MaxBriefRunes:
		writeError(w, http.StatusBadRequest, "invalid_request", "brief en fazla 2000 karakter olabilir", nil)
		return "", false
	}
	return brief, true
}

func (s *Server) maxLengths(ctx context.Context, w http.ResponseWriter) (content.VarLengths, bool) {
	max, err := s.varLengths.MaxLengths(ctx)
	if err != nil {
		slog.Error("variable lengths", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Değişken uzunlukları hesaplanamadı", nil)
		return nil, false
	}
	return max, true
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

// progress forwards a generator's progress events to the client.
func (s *eventStream) progress(e content.Event) {
	switch e.Kind {
	case content.EventAttempt:
		s.send("attempt", map[string]any{"attempt": e.Attempt})
	case content.EventDelta:
		delta := map[string]any{"field": e.Field, "text": e.Text}
		if e.Language != "" {
			delta["language"] = e.Language
		}
		s.send("delta", delta)
	case content.EventRejected:
		s.send("rejected", map[string]any{"attempt": e.Attempt, "errors": e.Errors})
	}
}

// failed reports a generation error to the client as the final "error" event
// and says whether there was one.
func (s *eventStream) failed(r *http.Request, err error, logMessage string) bool {
	var invalid *content.InvalidOutputError
	switch {
	case err == nil:
		return false
	case errors.As(err, &invalid):
		s.send("error", map[string]any{
			"code": "llm_output_invalid", "message": "Model limitlere uyan bir içerik üretemedi",
			"details": invalid.Errors, "attempts": invalid.Attempts,
		})
	case r.Context().Err() != nil:
		// the client went away; nobody to tell
	default:
		slog.Error(logMessage, "err", err)
		s.send("error", map[string]any{"code": "llm_unavailable", "message": "Model şu anda yanıt veremiyor"})
	}
	return true
}
