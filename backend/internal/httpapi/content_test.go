package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
)

type fixedLengths struct {
	max content.VarLengths
	err error
}

func (f fixedLengths) MaxLengths(context.Context) (content.VarLengths, error) { return f.max, f.err }

var testLengths = fixedLengths{max: content.VarLengths{"first_name": 10, "last_name": 12, "city": 9}}

type sseEvent struct {
	name string
	data map[string]any
}

// parseSSE decodes a text/event-stream body and checks its framing.
func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, block := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		lines := strings.Split(block, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "event: ") || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("malformed event block %q", block)
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &data); err != nil {
			t.Fatalf("event data is not JSON: %q", lines[1])
		}
		events = append(events, sseEvent{strings.TrimPrefix(lines[0], "event: "), data})
	}
	return events
}

func postContent(t *testing.T, model llm.Client, lengths VarLengthSource, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := New(Options{DB: fakePinger{}, Content: content.Generator{LLM: model}, VarLengths: lengths})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/content/generate", strings.NewReader(body)))
	return rec
}

const (
	goodContent = `{"sms":"Merhaba {{first_name}}, %30 indirim seni bekliyor!","email":{"subject":"{{first_name}}, sepetin seni bekliyor","body":"Merhaba {{first_name}},\n\nİyi alışverişler!"}}`
	badContent  = `{"sms":"Merhaba {{name}}","email":{"subject":"s","body":"b"}}`
)

