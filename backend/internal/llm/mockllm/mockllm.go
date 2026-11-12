// Package mockllm is the LLM used when no API key is configured.
//
// It is a small keyword matcher, not a language model: it recognises the
// phrases of typical briefs (cities, "sepet", "satın almayan", "son 30 gün" ...)
// well enough to demo the whole flow offline. Its output goes through exactly
// the same validation as a real model's.
package mockllm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/content"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/llm"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/segment"
)

// Client is the offline LLM. ChunkDelay slows streamed replies down so the
// UI visibly streams; zero (the default) means no delay.
type Client struct {
	ChunkDelay time.Duration
}

// DemoRetryMarker in a brief makes the first content reply invalid (an
// unknown variable in an over-long SMS), so the validate-and-retry loop can be
// shown without a real model.
const DemoRetryMarker = "#demo-retry"

func (Client) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	switch req.Task {
	case content.TaskGenerate:
		brief := content.BriefFromRequest(req)
		firstAttempt := len(req.Messages) == 1
		out, err := json.Marshal(contentResult(brief, content.LanguageFromRequest(req),
			firstAttempt && strings.Contains(brief, DemoRetryMarker)))
		return llm.Response{Text: string(out)}, err
	case content.TaskLocalize:
		// The mock does not translate: it writes its fixed copy for the brief
		// again, in each language asked for. Edits the marketer made to the
		// source text are therefore not carried over, as a real model would.
		brief := content.BriefFromRequest(req)
		to, _ := content.LocalizeFromRequest(req)
		broken := len(req.Messages) == 1 && strings.Contains(brief, DemoRetryMarker)
		variants := map[string]content.Content{}
		for i, language := range to {
			variants[language] = contentResult(brief, language, broken && i == 0)
		}
		out, err := json.Marshal(variants)
		return llm.Response{Text: string(out)}, err
	case segment.TaskGenerate:
		clock, _ := segment.ClockFromRequest(firstMessage(req))
		out, err := json.Marshal(map[string]any{"result": segmentResult(segment.BriefFromRequest(req), clock)})
		return llm.Response{Text: string(out)}, err
	default:
		return llm.Response{}, fmt.Errorf("mockllm: unknown task %q", req.Task)
	}
}

// Stream implements llm.Streamer by delivering the reply a few runes at a time.
func (c Client) Stream(ctx context.Context, req llm.Request, onText func(delta string)) (llm.Response, error) {
	resp, err := c.Complete(ctx, req)
	if err != nil || onText == nil {
		return resp, err
	}
	const chunk = 6
	runes := []rune(resp.Text)
	for len(runes) > 0 {
		if c.ChunkDelay > 0 {
			select {
			case <-ctx.Done():
				return llm.Response{}, ctx.Err()
			case <-time.After(c.ChunkDelay):
			}
		}
		n := min(chunk, len(runes))
		onText(string(runes[:n]))
		runes = runes[n:]
	}
	return resp, nil
}

var discountRe = regexp.MustCompile(`%\s?(\d{1,2})|(\d{1,2})\s?%`)

// contentResult writes fixed campaign copy around the discount found in the
// brief, in Turkish unless language is "en". broken returns copy the
// validator must reject.
func contentResult(brief, language string, broken bool) content.Content {
	if language == "en" {
		return englishContent(brief, broken)
	}
	offer := "özel fırsatlar"
	if m := discountRe.FindStringSubmatch(brief); m != nil {
		offer = "%" + m[1] + m[2] + " indirim"
	}
	about := "seçtiğin ürünler"
	if strings.Contains(segment.Fold(brief), "sepet") {
		about = "sepetindeki ürünler"
	}
	c := content.Content{
		SMS: fmt.Sprintf("Merhaba {{first_name}}, %s seni bekliyor! Sana özel %s için hemen uygulamaya göz at.", about, offer),
		Email: content.Email{
			Subject: fmt.Sprintf("{{first_name}}, %s seni bekliyor: %s", about, offer),
			Body: fmt.Sprintf("Merhaba {{first_name}},\n\n%s hâlâ seni bekliyor. Sana özel %s ile alışverişini tamamlayabilirsin.\n\nFırsatı kaçırmamak için uygulamaya göz at.\n\nİyi alışverişler!",
				strings.ToUpper(about[:1])+about[1:], offer),
		},
	}
	if broken {
		c.SMS = fmt.Sprintf("Merhaba {{first_name}} {{surname}}, %s seni bekliyor! Sana özel %s fırsatını kaçırmamak için hemen uygulamamızı aç, sepetine git ve alışverişini bugün tamamla. Stoklar tükenmeden acele et!", about, offer)
	}
	return c
}

