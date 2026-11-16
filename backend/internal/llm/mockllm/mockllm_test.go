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
		"Kampanyadan haberdar et",
		// More than one "or" between different conditions is beyond the matcher.
		"Kadınlara veya son 7 günde sepete ekleyenlere ya da 1000 TL üzeri harcayanlara gönder",
		// An "or" with nothing recognisable on one side.
		"Kadınlara veya mutlu olanlara gönder",
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
		"Platin üyelere özel erken erişim duyurusu yap",
		"Doğum günü bu hafta olanlara hediye çeki gönder",
		"iOS kullanan İstanbullulara uygulama duyurusu yap",
	} {
		if out := generate(t, brief); out.Unsupported == nil || out.Rule != nil {
			t.Errorf("brief %q: want unsupported, got %+v", brief, out.Rule)
		}
	}
}

// Every province is in the data (DECISIONS 39), so a city outside the
// fifteen large ones is an ordinary condition.
func TestProvincesAreCities(t *testing.T) {
	for brief, want := range map[string][]string{
		"Elazığ'daki kadın kullanıcılara %10 indirim gönder":                               {"Elazığ"},
		"Son 30 günde sepete ekleyen Van ve İstanbul'daki kullanıcılara hatırlatma gönder": {"İstanbul", "Van"},
		"Şanlıurfa, Ankara ve İzmir için kampanya":                                         {"Ankara", "İzmir", "Şanlıurfa"},
	} {
		out := generate(t, brief)
		if out.Rule == nil {
			t.Errorf("brief %q: unsupported %+v", brief, out.Unsupported)
			continue
		}
		if got := out.Rule.Conditions[0]; got.Field != "city" || !reflect.DeepEqual(got.Values, want) {
			t.Errorf("brief %q: got %+v, want %v", brief, got, want)
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

// A requirement the matcher cannot express is refused, never dropped: every
// one of these used to produce a rule for a wider audience than was asked for.
func TestRefusesWhatItWouldDrop(t *testing.T) {
	for brief, phrase := range map[string]string{
		"İstanbul'daki kullanıcılara yalnızca SMS gönder":                                          "yalnızca SMS",
		"Ankara'daki kullanıcılara sadece email gönder":                                            "sadece email",
		"Ankara'daki kullanıcılara WhatsApp mesajı gönder":                                         "WhatsApp",
		"İstanbul'daki Almanca konuşan kullanıcılara gönder":                                       "Almanca",
		"-500 TL üzeri harcayanlara gönder":                                                        "-500",
		"Son 30 günde alışveriş yapan ve son 30 günde hiç alışveriş yapmayan kullanıcılara gönder": "alışveriş yapmayan",
		"Send a 20% discount to users in Istanbul who added to cart in the last 14 days":           "Send",
	} {
		out := generate(t, brief)
		if out.Unsupported == nil || out.Rule != nil {
			t.Errorf("brief %q: want unsupported, got %+v", brief, out.Rule)
			continue
		}
		if len(out.Unsupported.Phrases) != 1 || out.Unsupported.Phrases[0] != phrase {
			t.Errorf("brief %q: phrases = %q, want %q", brief, out.Unsupported.Phrases, phrase)
		}
	}
}

// "Outside İstanbul" excludes the city; it used to select it.
func TestCityExclusion(t *testing.T) {
	for _, brief := range []string{"İstanbul dışındaki kullanıcılara kampanya gönder", "Ankara ve İzmir hariç herkese kampanya gönder"} {
		out := generate(t, brief)
		if out.Rule == nil || len(out.Rule.Conditions) != 1 || out.Rule.Conditions[0].Op != segment.OpNotIn {
			t.Errorf("brief %q: got %+v", brief, out.Rule)
		}
	}
	if out := generate(t, "İstanbul'daki kullanıcılara kampanya gönder"); out.Rule.Conditions[0].Op != segment.OpIn {
		t.Errorf("plain city: got %+v", out.Rule.Conditions[0])
	}
}

// One-sided ages become a range instead of being ignored, and say how the
// boundary was read.
func TestOneSidedAge(t *testing.T) {
	out := generate(t, "25 yaşından büyük erkeklere kampanya gönder")
	want := []segment.Condition{
		{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"male"}},
		{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: 26, Max: segment.MaxAge},
	}
	if out.Rule == nil || !reflect.DeepEqual(out.Rule.Conditions, want) {
		t.Fatalf("got %+v", out.Rule)
	}
	if len(out.Assumptions) != 1 || out.Assumptions[0].ConditionIndex != 1 || !strings.Contains(out.Assumptions[0].Interpretation, "26 yaş ve üzeri") {
		t.Errorf("assumptions = %+v", out.Assumptions)
	}

	out = generate(t, "40 yaş altı kadınlara kampanya gönder")
	if c := out.Rule.Conditions[1]; c.Min != segment.MinAge || c.Max != 39 {
		t.Errorf("under 40: got %+v", c)
	}
	if out := generate(t, "Türkçe konuşan kullanıcılara kampanya gönder"); out.Rule == nil || out.Rule.Conditions[0].Values[0] != "tr" {
		t.Errorf("turkish speakers: got %+v", out.Rule)
	}
}

// "or" between different conditions is what it says: those two conditions
// become an "any" pair, and the rest of the brief still has to hold. It used
// to be refused, and before that it silently became "and".
func TestOrBetweenConditionsBecomesAGroup(t *testing.T) {
	out := generate(t, "İstanbul'da yaşayan ve son 30 günde sepete ekleyen veya 1000 TL üzeri harcayan kullanıcılara gönder")
	want := segment.Rule{Match: segment.MatchAll, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: []string{"İstanbul"}},
		{Type: segment.TypeGroup, Match: segment.MatchAny, Conditions: []segment.Condition{
			{Type: segment.TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true},
			{Type: segment.TypeMetric, Metric: segment.MetricTotalSpent, Op: segment.OpGTE, Value: 1000},
		}},
	}}
	if out.Rule == nil || !reflect.DeepEqual(*out.Rule, want) {
		t.Fatalf("got %+v\nwant %+v", out.Rule, want)
	}
	// The grouping is a reading of an ambiguous sentence, so it is said out
	// loud, on the group; the spend boundary keeps its own note there too.
	var grouping, boundary bool
	for _, a := range out.Assumptions {
		if a.ConditionIndex != 1 {
			t.Errorf("assumption on condition %d: %+v", a.ConditionIndex, a)
		}
		grouping = grouping || (a.Phrase == "veya" && strings.Contains(a.Interpretation, "en az biri"))
		boundary = boundary || strings.Contains(a.Interpretation, "1000 TL")
	}
	if !grouping || !boundary || len(out.Assumptions) != 2 {
		t.Errorf("assumptions = %+v", out.Assumptions)
	}

	// Conditions after the pair still apply to everyone.
	// (One window for the whole brief: two different ones are refused.)
	out = generate(t, "Kadınlara ya da son 7 günde üye olanlara, sepete ekleyenlere gönder")
	if out.Rule == nil || out.Rule.Match != segment.MatchAll || len(out.Rule.Conditions) != 2 ||
		out.Rule.Conditions[0].Type != segment.TypeGroup || out.Rule.Conditions[1].Event != "cart_add" {
		t.Errorf("got %+v", out.Rule)
	}
}

