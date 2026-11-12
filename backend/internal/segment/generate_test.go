package segment

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
)

const brief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."

func ruleReply(rule, assumptions string) string {
	return `{"result":{"kind":"rule","rule":` + rule + `,"assumptions":` + assumptions + `}}`
}

func TestGenerateAcceptsValidReply(t *testing.T) {
	model := &llm.Scripted{Replies: []string{ruleReply(exampleJSON,
		`[{"condition_index":2,"phrase":"alışveriş yapmayan","interpretation":"Son 30 günde hiç satın alma yapmamış."}]`)}}
	out, err := Generator{LLM: model}.Generate(context.Background(), brief)
	if err != nil {
		t.Fatal(err)
	}
	if out.Rule == nil || len(out.Rule.Conditions) != 3 || out.Attempts != 1 || len(out.Assumptions) != 1 {
		t.Fatalf("unexpected outcome: %+v", out)
	}
	req := model.Requests()[0]
	if got := BriefFromRequest(req); got != brief {
		t.Errorf("brief in request = %q", got)
	}
	if req.Schema == nil || req.Task != TaskGenerate {
		t.Errorf("request must carry the schema and task: %+v", req)
	}
}

// A rejected reply goes back to the model with the validator's errors, and the
// corrected reply is accepted.
func TestGenerateFeedsErrorsBack(t *testing.T) {
	tests := []struct {
		name         string
		bad          string
		wantFeedback string
	}{
		{"broken json", `{"result": {"kind": "rule", "rule": {`, "geçerli JSON değil"},
		{"prose instead of json", "Tabii! İşte kural: ...", "geçerli JSON değil"},
		{"city outside the closed set",
			ruleReply(strings.Replace(exampleJSON, "İstanbul", "Istanbul", 1), `[]`),
			`result.rule.conditions[0].values[0]: "Istanbul" geçerli bir şehir değil ("İstanbul" mi demek istediniz?)`},
		{"unknown field", ruleReply(`{"match":"all","conditions":[{"type":"attribute","field":"segment","op":"in","values":["vip"]}]}`, `[]`),
			"result.rule.conditions[0].field"},
		{"assumption points past the rule", ruleReply(exampleJSON, `[{"condition_index":3,"phrase":"x","interpretation":"y"}]`),
			"result.assumptions[0].condition_index: 3 geçersiz"},
		{"extra key", `{"result":{"kind":"rule","rule":` + exampleJSON + `,"assumptions":[],"sql":"SELECT 1"}}`, "result.sql: bilinmeyen alan"},
		{"unknown kind", `{"result":{"kind":"sql","query":"SELECT 1"}}`, "result.kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &llm.Scripted{Replies: []string{tt.bad, ruleReply(exampleJSON, `[]`)}}
			out, err := Generator{LLM: model}.Generate(context.Background(), brief)
			if err != nil {
				t.Fatal(err)
			}
			if out.Attempts != 2 || out.Rule == nil {
				t.Fatalf("want a rule on attempt 2, got %+v", out)
			}
			msgs := model.Requests()[1].Messages
			if len(msgs) != 3 || msgs[1].Role != llm.RoleAssistant || msgs[1].Text != tt.bad || msgs[2].Role != llm.RoleUser {
				t.Fatalf("retry must replay the bad reply and add feedback; got %+v", msgs)
			}
			if !strings.Contains(msgs[2].Text, tt.wantFeedback) {
				t.Fatalf("feedback does not contain %q:\n%s", tt.wantFeedback, msgs[2].Text)
			}
		})
	}
}

func TestGenerateGivesUpAfterMaxAttempts(t *testing.T) {
	bad := ruleReply(strings.Replace(exampleJSON, "İstanbul", "Paris", 1), `[]`)
	model := &llm.Scripted{Replies: []string{bad, bad, bad, ruleReply(exampleJSON, `[]`)}}
	_, err := Generator{LLM: model}.Generate(context.Background(), brief)

	var invalid *InvalidOutputError
	if !errors.As(err, &invalid) {
		t.Fatalf("want InvalidOutputError, got %v", err)
	}
	if invalid.Attempts != DefaultMaxAttempts || len(model.Requests()) != DefaultMaxAttempts {
		t.Fatalf("attempts = %d, requests = %d, want %d", invalid.Attempts, len(model.Requests()), DefaultMaxAttempts)
	}
	if len(invalid.Errors) == 0 || invalid.Errors[0].Path != "result.rule.conditions[0].values[0]" {
		t.Fatalf("last errors not reported: %v", invalid.Errors)
	}
}