func englishContent(brief string, broken bool) content.Content {
	offer := "special offers"
	if m := discountRe.FindStringSubmatch(brief); m != nil {
		offer = m[1] + m[2] + "% off"
	}
	about := "The products you picked"
	if strings.Contains(segment.Fold(brief), "sepet") {
		about = "The items in your cart"
	}
	lower := strings.ToLower(about[:1]) + about[1:]
	c := content.Content{
		SMS: fmt.Sprintf("Hi {{first_name}}, %s are waiting for you! Open the app now to get %s.", lower, offer),
		Email: content.Email{
			Subject: fmt.Sprintf("{{first_name}}, %s are waiting: %s", lower, offer),
			Body: fmt.Sprintf("Hi {{first_name}},\n\n%s are still waiting for you. Complete your order with %s, just for you.\n\nOpen the app so you don't miss out.\n\nHappy shopping!",
				about, offer),
		},
	}
	if broken {
		c.SMS = fmt.Sprintf("Hi {{first_name}} {{surname}}, %s are waiting for you! Do not miss your %s: open our app right now, go to your cart and complete your order today. Hurry up before the stock runs out!", lower, offer)
	}
	return c
}

var (
	windowRe = regexp.MustCompile(`(\d+)\s*(gun|hafta|ay)`)
	ageRe    = regexp.MustCompile(`(\d+)\s*-\s*(\d+)\s*yas`)
	wordRe   = regexp.MustCompile(`[a-z0-9]+`)

	// Requirements the rule format cannot express; the mock answers "unsupported"
	// for these so that path can be demoed without a real model.
	unsupportedHints = []string{"dogum gun", "en cok", "sadik", "goruntule", "gold", "silver", "bronze", "ios", "android"}

	timesRe  = regexp.MustCompile(`en az (\d+) (kez|kere|defa)`)
	spentRe  = regexp.MustCompile(`(\d+)\s*(tl|lira)\S*\s+(uzeri|ustu|fazla|ve uzeri|alti|altinda|az)`)
	ordersRe = regexp.MustCompile(`en az (\d+) siparis|(\d+)\S*\s+fazla siparis`)
	signupRe = regexp.MustCompile(`kayit ol|uye ol|yeni uye`)

	categoryHints = []struct{ hint, category string }{
		{"elektronik", "electronics"}, {"moda", "fashion"}, {"giyim", "fashion"},
		{"kozmetik", "cosmetics"}, {"mobilya", "home"}, {"spor", "sports"}, {"kitap", "books"},
	}
	notPurchased = []string{"alisveris yapmayan", "satin almayan", "alisveris yapmamis", "satin almamis", "almayan"}
	purchased    = []string{"alisveris yapan", "satin alan", "alisveris yapmis", "satin almis"}
)

func firstMessage(req llm.Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	return req.Messages[0].Text
}

