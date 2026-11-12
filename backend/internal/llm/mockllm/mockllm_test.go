package mockllm_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

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

// A city without data is refused by name, with the list of valid ones; it is
// never dropped, which would send the campaign to everyone else in the brief.
func TestCityOutsideTheData(t *testing.T) {
	for brief, city := range map[string]string{
		"Elazığ'daki kadın kullanıcılara %10 indirim gönder":                               "Elazığ",
		"Son 30 günde sepete ekleyen Van ve İstanbul'daki kullanıcılara hatırlatma gönder": "Van",
		"Şanlıurfa, Ankara ve İzmir için kampanya":                                         "Şanlıurfa",
	} {
		out := generate(t, brief)
		if out.Unsupported == nil || out.Rule != nil {
			t.Errorf("brief %q: want unsupported, got %+v", brief, out.Rule)
			continue
		}
		if len(out.Unsupported.Phrases) != 1 || out.Unsupported.Phrases[0] != city {
			t.Errorf("brief %q: phrases = %v, want [%s]", brief, out.Unsupported.Phrases, city)
		}
		if !strings.Contains(out.Unsupported.Reason, city+" sistemdeki şehirler arasında yok") ||
			!strings.Contains(out.Unsupported.Reason, "İstanbul, Ankara") {
			t.Errorf("brief %q: reason = %q", brief, out.Unsupported.Reason)
		}
	}

	// Words that only resemble a province are not cities.
	out := generate(t, "Ordular gibi kalabalık İstanbul'daki vanilya sevenlere kampanya")
	if out.Rule == nil || out.Rule.Conditions[0].Values[0] != "İstanbul" {
		t.Errorf("got %+v", out)
	}
}

// A send time in the brief comes back as a suggestion, resolved against the
// marketer's clock. Thursday 12 November 2026, 16:20, Istanbul.
func TestScheduleFromBrief(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatal(err)
	}
	clock := segment.Clock{Now: time.Date(2026, 11, 12, 16, 20, 0, 0, loc)}
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, loc) }

	tests := []struct {
		brief  string
		want   time.Time
		phrase string
		note   string
	}{
		{"İstanbul'daki kadınlara yarın saat 10'da indirim duyurusu gönder", at(13, 10, 0), "yarın", ""},
		{"Ankara'daki kullanıcılara cuma sabahı kampanya gönder", at(13, 9, 0), "cuma", "09:00 varsayıldı"},
		{"İzmir'deki kullanıcılara pazartesi gönder", at(16, 10, 0), "pazartesi", "10:00 varsayıldı"},
		{"Bursa'daki erkeklere 15 Kasım 09:30 için hatırlatma kur", at(15, 9, 30), "15 Kasım", ""},
		{"Konya'daki kullanıcılara 3 gün sonra akşam gönder", at(15, 18, 0), "3 gün sonra", "18:00 varsayıldı"},
		// Thursday itself means next Thursday, not today.
		{"Adana'daki kullanıcılara perşembeye kampanya hazırla", at(19, 10, 0), "perşembe", ""},
	}
	for _, tt := range tests {
		out, err := segment.Generator{LLM: mockllm.Client{}, MaxAttempts: 1}.GenerateAt(context.Background(), tt.brief, clock)
		if err != nil {
			t.Errorf("brief %q: %v", tt.brief, err)
			continue
		}
		if out.Rule == nil || out.Schedule == nil {
			t.Errorf("brief %q: rule %v, schedule %v", tt.brief, out.Rule, out.Schedule)
			continue
		}
		if !out.Schedule.SendAt.Equal(tt.want) || out.Schedule.Phrase != tt.phrase || !strings.Contains(out.Schedule.Interpretation, tt.note) {
			t.Errorf("brief %q: got %v %q %q, want %v %q", tt.brief, out.Schedule.SendAt.In(loc), out.Schedule.Phrase,
				out.Schedule.Interpretation, tt.want, tt.phrase)
		}
	}

	// "3 gün sonra" is when to send, not a three-day look-back window.
	out, err := segment.Generator{LLM: mockllm.Client{}}.GenerateAt(context.Background(),
		"Sepetine ürün ekleyenlere 3 gün sonra hatırlatma gönder", clock)
	if err != nil || out.Rule == nil || out.Rule.Conditions[0].WithinDays != 30 || out.Schedule == nil {
		t.Errorf("look-back window: %+v, %v", out.Rule, err)
	}
}

// No send time in the brief, a sale named after a weekday, a date that is too
// far away, or no clock at all: no suggestion, and the rule is unaffected.
func TestNoScheduleSuggested(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Istanbul")
	clock := segment.Clock{Now: time.Date(2026, 11, 12, 16, 20, 0, 0, loc)}
	for _, brief := range []string{
		"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder",
		"İstanbul'daki kullanıcılara Kara Cuma indirimini duyur",
		"Ankara'daki kullanıcılara 20 Mart için kampanya hazırla",
		"İzmir'deki pazarlama meraklısı kadınlara kampanya gönder",
	} {
		out, err := segment.Generator{LLM: mockllm.Client{}, MaxAttempts: 1}.GenerateAt(context.Background(), brief, clock)
		if err != nil || out.Rule == nil || out.Schedule != nil {
			t.Errorf("brief %q: rule %v, schedule %+v, err %v", brief, out.Rule, out.Schedule, err)
		}
	}
	out := generate(t, "Ankara'daki kullanıcılara yarın saat 10'da gönder") // Generate: no clock
	if out.Schedule != nil {
		t.Errorf("without a clock no send time may be suggested: %+v", out.Schedule)
	}
}