// With nothing else in the brief the pair is the whole rule: a plain "any".
func TestOrBetweenTwoConditionsIsAFlatRule(t *testing.T) {
	out := generate(t, "Kadınlara veya son 7 günde sepete ekleyenlere gönder")
	want := segment.Rule{Match: segment.MatchAny, Conditions: []segment.Condition{
		{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"female"}},
		{Type: segment.TypeEvent, Event: "cart_add", WithinDays: 7, Exists: true},
	}}
	if out.Rule == nil || !reflect.DeepEqual(*out.Rule, want) {
		t.Fatalf("got %+v\nwant %+v", out.Rule, want)
	}
}

// The pair is chosen by where the words are, not by the order the matcher
// recognises conditions in: "new members or frequent buyers, in İstanbul".
func TestOrJoinsTheConditionsNextToIt(t *testing.T) {
	out := generate(t, "Son 7 günde kayıt olan veya en az 5 sipariş vermiş, İstanbul'daki İngilizce konuşan kullanıcılara gönder")
	if out.Rule == nil || out.Rule.Match != segment.MatchAll || len(out.Rule.Conditions) != 3 {
		t.Fatalf("got %+v", out.Rule)
	}
	var group *segment.Condition
	for i, c := range out.Rule.Conditions {
		if c.Type == segment.TypeGroup {
			group = &out.Rule.Conditions[i]
		} else if c.Field != "city" && c.Field != "language" {
			t.Errorf("outside the group: %+v", c)
		}
	}
	if group == nil || len(group.Conditions) != 2 || group.Conditions[0].Type != segment.TypeSignup ||
		group.Conditions[1].Metric != segment.MetricOrderCount || group.Conditions[1].Value != 5 {
		t.Fatalf("group = %+v", group)
	}
}

