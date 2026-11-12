package mockllm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/mockllm"
)

// Generous lengths: the mock's copy must fit even with long names.
var maxLengths = content.VarLengths{"first_name": 15, "last_name": 15, "city": 15}

const exampleBrief = "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul bölgesindeki kullanıcılara %30 indirim hatırlatması gönder."

// The mock's content is valid on the first attempt and reflects the brief.
func TestContentExampleBrief(t *testing.T) {
	var streamed strings.Builder
	res, err := content.Generator{LLM: mockllm.Client{}, MaxAttempts: 1}.Generate(context.Background(), exampleBrief, maxLengths,
		func(e content.Event) {
			if e.Kind == content.EventDelta && e.Field == content.FieldSMS {
				streamed.WriteString(e.Text)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content.SMS, "%30 indirim") || !strings.Contains(res.Content.SMS, "{{first_name}}") {
		t.Errorf("sms = %q", res.Content.SMS)
	}
	if res.Lengths.SMS > content.MaxSMS || res.Lengths.EmailSubject > content.MaxEmailSubject {
		t.Errorf("lengths = %+v", res.Lengths)
	}
	if streamed.String() != res.Content.SMS {
		t.Errorf("streamed sms %q != final sms %q", streamed.String(), res.Content.SMS)
	}
}

// With the demo marker the first reply is rejected for the two reasons the
// UI needs to show, and the retry succeeds.
func TestContentDemoRetry(t *testing.T) {
	var rejected []string
	res, err := content.Generator{LLM: mockllm.Client{}}.Generate(context.Background(), exampleBrief+" "+mockllm.DemoRetryMarker, maxLengths,
		func(e content.Event) {
			if e.Kind == content.EventRejected {
				rejected = append(rejected, e.Errors.Error())
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 2 || len(rejected) != 1 {
		t.Fatalf("attempts = %d, rejected = %v", res.Attempts, rejected)
	}
	if !strings.Contains(rejected[0], "{{surname}} bilinmeyen bir değişken") {
		t.Errorf("rejection = %s", rejected[0])
	}
}

// A streaming mock stops when the request is cancelled.
func TestStreamHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mockllm.Client{ChunkDelay: time.Hour}.Stream(ctx, llm.Request{
		Task:     content.TaskGenerate,
		Messages: []llm.Message{{Role: llm.RoleUser, Text: "<brief>\nx\n</brief>"}},
	}, func(string) {})
	if err == nil {
		t.Fatal("want a context error")
	}
}

// Asked for English, the mock writes English copy that passes validation.
func TestContentInEnglish(t *testing.T) {
	res, err := content.Generator{LLM: mockllm.Client{}, MaxAttempts: 1}.GenerateIn(context.Background(), exampleBrief, "en", maxLengths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Content.SMS, "Hi {{first_name}}, the items in your cart") || !strings.Contains(res.Content.SMS, "30% off") {
		t.Errorf("sms = %q", res.Content.SMS)
	}
	if strings.Contains(res.Content.Email.Body, "Merhaba") {
		t.Errorf("body = %q", res.Content.Email.Body)
	}
}

// The mock localises in one reply, valid on the first attempt, and writes
// only the languages asked for.
func TestLocalize(t *testing.T) {
	g := content.Generator{LLM: mockllm.Client{}, MaxAttempts: 1}
	turkish, err := g.GenerateIn(context.Background(), exampleBrief, "tr", maxLengths, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := g.Localize(context.Background(), exampleBrief, turkish.Content, "tr", []string{"en"}, maxLengths, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Variants) != 1 || !strings.Contains(out.Variants["en"].SMS, "30% off") || out.Lengths["en"].SMS > content.MaxSMS {
		t.Errorf("variants = %+v, lengths = %+v", out.Variants, out.Lengths)
	}

	// The other direction: an English campaign localised into Turkish.
	back, err := g.Localize(context.Background(), exampleBrief, out.Variants["en"], "en", []string{"tr"}, maxLengths, nil)
	if err != nil || back.Variants["tr"] != turkish.Content {
		t.Errorf("turkish = %+v, err = %v", back.Variants["tr"], err)
	}
}

// The demo marker shows the retry loop in localisation too.
func TestLocalizeDemoRetry(t *testing.T) {
	var rejected []string
	out, err := content.Generator{LLM: mockllm.Client{}}.Localize(context.Background(), exampleBrief+" "+mockllm.DemoRetryMarker,
		content.Content{SMS: "a", Email: content.Email{Subject: "s", Body: "b"}}, "tr", []string{"en"}, maxLengths,
		func(e content.Event) {
			if e.Kind == content.EventRejected {
				rejected = append(rejected, e.Errors.Error())
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if out.Attempts != 2 || len(rejected) != 1 || !strings.Contains(rejected[0], "en.sms: {{surname}} bilinmeyen bir değişken") {
		t.Errorf("attempts = %d, rejected = %v", out.Attempts, rejected)
	}
}
