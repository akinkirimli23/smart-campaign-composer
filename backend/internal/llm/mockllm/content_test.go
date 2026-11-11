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