// Things the matcher used to get wrong without saying so.
func TestRefusesWhatItCannotRead(t *testing.T) {
	for brief, reason := range map[string]string{
		// A purchase phrased in a way it does not know.
		"Ankara'daki kadınlara, elektronik alışverişi olanlara kampanya gönder": "alışverişten söz ediyor",
		// Two conditions with two different windows.
		"Son 90 günde sepete ekleyip son 30 günde hiç alışveriş yapmayan erkeklere gönder": "tek bir dönem",
	} {
		out := generate(t, brief)
		if out.Unsupported == nil || !strings.Contains(out.Unsupported.Reason, reason) {
			t.Errorf("brief %q: got rule %+v, unsupported %+v", brief, out.Rule, out.Unsupported)
		}
	}
	// "alışveriş yaptı" is now a purchase, with its category and count.
	out := generate(t, "Son 60 günde elektronik kategorisinden en az 2 kez alışveriş yaptılarsa kadınlara kampanya gönder")
	if out.Rule == nil || len(out.Rule.Conditions) != 2 {
		t.Fatalf("got %+v / %+v", out.Rule, out.Unsupported)
	}
	if c := out.Rule.Conditions[1]; c.Event != "purchase" || c.Category != "electronics" || c.MinCount != 2 || c.WithinDays != 60 {
		t.Errorf("purchase = %+v", c)
	}
}

// With all 81 provinces in the data, a city is a word, not a substring:
// ordinary words that contain a province's name must not select it.
func TestCitiesAreWords(t *testing.T) {
	for brief, want := range map[string][]string{
		"Muş'taki müşterilere karşı konulmaz bir fırsat gönder":             {"Muş"},
		"Ordulu kullanıcılara, bölümünde yeni ürünleri duyur":               {"Ordu"},
		"vandaki ve KARSTAKİ kullanıcılara kampanya gönder":                 {"Kars", "Van"},
		"Antep ve Urfa'daki kullanıcılara, İzmit'tekilerle birlikte gönder": {"Gaziantep", "Kocaeli", "Şanlıurfa"},
		"istanbuldaki kadın müşterilere indirim gönder":                     {"İstanbul"},
	} {
		out := generate(t, brief)
		if out.Rule == nil {
			t.Errorf("brief %q: unsupported %+v", brief, out.Unsupported)
			continue
		}
		var cities []string
		for _, c := range out.Rule.Conditions {
			if c.Field == "city" {
				cities = c.Values
			}
		}
		if !reflect.DeepEqual(cities, want) {
			t.Errorf("brief %q: cities = %v, want %v", brief, cities, want)
		}
	}
	// No city at all in a brief full of near misses.
	if out := generate(t, "Müşterilere karşı ordusu olan bölümün musluk gibi ağrısız avantajlarını kadınlara duyur"); out.Rule == nil || len(out.Rule.Conditions) != 1 || out.Rule.Conditions[0].Field != "gender" {
		t.Errorf("near misses: got %+v", out.Rule)
	}
}

