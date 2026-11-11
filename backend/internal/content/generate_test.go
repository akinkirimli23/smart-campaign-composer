package content

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
)

const brief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul bölgesindeki kullanıcılara %30 indirim hatırlatması gönder."

var good = string(mustJSON(valid()))

func withSMS(sms string) string {
	c := valid()
	c.SMS = sms
	return string(mustJSON(c))
}

// run generates with scripted replies and records every event.
func run(t *testing.T, replies ...string) (Result, error, *llm.Scripted, []Event) {
	t.Helper()
	model := &llm.Scripted{Replies: replies}
	var events []Event
	res, err := Generator{LLM: model}.Generate(context.Background(), brief, testMax, func(e Event) { events = append(events, e) })
	return res, err, model, events
}

// lastFeedback is the user message sent after the first rejected reply.
func lastFeedback(t *testing.T, model *llm.Scripted) (rejected, feedback string) {
	t.Helper()
	reqs := model.Requests()
	msgs := reqs[len(reqs)-1].Messages
	if len(msgs) < 3 {
		t.Fatalf("retry request has %d messages, want the rejected reply and the feedback", len(msgs))
	}
	return msgs[len(msgs)-2].Text, msgs[len(msgs)-1].Text
}

func TestGenerateFirstTry(t *testing.T) {
	res, err, model, _ := run(t, good)
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 1 || res.Content != valid() {
		t.Errorf("result = %+v", res)
	}
	req := model.Requests()[0]
	if req.Task != TaskGenerate || req.Schema == nil {
		t.Errorf("task = %q, schema set = %v", req.Task, req.Schema != nil)
	}
	// The brief is delimited as data and the model is told the real lengths.
	user := req.Messages[0].Text
	for _, want := range []string{"<brief>\n" + brief + "\n</brief>", "{{first_name}}", "counted as 10 characters"} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt does not contain %q:\n%s", want, user)
		}
	}
	if !strings.Contains(req.System, "untrusted") {
		t.Error("system prompt must mark the brief as untrusted")
	}
}

// The three failure modes of the stage's done criteria. Each is rejected,
// fed back with the validator's exact errors, and fixed on the second attempt.
func TestGenerateRecoversFromBadOutput(t *testing.T) {
	tests := []struct {
		name         string
		bad          string
		wantFeedback []string
	}{
		{"broken json", `{"sms": "Merhaba", "email": {"subject": "x"`, []string{"geçerli JSON değil"}},
		{"markdown fence", "```json\n" + good + "\n```", []string{"geçerli JSON değil"}},
		{"sms over the limit", withSMS("Merhaba {{first_name}}, " + strings.Repeat("çok ", 40)),
			[]string{"sms: ", "limit 160", "en az", "{{first_name}} 10 karakter sayılır"}},
		{"sms over the limit only after personalisation", withSMS(strings.Repeat("x", 155) + "{{first_name}}"),
			[]string{"sms: 165 karakter, limit 160; en az 5 karakter kısaltın"}},
		{"unknown variable", withSMS("Merhaba {{name}}, indirim seni bekliyor!"),
			[]string{"sms: {{name}} bilinmeyen bir değişken", "{{first_name}}, {{last_name}}, {{city}}"}},
		{"missing field", `{"sms":"Merhaba"}`, []string{"email: zorunlu alan eksik"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err, model, events := run(t, tt.bad, good)
			if err != nil {
				t.Fatal(err)
			}
			if res.Attempts != 2 || res.Content != valid() {
				t.Errorf("result = %+v", res)
			}
			rejected, feedback := lastFeedback(t, model)
			if rejected != tt.bad {
				t.Errorf("the rejected reply must go back unchanged, got %q", rejected)
			}
			for _, want := range tt.wantFeedback {
				if !strings.Contains(feedback, want) {
					t.Errorf("feedback does not contain %q:\n%s", want, feedback)
				}
			}
			var kinds []string
			for _, e := range events {
				if e.Kind != EventDelta {
					kinds = append(kinds, fmt.Sprintf("%s:%d", e.Kind, e.Attempt))
				}
			}
			if got := strings.Join(kinds, " "); got != "attempt:1 rejected:1 attempt:2" {
				t.Errorf("events = %s", got)
			}
		})
	}
}

