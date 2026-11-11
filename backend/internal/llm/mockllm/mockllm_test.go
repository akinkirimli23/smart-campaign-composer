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
		// "or" between different conditions must not quietly become "and".
		"İstanbul veya Ankara'da olup sepete ekleyen ya da son 30 günde satın alanlara gönder",
		"Kadınlara veya son 7 günde sepete ekleyenlere gönder",
	} {
		if out := generate(t, brief); out.Unsupported == nil || out.Rule != nil {
			t.Errorf("brief %q: want unsupported, got %+v", brief, out)
		}
	}
}

// "or" between two cities is a single condition and stays supported.
func TestOrBetweenCities(t *testing.T) {
	out := generate(t, "İstanbul veya Ankara'daki kadınlara, ayrıca İzmir ya da Bursa'dakilere gönder")
	want := []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul", "Ankara", "İzmir", "Bursa"}},
		{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"female"}},
	}
	if out.Rule == nil || !reflect.DeepEqual(out.Rule.Conditions, want) {
		t.Fatalf("got %+v\nwant %+v", out.Rule, want)
	}
}

// The three additions of DECISIONS 25, as the mock reads them.
func TestCountsMetricsAndSignup(t *testing.T) {
	tests := []struct {
		brief string
		want  []segment.Condition
	}{
		{"1000 TL üzeri harcayan İstanbulluları VIP kampanyasına davet et", []segment.Condition{
			{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
			{Type: segment.TypeMetric, Metric: segment.MetricTotalSpent, Op: segment.OpGTE, Value: 1000},
		}},
		{"500 TL altında harcama yapanlara indirim gönder", []segment.Condition{
			{Type: segment.TypeMetric, Metric: segment.MetricTotalSpent, Op: segment.OpLTE, Value: 500},
		}},
		{"Son 30 günde en az 3 kez sepete ürün ekleyenlere hatırlatma gönder", []segment.Condition{
			{Type: segment.TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true, MinCount: 3},
		}},
		{"Son 90 günde en az 2 kez alışveriş yapan kadınlara teşekkür et", []segment.Condition{
			{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"female"}},
			{Type: segment.TypeEvent, Event: "purchase", WithinDays: 90, Exists: true, MinCount: 2},
		}},
		{"Son 7 günde kayıt olanlara hoş geldin mesajı gönder", []segment.Condition{
			{Type: segment.TypeSignup, Op: segment.OpWithin, Days: 7},
		}},
		{"En az 5 sipariş vermiş Ankaralılara özel teklif", []segment.Condition{
			{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"Ankara"}},
			{Type: segment.TypeMetric, Metric: segment.MetricOrderCount, Op: segment.OpGTE, Value: 5},
		}},
	}
	for _, tt := range tests {
		out := generate(t, tt.brief)
		if out.Rule == nil || !reflect.DeepEqual(out.Rule.Conditions, tt.want) {
			t.Errorf("brief %q:\n got %+v\nwant %+v", tt.brief, out.Rule, tt.want)
		}
	}

	// The inclusive boundary of "üzeri" is an interpretation; it is surfaced.
	out := generate(t, "1000 TL üzeri harcayanlara gönder")
	if len(out.Assumptions) != 1 || out.Assumptions[0].ConditionIndex != 0 || out.Assumptions[0].Phrase != "1000 TL üzeri" {
		t.Errorf("assumptions = %+v", out.Assumptions)
	}
}

// Requests for data that does not exist are still refused.
func TestStillUnsupported(t *testing.T) {
	for _, brief := range []string{
		"Son 30 günde en az 3 kez ürün görüntüleyenlere indirim gönder",
		"Gold üyelere özel erken erişim duyurusu yap",
		"Doğum günü bu hafta olanlara hediye çeki gönder",
		"iOS kullanan İstanbullulara uygulama duyurusu yap",
	} {
		if out := generate(t, brief); out.Unsupported == nil || out.Rule != nil {
			t.Errorf("brief %q: want unsupported, got %+v", brief, out.Rule)
		}
	}
}