func segmentResult(brief string, clock segment.Clock) map[string]any {
	text := segment.Fold(brief)

	// A send time is read first and its words are blanked out of text, so
	// "3 gün sonra gönder" is not also taken for a look-back window.
	var schedule any
	if !clock.Now.IsZero() {
		schedule, text = scheduleResult(brief, text, clock)
	}

	for _, hint := range unsupportedHints {
		if strings.Contains(text, hint) {
			return map[string]any{
				"kind":    "unsupported",
				"reason":  "Kural formatı sıralama, doğum günü, sadakat seviyesi, cihaz ya da ürün görüntüleme koşullarını ifade edemiyor.",
				"phrases": []string{original(brief, text, hint)},
			}
		}
	}

	// A city the data does not cover must not simply be ignored: the rest of
	// the brief would then select a much wider audience than was asked for.
	if city, ok := unknownCity(brief, text); ok {
		return map[string]any{
			"kind": "unsupported",
			"reason": fmt.Sprintf("%s sistemdeki şehirler arasında yok. Geçerli şehirler: %s.",
				city, strings.Join(seed.Cities(), ", ")),
			"phrases": []string{city},
		}
	}

	// "A veya B" between two cities is one condition (city in A, B). Any other
	// "or" joins different conditions, which this matcher would silently turn
	// into "and", i.e. a different audience. Refuse instead.
	if phrase, ok := orBetweenConditions(text); ok {
		return map[string]any{
			"kind":    "unsupported",
			"reason":  "Mock LLM \"veya\" ile bağlanan farklı koşulları ayırt edemiyor; koşulları \"ve\" ile bağlayın ya da gerçek modeli kullanın.",
			"phrases": []string{original(brief, text, phrase)},
		}
	}

	days := 30
	if m := windowRe.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		days = n * map[string]int{"gun": 1, "hafta": 7, "ay": 30}[m[2]]
	}

	category := ""
	for _, h := range categoryHints {
		if strings.Contains(text, h.hint) {
			category = h.category
			break
		}
	}

	var conditions []segment.Condition
	var assumptions []segment.Assumption

	var cities []string
	for _, city := range seed.Cities() {
		if strings.Contains(text, segment.Fold(city)) {
			cities = append(cities, city)
		}
	}
	if len(cities) > 0 {
		conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "city", Op: segment.OpIn, Values: cities})
	}
	if strings.Contains(text, "ingilizce") {
		conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "language", Op: segment.OpIn, Values: []string{"en"}})
	}
	words := wordRe.FindAllString(text, -1)
	hasWordPrefix := func(prefix string) bool {
		for _, w := range words {
			if strings.HasPrefix(w, prefix) {
				return true
			}
		}
		return false
	}
	switch female, male := hasWordPrefix("kadin"), hasWordPrefix("erkek"); {
	case female && !male:
		conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"female"}})
	case male && !female:
		conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "gender", Op: segment.OpIn, Values: []string{"male"}})
	}
	if m := ageRe.FindStringSubmatch(text); m != nil {
		lo, _ := strconv.Atoi(m[1])
		hi, _ := strconv.Atoi(m[2])
		conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: lo, Max: hi})
	}

	if signupRe.MatchString(text) {
		conditions = append(conditions, segment.Condition{Type: segment.TypeSignup, Op: segment.OpWithin, Days: days})
	}
	if m := spentRe.FindStringSubmatch(text); m != nil {
		value, _ := strconv.Atoi(m[1])
		op, bound := segment.OpGTE, "en az"
		if strings.HasPrefix(m[3], "alt") || m[3] == "az" {
			op, bound = segment.OpLTE, "en fazla"
		}
		conditions = append(conditions, segment.Condition{Type: segment.TypeMetric, Metric: segment.MetricTotalSpent, Op: op, Value: value})
		assumptions = append(assumptions, segment.Assumption{
			ConditionIndex: len(conditions) - 1, Phrase: original(brief, text, m[0]),
			Interpretation: fmt.Sprintf("Tüm zamanlardaki toplam harcama %s %d TL olarak yorumlandı; %d TL'nin kendisi dahil.", bound, value, value),
		})
	}
	if m := ordersRe.FindStringSubmatch(text); m != nil {
		value, _ := strconv.Atoi(m[1] + m[2])
		if m[2] != "" {
			value++ // "3'ten fazla" means at least 4
		}
		conditions = append(conditions, segment.Condition{Type: segment.TypeMetric, Metric: segment.MetricOrderCount, Op: segment.OpGTE, Value: value})
	}
	minCount := 0
	if m := timesRe.FindStringSubmatch(text); m != nil {
		minCount, _ = strconv.Atoi(m[1])
	}

	hasCart := strings.Contains(text, "sepet")
	if hasCart {
		conditions = append(conditions, segment.Condition{Type: segment.TypeEvent, Event: "cart_add", Category: category, WithinDays: days, Exists: true, MinCount: minCount})
	}
	if phrase, ok := firstMatch(text, notPurchased); ok {
		phrase = original(brief, text, phrase)
		conditions = append(conditions, segment.Condition{Type: segment.TypeEvent, Event: "purchase", WithinDays: days, Exists: false})
		interpretation := fmt.Sprintf("Son %d günde hiç satın alma yapmamış olarak yorumlandı.", days)
		if hasCart {
			interpretation = fmt.Sprintf("Son %d günde hiç satın alma yapmamış olarak yorumlandı; sepete eklenen ürünün sonradan alınıp alınmadığına bakılmıyor.", days)
		}
		assumptions = append(assumptions, segment.Assumption{ConditionIndex: len(conditions) - 1, Phrase: phrase, Interpretation: interpretation})
	} else if _, ok := firstMatch(text, purchased); ok {
		cat := category
		if hasCart {
			cat = "" // the category hint already went to the cart condition
		}
		count := minCount
		if hasCart {
			count = 0 // "en az N kez" already went to the cart condition
		}
		conditions = append(conditions, segment.Condition{Type: segment.TypeEvent, Event: "purchase", Category: cat, WithinDays: days, Exists: true, MinCount: count})
	}

	if len(conditions) == 0 {
		return map[string]any{
			"kind":    "unsupported",
			"reason":  "Brief'te tanınan bir kitle koşulu bulunamadı (mock LLM yalnızca şehir, dil, cinsiyet, yaş, üyelik tarihi, sepet, satın alma, harcama ve sipariş sayısı ifadelerini tanır).",
			"phrases": []string{},
		}
	}
	if assumptions == nil {
		assumptions = []segment.Assumption{}
	}
	return map[string]any{
		"kind":        "rule",
		"rule":        segment.Rule{Match: segment.MatchAll, Conditions: conditions},
		"assumptions": assumptions,
		"schedule":    schedule,
	}
}