// An "or" between two audiences that each name their own city is read as
// two clauses, not as one city list with an "or" of the nearest conditions.
func TestOrBetweenTwoAudiences(t *testing.T) {
	out := generate(t, "İstanbul'da yaşayan ve son 30 günde sepete ekleyen veya Ankara da yaşayan ve 1000 TL üzeri harcayan kullanıcılara gönder.")
	if out.Rule == nil || out.Rule.Match != segment.MatchAny || len(out.Rule.Conditions) != 2 {
		t.Fatalf("got %+v / %+v", out.Rule, out.Unsupported)
	}
	first, second := out.Rule.Conditions[0], out.Rule.Conditions[1]
	if first.Type != segment.TypeGroup || first.Match != segment.MatchAll || len(first.Conditions) != 2 ||
		!reflect.DeepEqual(first.Conditions[0].Values, []string{"İstanbul"}) || first.Conditions[1].Event != "cart_add" {
		t.Errorf("first side = %+v", first)
	}
	if second.Type != segment.TypeGroup || len(second.Conditions) != 2 ||
		!reflect.DeepEqual(second.Conditions[0].Values, []string{"Ankara"}) || second.Conditions[1].Metric != segment.MetricTotalSpent {
		t.Errorf("second side = %+v", second)
	}
	// The reading is shown to the marketer.
	said := false
	for _, a := range out.Assumptions {
		said = said || strings.Contains(a.Interpretation, "iki ayrı kitle")
	}
	if !said {
		t.Errorf("assumptions = %+v", out.Assumptions)
	}

	// One side with a single condition stays a plain condition.
	out = generate(t, "İzmir'deki kadınlara ya da Bursa'dakilere gönder")
	if out.Rule == nil || out.Rule.Match != segment.MatchAny || out.Rule.Conditions[0].Type != segment.TypeGroup ||
		!reflect.DeepEqual(out.Rule.Conditions[1].Values, []string{"Bursa"}) {
		t.Errorf("got %+v / %+v", out.Rule, out.Unsupported)
	}

	// A city on one side only keeps the earlier reading: city AND (a OR b).
	out = generate(t, "İstanbul'da yaşayan ve son 30 günde sepete ekleyen veya 1000 TL üzeri harcayan kullanıcılara gönder")
	if out.Rule == nil || out.Rule.Match != segment.MatchAll || len(out.Rule.Conditions) != 2 {
		t.Errorf("got %+v / %+v", out.Rule, out.Unsupported)
	}
}

// Counts with an upper bound, the average order value and spend in one
// category (DECISIONS 40).
func TestVariety(t *testing.T) {
	out := generate(t, "Son 90 günde yalnızca bir kez alışveriş yapan İzmirlilere ikinci sipariş indirimi gönder")
	if out.Rule == nil || len(out.Rule.Conditions) != 2 {
		t.Fatalf("got %+v / %+v", out.Rule, out.Unsupported)
	}
	if c := out.Rule.Conditions[1]; c.Event != "purchase" || !c.Exists || c.MaxCount != 1 || c.MinCount != 0 || c.WithinDays != 90 {
		t.Errorf("only once = %+v", c)
	}

	out = generate(t, "Son 60 günde en fazla 2 kez sepete ekleyen kadınlara gönder")
	if out.Rule == nil || out.Rule.Conditions[1].Event != "cart_add" || out.Rule.Conditions[1].MaxCount != 2 {
		t.Errorf("at most = %+v / %+v", out.Rule, out.Unsupported)
	}

	// "sepet" in "ortalama sepet tutarı" is not a cart event.
	out = generate(t, "Ortalama sepet tutarı 800 TL üzeri olan Ankaralılara premium koleksiyonu duyur")
	if out.Rule == nil || len(out.Rule.Conditions) != 2 {
		t.Fatalf("got %+v / %+v", out.Rule, out.Unsupported)
	}
	if c := out.Rule.Conditions[1]; c.Metric != segment.MetricAvgOrderValue || c.Op != segment.OpGTE || c.Value != 800 {
		t.Errorf("average = %+v", c)
	}

	// A category with no event to carry it limits the spend, and says so.
	out = generate(t, "Elektronikte 2000 TL üzeri harcayan erkeklere aksesuar kampanyası gönder")
	if out.Rule == nil || len(out.Rule.Conditions) != 2 {
		t.Fatalf("got %+v / %+v", out.Rule, out.Unsupported)
	}
	if c := out.Rule.Conditions[1]; c.Metric != segment.MetricTotalSpent || c.Category != "electronics" || c.Value != 2000 {
		t.Errorf("category spend = %+v", c)
	}
	said := false
	for _, a := range out.Assumptions {
		said = said || strings.Contains(a.Interpretation, "Yalnızca bu kategorideki")
	}
	if !said {
		t.Errorf("assumptions = %+v", out.Assumptions)
	}

	// With a purchase condition in the brief the category stays on it.
	out = generate(t, "Son 30 günde elektronik kategorisinden alışveriş yapan ve 1000 TL üzeri harcayan kullanıcılara gönder")
	if out.Rule == nil || len(out.Rule.Conditions) != 2 || out.Rule.Conditions[0].Category != "" || out.Rule.Conditions[1].Category != "electronics" {
		t.Errorf("got %+v / %+v", out.Rule, out.Unsupported)
	}
}