func TestGenerateUnsupported(t *testing.T) {
	model := &llm.Scripted{Replies: []string{
		`{"result":{"kind":"unsupported","reason":"Harcama tutarına göre filtre desteklenmiyor.","phrases":["500 TL üzeri harcayan"]}}`}}
	out, err := Generator{LLM: model}.Generate(context.Background(), "500 TL üzeri harcayan kullanıcılara yaz")
	if err != nil {
		t.Fatal(err)
	}
	if out.Rule != nil || out.Unsupported == nil || out.Unsupported.Phrases[0] != "500 TL üzeri harcayan" {
		t.Fatalf("unexpected outcome: %+v", out)
	}
}

// "unsupported" must not be a way to return a rule that skips validation.
func TestGenerateUnsupportedCannotCarryARule(t *testing.T) {
	model := &llm.Scripted{Replies: []string{
		`{"result":{"kind":"unsupported","reason":"x","phrases":[],"rule":` + exampleJSON + `}}`,
		`{"result":{"kind":"unsupported","reason":"x","phrases":[]}}`}}
	out, err := Generator{LLM: model}.Generate(context.Background(), brief)
	if err != nil || out.Attempts != 2 {
		t.Fatalf("want rejection then success, got %+v, %v", out, err)
	}
}

func TestGenerateLLMError(t *testing.T) {
	_, err := Generator{LLM: &llm.Scripted{}}.Generate(context.Background(), brief)
	var invalid *InvalidOutputError
	if err == nil || errors.As(err, &invalid) {
		t.Fatalf("a transport error must not look like invalid output: %v", err)
	}
}

// --- send time read from the brief (DECISIONS 28) ------------------------------

var scheduleClock = Clock{Now: time.Date(2026, 11, 12, 16, 20, 0, 0, time.FixedZone("TRT", 3*3600))}

func scheduleReply(schedule string) string {
	return `{"result":{"kind":"rule","rule":` + exampleJSON + `,"assumptions":[],"schedule":` + schedule + `}}`
}

func TestGenerateReadsASchedule(t *testing.T) {
	model := &llm.Scripted{Replies: []string{
		scheduleReply(`{"local_time":"2026-11-13T09:00","phrase":"cuma sabahı","interpretation":"13 Kasım Cuma 09:00"}`)}}
	out, err := Generator{LLM: model}.GenerateAt(context.Background(), "brief", scheduleClock)
	if err != nil {
		t.Fatal(err)
	}
	// 09:00 on the marketer's clock (UTC+3) is 06:00 UTC.
	want := time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC)
	if out.Schedule == nil || !out.Schedule.SendAt.Equal(want) || out.Schedule.Phrase != "cuma sabahı" {
		t.Fatalf("schedule = %+v", out.Schedule)
	}
	// The model was told what time it is for the marketer.
	if prompt := model.Requests()[0].Messages[0].Text; !strings.Contains(prompt, "<local_time>2026-11-12T16:20 Thursday TRT</local_time>") {
		t.Errorf("prompt lacks the clock:\n%s", prompt)
	}
}

func TestGenerateWithoutASchedule(t *testing.T) {
	for name, reply := range map[string]string{
		"null":    scheduleReply(`null`),
		"omitted": `{"result":{"kind":"rule","rule":` + exampleJSON + `,"assumptions":[]}}`,
	} {
		out, err := Generator{LLM: &llm.Scripted{Replies: []string{reply}}}.GenerateAt(context.Background(), "brief", scheduleClock)
		if err != nil || out.Rule == nil || out.Schedule != nil {
			t.Errorf("%s: rule %v, schedule %+v, err %v", name, out.Rule, out.Schedule, err)
		}
	}
	// Without a clock the brief is not searched for a time: the prompt carries
	// none, and whatever the model says about a schedule is ignored.
	model := &llm.Scripted{Replies: []string{scheduleReply(`{"local_time":"nonsense","phrase":"x","interpretation":"y"}`)}}
	out, err := Generator{LLM: model}.Generate(context.Background(), "brief")
	if err != nil || out.Schedule != nil || strings.Contains(model.Requests()[0].Messages[0].Text, "local_time") {
		t.Errorf("schedule %+v, err %v", out.Schedule, err)
	}
}

