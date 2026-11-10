package mockllm_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm/mockllm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

func generate(t *testing.T, brief string) segment.Outcome {
	t.Helper()
	// MaxAttempts 1: the mock must produce valid output on its own.
	out, err := segment.Generator{LLM: mockllm.Client{}, MaxAttempts: 1}.Generate(context.Background(), brief)
	if err != nil {
		t.Fatalf("brief %q: %v", brief, err)
	}
	return out
}

func TestExampleBrief(t *testing.T) {
	out := generate(t, "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.")
	want := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
		{Type: segment.TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true},
		{Type: segment.TypeEvent, Event: "purchase", WithinDays: 30, Exists: false},
	}}
	if out.Rule == nil || !reflect.DeepEqual(*out.Rule, want) {
		t.Fatalf("got %+v\nwant %+v", out.Rule, want)
	}
	if len(out.Assumptions) != 1 || out.Assumptions[0].ConditionIndex != 2 || out.Assumptions[0].Phrase != "alışveriş yapmayan" {
		t.Fatalf("the ambiguous phrase must be surfaced in the marketer's own words: %+v", out.Assumptions)
	}
}

func TestOtherBriefs(t *testing.T) {
	out := generate(t, "Ankara ve İzmir'de son 2 haftada kozmetik satın alan 25-34 yaş kadınlara yeni seri duyurusu")
	want := []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"Ankara", "İzmir"}},
		{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"female"}},
		{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: 25, Max: 34},
		{Type: segment.TypeEvent, Event: "purchase", Category: "cosmetics", WithinDays: 14, Exists: true},
	}
	if out.Rule == nil || !reflect.DeepEqual(out.Rule.Conditions, want) {
		t.Fatalf("got %+v\nwant %+v", out.Rule, want)
	}
}

func TestUnsupportedBriefs(t *testing.T) {
	for _, brief := range []string{
		"En çok harcayan 100 müşteriye teşekkür mesajı gönder",
		"Herkese merhaba de",
	} {
		if out := generate(t, brief); out.Unsupported == nil || out.Rule != nil {
			t.Errorf("brief %q: want unsupported, got %+v", brief, out)
		}
	}
}