// provinces are Turkey's provinces (and two common short names), folded. The
// mock uses them only to notice a city that is not in the seed data.
var provinces = strings.Fields(`adana adiyaman afyonkarahisar afyon agri aksaray amasya ankara antalya ardahan
artvin aydin balikesir bartin batman bayburt bilecik bingol bitlis bolu burdur bursa canakkale cankiri corum
denizli diyarbakir duzce edirne elazig erzincan erzurum eskisehir gaziantep giresun gumushane hakkari hatay
igdir isparta istanbul izmir kahramanmaras karabuk karaman kars kastamonu kayseri kilis kirikkale kirklareli
kirsehir kocaeli konya kutahya malatya manisa mardin mersin mugla mus nevsehir nigde ordu osmaniye rize
sakarya samsun sanliurfa urfa siirt sinop sirnak sivas tekirdag tokat trabzon tunceli usak van yalova yozgat
zonguldak`)

// unknownCity returns the first word of the brief that names a province with
// no users in the data, in the marketer's own spelling. A word counts only
// when it is the name itself or the name followed by an apostrophe suffix
// ("Elazığ'daki"), so ordinary words that merely start like a province do not.
func unknownCity(brief, folded string) (string, bool) {
	supported := map[string]bool{}
	for _, c := range seed.Cities() {
		supported[segment.Fold(c)] = true
	}
	original := strings.Fields(strings.TrimSpace(brief))
	words := strings.Fields(folded)
	for i, w := range words {
		name := strings.TrimRight(w, ".,;:!?)")
		for _, sep := range []string{"'", "’"} {
			if at := strings.Index(name, sep); at >= 0 {
				name = name[:at]
			}
		}
		if supported[name] || !slices.Contains(provinces, name) {
			continue
		}
		if i < len(original) {
			shown := strings.TrimRight(original[i], ".,;:!?)")
			for _, sep := range []string{"'", "’"} {
				if at := strings.Index(shown, sep); at >= 0 {
					shown = shown[:at]
				}
			}
			return shown, true
		}
		return name, true
	}
	return "", false
}

// orBetweenConditions finds an "or" that does not simply join two city names.
func orBetweenConditions(text string) (string, bool) {
	isCity := func(word string) bool {
		for _, city := range seed.Cities() {
			if strings.HasPrefix(word, segment.Fold(city)) {
				return true
			}
		}
		return false
	}
	words := strings.Fields(text)
	for i, w := range words {
		phrase, next := "", i+1
		switch {
		case w == "veya":
			phrase = "veya"
		case w == "ya" && i+1 < len(words) && words[i+1] == "da":
			phrase, next = "ya da", i+2
		default:
			continue
		}
		if i == 0 || next >= len(words) || !isCity(words[i-1]) || !isCity(words[next]) {
			return phrase, true
		}
	}
	return "", false
}

func firstMatch(text string, phrases []string) (string, bool) {
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return p, true
		}
	}
	return "", false
}

// original returns the part of brief that produced the folded match, so the
// marketer sees their own words. Fold maps rune to rune, so indexes line up.
func original(brief, folded, match string) string {
	src := []rune(strings.TrimSpace(brief))
	i := strings.Index(folded, match)
	if i < 0 {
		return match
	}
	start := len([]rune(folded[:i]))
	end := start + len([]rune(match))
	if end > len(src) {
		return match
	}
	return string(src[start:end])
}