// Everyone only when the brief says so and says nothing else (DECISIONS 43).
func TestEveryone(t *testing.T) {
	for _, brief := range []string{
		"Tüm kullanıcılara kampanya gönder",
		"Herkese %50 indirim duyurusu yap",
		"Bütün müşterilerimize yeni yıl kampanyasını duyur",
	} {
		out := generate(t, brief)
		if out.Rule == nil || !out.Rule.Everyone || len(out.Rule.Conditions) != 0 {
			t.Errorf("brief %q: got rule %+v, unsupported %+v", brief, out.Rule, out.Unsupported)
		}
	}
	// With a condition, "herkese" only ends the sentence.
	out := generate(t, "İstanbul'da yaşayan herkese kampanya gönder")
	if out.Rule == nil || out.Rule.Everyone || len(out.Rule.Conditions) != 1 {
		t.Errorf("city brief: got %+v", out.Rule)
	}
	// No audience named is not a request for everyone.
	if out := generate(t, "İndirim var, duyur"); out.Rule != nil {
		t.Errorf("no audience: got rule %+v", out.Rule)
	}
	// What the matcher cannot read still refuses, even next to "herkese".
	if out := generate(t, "iOS kullananlar dahil herkese duyur"); out.Rule != nil {
		t.Errorf("unsupported next to everyone: got rule %+v", out.Rule)
	}
}

// Loyalty tiers are a closed-set attribute like any other (DECISIONS 45).
func TestLoyaltyTier(t *testing.T) {
	cases := map[string][]string{
		"Altın üyelere özel erken erişim duyurusu yap": {"gold"},
		"Gold üyelere kampanya gönder":                 {"gold"},
		"Gümüş ve altın üyelere kampanya gönder":       {"silver", "gold"},
		"Bronz seviyedeki kullanıcılara gönder":        {"bronze"},
	}
	for brief, want := range cases {
		out := generate(t, brief)
		if out.Rule == nil || len(out.Rule.Conditions) != 1 || out.Rule.Conditions[0].Field != "loyalty_tier" ||
			out.Rule.Conditions[0].Op != segment.OpIn || !reflect.DeepEqual(out.Rule.Conditions[0].Values, want) {
			t.Errorf("brief %q: rule %+v, unsupported %+v", brief, out.Rule, out.Unsupported)
		}
	}
	// Next to another condition it is one more condition.
	out := generate(t, "İstanbul'daki altın üyelere kampanya gönder")
	if out.Rule == nil || len(out.Rule.Conditions) != 2 {
		t.Errorf("city and tier: %+v", out.Rule)
	}
	// "Altın" on its own is not a tier ("altın günleri kampanyası").
	if out := generate(t, "İstanbul'daki kullanıcılara altın günleri kampanyasını duyur"); out.Rule == nil || len(out.Rule.Conditions) != 1 {
		t.Errorf("gold as a word: %+v", out.Rule)
	}
}
