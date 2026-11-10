package segment

import (
	"context"
	"errors"
	"strings"
	"testing"

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