// The loop is bounded: after MaxAttempts the last errors are returned and the
// model is not called again.
func TestGenerateGivesUp(t *testing.T) {
	bad := withSMS("{{nope}}")
	res, err, model, events := run(t, bad, bad, bad, good)
	var invalid *InvalidOutputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidOutputError", err)
	}
	if invalid.Attempts != DefaultMaxAttempts || len(invalid.Errors) != 1 || invalid.Errors[0].Path != "sms" {
		t.Errorf("invalid = %+v", invalid)
	}
	if n := len(model.Requests()); n != DefaultMaxAttempts {
		t.Errorf("model called %d times, want %d", n, DefaultMaxAttempts)
	}
	if res != (Result{}) {
		t.Errorf("no content may be returned on failure: %+v", res)
	}
	if last := events[len(events)-1]; last.Kind != EventRejected || last.Attempt != DefaultMaxAttempts {
		t.Errorf("last event = %+v", last)
	}
}

func TestGenerateMaxAttemptsIsConfigurable(t *testing.T) {
	model := &llm.Scripted{Replies: []string{"nope", good}}
	_, err := Generator{LLM: model, MaxAttempts: 1}.Generate(context.Background(), brief, testMax, nil)
	var invalid *InvalidOutputError
	if !errors.As(err, &invalid) || len(model.Requests()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(model.Requests()))
	}
}

// Deltas of an attempt add up to exactly the fields of that attempt's reply,
// and an attempt event precedes them so the UI knows when to clear.
func TestGenerateStreamsFieldDeltas(t *testing.T) {
	bad := withSMS("Merhaba {{name}}")
	_, err, _, events := run(t, bad, good)
	if err != nil {
		t.Fatal(err)
	}
	text := map[int]map[string]string{1: {}, 2: {}}
	deltas := 0
	current := 0
	for _, e := range events {
		switch e.Kind {
		case EventAttempt:
			current = e.Attempt
		case EventDelta:
			if e.Attempt != current {
				t.Fatalf("delta of attempt %d arrived during attempt %d", e.Attempt, current)
			}
			text[e.Attempt][e.Field] += e.Text
			deltas++
		}
	}
	if text[1][FieldSMS] != "Merhaba {{name}}" {
		t.Errorf("attempt 1 sms = %q", text[1][FieldSMS])
	}
	want := valid()
	got := text[2]
	if got[FieldSMS] != want.SMS || got[FieldEmailSubject] != want.Email.Subject || got[FieldEmailBody] != want.Email.Body {
		t.Errorf("attempt 2 streamed %q", got)
	}
	if deltas < 10 {
		t.Errorf("only %d deltas; the reply was not streamed in pieces", deltas)
	}
}

// completeOnly hides Scripted's Stream method.
type completeOnly struct{ inner *llm.Scripted }

func (c completeOnly) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	return c.inner.Complete(ctx, req)
}

// A client that cannot stream still fills the fields, in one piece each.
func TestGenerateWithoutStreaming(t *testing.T) {
	var events []Event
	res, err := Generator{LLM: completeOnly{&llm.Scripted{Replies: []string{good}}}}.
		Generate(context.Background(), brief, testMax, func(e Event) { events = append(events, e) })
	if err != nil || res.Content != valid() {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if len(events) != 4 || events[1].Field != FieldSMS || events[1].Text != valid().SMS {
		t.Errorf("events = %+v", events)
	}
}

func TestGenerateLLMError(t *testing.T) {
	_, err, _, _ := run(t) // no reply scripted: the client fails
	var invalid *InvalidOutputError
	if err == nil || errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a transport error, not InvalidOutputError", err)
	}
}
