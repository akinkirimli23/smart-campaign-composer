package anthropicllm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
)

// These tests talk to a local server that speaks the Messages API's SSE
// format. No API key and no network are needed.

type block struct {
	kind   string   // "text" or "thinking"
	deltas []string // text chunks, or thinking chunks
}

// sse renders one streamed assistant message.
func sse(stopReason string, blocks ...block) string {
	var b strings.Builder
	event := func(name string, payload any) {
		data, _ := json.Marshal(payload)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", name, data)
	}
	event("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 1},
	}})
	for i, blk := range blocks {
		start := map[string]any{"type": "text", "text": ""}
		if blk.kind == "thinking" {
			start = map[string]any{"type": "thinking", "thinking": "", "signature": ""}
		}
		event("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": start})
		for _, d := range blk.deltas {
			delta := map[string]any{"type": "text_delta", "text": d}
			if blk.kind == "thinking" {
				delta = map[string]any{"type": "thinking_delta", "thinking": d}
			}
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": delta})
		}
		if blk.kind == "thinking" {
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": i,
				"delta": map[string]any{"type": "signature_delta", "signature": "sig-abc"}})
		}
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	event("message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": 20}})
	event("message_stop", map[string]any{"type": "message_stop"})
	return b.String()
}

type fakeAPI struct {
	t       *testing.T
	replies []string // SSE bodies, one per request
	status  int      // non-zero: answer every request with this status
	bodies  []map[string]any
	headers []http.Header
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		f.t.Errorf("request body is not JSON: %v", err)
	}
	f.bodies = append(f.bodies, body)
	f.headers = append(f.headers, r.Header.Clone())

	if f.status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "req_test")
		w.WriteHeader(f.status)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad schema"}}`)
		return
	}
	n := len(f.bodies) - 1
	if n >= len(f.replies) {
		f.t.Fatalf("no reply scripted for request %d", n+1)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, f.replies[n])
}

func newClient(t *testing.T, api *fakeAPI) *Client {
	t.Helper()
	api.t = t
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return New("test-key", "", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
}

var testSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"sms": map[string]any{"type": "string"}},
	"required":             []any{"sms"},
	"additionalProperties": false,
}

func TestStreamSendsSchemaAndDeliversDeltas(t *testing.T) {
	api := &fakeAPI{replies: []string{sse("end_turn", block{"text", []string{`{"sms":"Mer`, `haba"}`}})}}
	c := newClient(t, api)

	var deltas []string
	resp, err := c.Stream(context.Background(), llm.Request{
		System:   "You write SMS.",
		Schema:   testSchema,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "brief"}},
	}, func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"sms":"Merhaba"}` {
		t.Errorf("text = %q", resp.Text)
	}
	if got := strings.Join(deltas, "|"); got != `{"sms":"Mer|haba"}` {
		t.Errorf("deltas = %q", got)
	}

	body := api.bodies[0]
	if body["model"] != DefaultModel || body["stream"] != true {
		t.Errorf("model = %v, stream = %v", body["model"], body["stream"])
	}
	// SDK v2 sends the key as a bearer token.
	if got := api.headers[0].Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("authorization header = %q", got)
	}
	system, _ := json.Marshal(body["system"])
	if !strings.Contains(string(system), "You write SMS.") {
		t.Errorf("system = %s", system)
	}
	format, _ := body["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("output_config.format = %v", format)
	}
	want, _ := json.Marshal(testSchema)
	got, _ := json.Marshal(format["schema"])
	if string(got) != string(want) {
		t.Errorf("schema sent = %s\nwant %s", got, want)
	}
	// Sampling and thinking knobs are rejected by current models; none is sent.
	for _, key := range []string{"temperature", "top_p", "top_k", "thinking", "tool_choice"} {
		if _, ok := body[key]; ok {
			t.Errorf("request must not set %q", key)
		}
	}
}

// A rejected reply goes back to the model exactly as it was produced, thinking
// block and signature included, followed by the validator's feedback.
func TestRetryReplaysAssistantTurnUnchanged(t *testing.T) {
	api := &fakeAPI{replies: []string{
		sse("end_turn", block{"thinking", []string{"let me ", "think"}}, block{"text", []string{`{"sms":"too long"}`}}),
		sse("end_turn", block{"text", []string{`{"sms":"ok"}`}}),
	}}
	c := newClient(t, api)
	ctx := context.Background()

	req := llm.Request{Schema: testSchema, Messages: []llm.Message{{Role: llm.RoleUser, Text: "brief"}}}
	first, err := c.Complete(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Text != `{"sms":"too long"}` {
		t.Fatalf("thinking must not leak into the text: %q", first.Text)
	}
	req.Messages = append(req.Messages,
		llm.Message{Role: llm.RoleAssistant, Text: first.Text, Raw: first.Raw},
		llm.Message{Role: llm.RoleUser, Text: "sms is 170 characters, limit is 160"},
	)
	second, err := c.Complete(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if second.Text != `{"sms":"ok"}` {
		t.Errorf("text = %q", second.Text)
	}

	msgs, _ := api.bodies[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	assistant, _ := msgs[1].(map[string]any)
	content, _ := assistant["content"].([]any)
	if assistant["role"] != "assistant" || len(content) != 2 {
		t.Fatalf("assistant turn = %v", assistant)
	}
	thinking, _ := content[0].(map[string]any)
	if thinking["type"] != "thinking" || thinking["thinking"] != "let me think" || thinking["signature"] != "sig-abc" {
		t.Errorf("thinking block not replayed unchanged: %v", thinking)
	}
	if text, _ := content[1].(map[string]any); text["text"] != `{"sms":"too long"}` {
		t.Errorf("text block = %v", text)
	}
	if last, _ := msgs[2].(map[string]any); last["role"] != "user" {
		t.Errorf("last message = %v", last)
	}
}

// Without Raw (e.g. a turn produced by the mock) the text is sent as-is.
func TestAssistantTurnWithoutRaw(t *testing.T) {
	api := &fakeAPI{replies: []string{sse("end_turn", block{"text", []string{`{"sms":"ok"}`}})}}
	c := newClient(t, api)
	_, err := c.Complete(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleUser, Text: "brief"},
		{Role: llm.RoleAssistant, Text: "previous"},
		{Role: llm.RoleUser, Text: "fix it"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(api.bodies[0]["messages"])
	if !strings.Contains(string(raw), `"previous"`) {
		t.Errorf("messages = %s", raw)
	}
	if _, ok := api.bodies[0]["output_config"]; ok {
		t.Error("no schema given, output_config must be omitted")
	}
}

func TestStopReasons(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  error
	}{
		{"refusal", sse("refusal"), ErrRefused},
		{"max tokens", sse("max_tokens", block{"text", []string{`{"sms":"cut o`}}), ErrTruncated},
		{"no text", sse("end_turn", block{"thinking", []string{"hmm"}}), ErrEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(t, &fakeAPI{replies: []string{tt.reply}})
			_, err := c.Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAPIErrorCarriesStatusAndNoKey(t *testing.T) {
	c := newClient(t, &fakeAPI{status: http.StatusBadRequest})
	_, err := c.Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Text: "x"}}})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "bad schema") {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Errorf("error leaks the API key: %v", err)
	}
}