// A rejected first attempt and a valid second one, as the browser sees them.
func TestContentStream(t *testing.T) {
	rec := postContent(t, &llm.Scripted{Replies: []string{badContent, goodContent}}, testLengths, `{"brief":"%30 indirim hatırlatması"}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !rec.Flushed {
		t.Error("the stream was never flushed")
	}
	events := parseSSE(t, rec.Body.String())

	var order []string
	sms := map[int]string{}
	attempt := 0
	for _, e := range events {
		switch e.name {
		case "attempt":
			attempt = int(e.data["attempt"].(float64))
		case "delta":
			if e.data["field"] == content.FieldSMS {
				sms[attempt] += e.data["text"].(string)
			}
			continue
		}
		order = append(order, e.name)
	}
	if got := strings.Join(order, " "); got != "attempt rejected attempt result" {
		t.Fatalf("events = %s", got)
	}
	if sms[1] != "Merhaba {{name}}" || sms[2] != "Merhaba {{first_name}}, %30 indirim seni bekliyor!" {
		t.Errorf("streamed sms = %q", sms)
	}

	var rejected, result map[string]any
	for _, e := range events {
		switch e.name {
		case "rejected":
			rejected = e.data
		case "result":
			result = e.data
		}
	}
	first := rejected["errors"].([]any)[0].(map[string]any)
	if first["path"] != "sms" || !strings.Contains(first["message"].(string), "bilinmeyen bir değişken") {
		t.Errorf("rejected = %v", rejected)
	}
	if result["attempts"] != float64(2) {
		t.Errorf("attempts = %v", result["attempts"])
	}
	if got := result["content"].(map[string]any)["sms"]; got != sms[2] {
		t.Errorf("result sms = %v", got)
	}
	// What the UI needs for its live counter: measured lengths, limits, and
	// how many characters each variable counts for.
	lengths := result["lengths"].(map[string]any)
	limits := result["limits"].(map[string]any)
	if lengths["sms"] != float64(46) || limits["sms"] != float64(160) || limits["email_subject"] != float64(80) {
		t.Errorf("lengths = %v, limits = %v", lengths, limits)
	}
	if result["variables"].(map[string]any)["first_name"] != float64(10) {
		t.Errorf("variables = %v", result["variables"])
	}
}

// When the model never gets it right the stream ends with an error event
// carrying the last validation errors, and no result.
func TestContentStreamGivesUp(t *testing.T) {
	rec := postContent(t, &llm.Scripted{Replies: []string{badContent, badContent, badContent}}, testLengths, `{"brief":"x"}`)
	events := parseSSE(t, rec.Body.String())
	last := events[len(events)-1]
	if last.name != "error" || last.data["code"] != "llm_output_invalid" || last.data["attempts"] != float64(3) {
		t.Fatalf("last event = %+v", last)
	}
	if len(last.data["details"].([]any)) != 1 {
		t.Errorf("details = %v", last.data["details"])
	}
	for _, e := range events {
		if e.name == "result" {
			t.Error("invalid content must never be sent as a result")
		}
	}
}

func TestContentStreamLLMFailure(t *testing.T) {
	rec := postContent(t, &llm.Scripted{}, testLengths, `{"brief":"x"}`)
	events := parseSSE(t, rec.Body.String())
	last := events[len(events)-1]
	if last.name != "error" || last.data["code"] != "llm_unavailable" {
		t.Fatalf("last event = %+v", last)
	}
	if strings.Contains(rec.Body.String(), "Scripted") {
		t.Error("internal error text leaked to the client")
	}
}

// Problems known before the model is called are plain JSON errors, so the
// client can tell them apart from a stream by status code.
func TestContentRequestErrors(t *testing.T) {
	tests := []struct {
		name    string
		lengths VarLengthSource
		body    string
		status  int
	}{
		{"not json", testLengths, `brief`, http.StatusBadRequest},
		{"empty brief", testLengths, `{"brief":"  "}`, http.StatusBadRequest},
		{"brief too long", testLengths, `{"brief":"` + strings.Repeat("a", content.MaxBriefRunes+1) + `"}`, http.StatusBadRequest},
		{"lengths unavailable", fixedLengths{err: errors.New("db down")}, `{"brief":"x"}`, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &llm.Scripted{Replies: []string{goodContent}}
			rec := postContent(t, model, tt.lengths, tt.body)
			if rec.Code != tt.status || rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
			}
			if n := len(model.Requests()); n != 0 {
				t.Errorf("model was called %d times", n)
			}
		})
	}
}

const englishContent = `{"sms":"Hi {{first_name}}, 30% off is waiting for you!","email":{"subject":"{{first_name}}, your cart is waiting","body":"Hi {{first_name}},\n\nHappy shopping!"}}`

func postLocalize(t *testing.T, model llm.Client, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := New(Options{DB: fakePinger{}, Content: content.Generator{LLM: model}, VarLengths: testLengths})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/content/localize", strings.NewReader(body)))
	return rec
}

// The language the audience mostly reads is passed on to the model and
// echoed in the result.
func TestContentGenerateInALanguage(t *testing.T) {
	model := &llm.Scripted{Replies: []string{englishContent}}
	rec := postContent(t, model, testLengths, `{"brief":"30% off reminder","language":"en"}`)
	events := parseSSE(t, rec.Body.String())
	last := events[len(events)-1]
	if last.name != "result" || last.data["language"] != "en" {
		t.Fatalf("last event = %+v", last)
	}
	if !strings.Contains(model.Requests()[0].Messages[0].Text, "<language>en</language>") {
		t.Error("the language did not reach the model")
	}

	rec = postContent(t, &llm.Scripted{}, testLengths, `{"brief":"x","language":"de"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown language: status %d", rec.Code)
	}
}

// Localisation as the browser sees it: one model call, deltas that name
// their language, and a result with validated texts per language.
func TestContentLocalize(t *testing.T) {
	model := &llm.Scripted{Replies: []string{`{"en":` + englishContent + `}`}}
	rec := postLocalize(t, model, `{"brief":"%30 indirim","content":`+goodContent+`,"from":"tr","to":["en"]}`)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	events := parseSSE(t, rec.Body.String())

	sms := ""
	for _, e := range events {
		if e.name == "delta" && e.data["field"] == content.FieldSMS {
			if e.data["language"] != "en" {
				t.Fatalf("delta without its language: %v", e.data)
			}
			sms += e.data["text"].(string)
		}
	}
	if sms != "Hi {{first_name}}, 30% off is waiting for you!" {
		t.Errorf("streamed sms = %q", sms)
	}
	last := events[len(events)-1]
	if last.name != "result" || last.data["attempts"] != float64(1) {
		t.Fatalf("last event = %+v", last)
	}
	variant := last.data["variants"].(map[string]any)["en"].(map[string]any)
	lengths := last.data["lengths"].(map[string]any)["en"].(map[string]any)
	if variant["sms"] != sms || lengths["sms"] != float64(42) || last.data["limits"].(map[string]any)["sms"] != float64(160) {
		t.Errorf("variant = %v, lengths = %v", variant, lengths)
	}

	reqs := model.Requests()
	if len(reqs) != 1 || reqs[0].Task != content.TaskLocalize {
		t.Fatalf("%d model calls", len(reqs))
	}
	// The model adapts the text the marketer approved.
	if !strings.Contains(reqs[0].Messages[0].Text, `Merhaba {{first_name}}, %30 indirim seni bekliyor!`) {
		t.Error("the source text did not reach the model")
	}
}

// Nothing is sent to the model for a request that cannot succeed.
func TestContentLocalizeRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"no target language", `{"brief":"b","content":` + goodContent + `,"from":"tr","to":[]}`, 400, "invalid_request"},
		{"target is the source", `{"brief":"b","content":` + goodContent + `,"from":"tr","to":["tr"]}`, 400, "invalid_request"},
		{"target twice", `{"brief":"b","content":` + goodContent + `,"from":"tr","to":["en","en"]}`, 400, "invalid_request"},
		{"unknown target", `{"brief":"b","content":` + goodContent + `,"from":"tr","to":["de"]}`, 400, "invalid_request"},
		{"unknown source", `{"brief":"b","content":` + goodContent + `,"from":"xx","to":["en"]}`, 400, "invalid_request"},
		{"no content", `{"brief":"b","from":"tr","to":["en"]}`, 400, "invalid_request"},
		{"no brief", `{"brief":" ","content":` + goodContent + `,"from":"tr","to":["en"]}`, 400, "invalid_request"},
		{"invalid source text", `{"brief":"b","content":` + badContent + `,"from":"tr","to":["en"]}`, 422, "content_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &llm.Scripted{}
			rec := postLocalize(t, model, tt.body)
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %s", rec.Body)
			}
			if rec.Code != tt.status || body["error"].(map[string]any)["code"] != tt.code {
				t.Errorf("status %d, body %v", rec.Code, body)
			}
			if len(model.Requests()) != 0 {
				t.Error("the model was called")
			}
		})
	}
}

// A localisation the model never gets right ends with an error event naming
// the language that failed, and no result.
func TestContentLocalizeGivesUp(t *testing.T) {
	bad := `{"en":` + badContent + `}`
	rec := postLocalize(t, &llm.Scripted{Replies: []string{bad, bad, bad}}, `{"brief":"b","content":`+goodContent+`,"from":"tr","to":["en"]}`)
	events := parseSSE(t, rec.Body.String())
	last := events[len(events)-1]
	if last.name != "error" || last.data["code"] != "llm_output_invalid" {
		t.Fatalf("last event = %+v", last)
	}
	if path := last.data["details"].([]any)[0].(map[string]any)["path"]; path != "en.sms" {
		t.Errorf("error path = %v", path)
	}
}