// A bad send time is fed back like any other validation error.
func TestGenerateRetriesABadSchedule(t *testing.T) {
	tests := map[string]struct{ bad, want string }{
		"wrong format": {`{"local_time":"13 Kasım 09:00","phrase":"cuma","interpretation":"x"}`, "biçim YYYY-MM-DDTHH:MM"},
		"with a zone":  {`{"local_time":"2026-11-13T09:00+03:00","phrase":"cuma","interpretation":"x"}`, "biçim YYYY-MM-DDTHH:MM"},
		"in the past":  {`{"local_time":"2026-11-12T09:00","phrase":"bu sabah","interpretation":"x"}`, "geçmişte kalıyor"},
		"too far":      {`{"local_time":"2027-03-01T09:00","phrase":"1 Mart","interpretation":"x"}`, "30 günden daha ileride"},
		"extra field":  {`{"local_time":"2026-11-13T09:00","phrase":"cuma","interpretation":"x","zone":"UTC"}`, "result.schedule.zone"},
		"no phrase":    {`{"local_time":"2026-11-13T09:00","phrase":"","interpretation":"x"}`, "result.schedule.phrase"},
	}
	good := scheduleReply(`{"local_time":"2026-11-13T09:00","phrase":"cuma","interpretation":"13 Kasım Cuma 09:00"}`)
	for name, tt := range tests {
		model := &llm.Scripted{Replies: []string{scheduleReply(tt.bad), good}}
		out, err := Generator{LLM: model}.GenerateAt(context.Background(), "brief", scheduleClock)
		if err != nil || out.Attempts != 2 || out.Schedule == nil {
			t.Errorf("%s: attempts %d, schedule %+v, err %v", name, out.Attempts, out.Schedule, err)
			continue
		}
		msgs := model.Requests()[1].Messages
		if feedback := msgs[len(msgs)-1].Text; !strings.Contains(feedback, tt.want) {
			t.Errorf("%s: feedback does not contain %q:\n%s", name, tt.want, feedback)
		}
	}
}

// The schedule is a convenience: when the model cannot get it right, the rule
// is still returned, without a suggestion. A bad rule still fails as before.
func TestBadScheduleNeverCostsTheRule(t *testing.T) {
	bad := scheduleReply(`{"local_time":"2020-01-01T09:00","phrase":"x","interpretation":"y"}`)
	model := &llm.Scripted{Replies: []string{bad, bad, bad}}
	out, err := Generator{LLM: model}.GenerateAt(context.Background(), "brief", scheduleClock)
	if err != nil {
		t.Fatalf("a valid rule was lost over its schedule: %v", err)
	}
	if out.Rule == nil || out.Schedule != nil || out.Attempts != 3 {
		t.Errorf("rule %v, schedule %+v, attempts %d", out.Rule, out.Schedule, out.Attempts)
	}

	badRule := `{"result":{"kind":"rule","rule":{"match":"all","conditions":[]},"assumptions":[],"schedule":null}}`
	_, err = Generator{LLM: &llm.Scripted{Replies: []string{badRule, badRule, badRule}}}.GenerateAt(context.Background(), "brief", scheduleClock)
	var invalid *InvalidOutputError
	if !errors.As(err, &invalid) {
		t.Errorf("err = %v, want InvalidOutputError", err)
	}
}

func TestClockRoundTripsThroughThePrompt(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatal(err)
	}
	clock := Clock{Now: time.Date(2026, 11, 12, 16, 20, 0, 0, loc)}
	got, ok := ClockFromRequest("The marketer's current local date and time: " + clock.promptLine() + "\n<brief>x</brief>")
	if !ok || !got.Now.Equal(clock.Now) || got.Now.Location().String() != "Europe/Istanbul" {
		t.Errorf("got %v, %v", got.Now, ok)
	}
	if _, ok := ClockFromRequest("<brief>no clock here</brief>"); ok {
		t.Error("a request without a clock must report none")
	}
}
