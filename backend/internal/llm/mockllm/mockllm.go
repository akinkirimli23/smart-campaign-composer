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
	unsupportedHints = []string{"dogum gun", "en cok", "sadik", "goruntule", "platin", "ios", "android"}

	// Loyalty tiers (DECISIONS 45): "altın üyelere", "gold üye", "gümüş ve
	// altın üyelere", "bronz seviyedeki". The tier word has to lead up to a
	// word for a member, so "altın günleri kampanyası" is not a tier; every
	// tier named on the way there counts, so none is dropped.
	tierPhraseRe = regexp.MustCompile(`((bronze|bronz|gumus|silver|altin|gold)(,| ve| veya| ya da)? )+(uye|seviye|musteri)`)
	tierWords    = []struct{ tier, words string }{
		{"bronze", `bronz`}, {"silver", `gumus|silver`}, {"gold", `altin|gold`},
	}

	timesRe = regexp.MustCompile(`en az (\d+) (kez|kere|defa)`)
	// "en fazla 2 kez", and "yalnızca 1 kez" / "sadece bir kere" (exactly N).
	atMostRe = regexp.MustCompile(`en fazla (\d+) (kez|kere|defa)`)
	onlyRe   = regexp.MustCompile(`(yalnizca|sadece|yalniz) (\d+|bir) (kez|kere|defa)`)
	// "ortalama sipariş tutarı", "ortalama sepet tutarı": the amount that
	// follows is an average per order, not a total.
	avgRe    = regexp.MustCompile(`ortalama (siparis|sepet)\S*( tutar\S*)?`)
	spentRe  = regexp.MustCompile(`(\d+)\s*(tl|lira)\S*\s+(uzeri|ustu|fazla|ve uzeri|alti|altinda|az)`)
	ordersRe = regexp.MustCompile(`en az (\d+) siparis|(\d+)\S*\s+fazla siparis`)
	signupRe = regexp.MustCompile(`kayit ol|uye ol|yeni uye`)

	// An explicit request for all users (DECISIONS 43).
	everyoneRe = regexp.MustCompile(`\b(herkese|(tum|butun) (kullanici|musteri|uye)\w*)`)

	excludeRe = regexp.MustCompile(`disinda|haric`)
	ageSideRe = regexp.MustCompile(`(\d+)\s*yas\w*\s+(buyuk|kucuk|ustu|uzeri|alti)`)

	// What the matcher would otherwise drop without a word. Each entry is a
	// requirement the rule format (or this matcher) cannot express.
	refusals = []struct {
		pattern *regexp.Regexp
		reason  string
	}{
		{regexp.MustCompile(`(yalnizca|sadece|yalniz)\s+(sms|email|e-posta|eposta|mail)|whatsapp|push|bildirim`),
			"Kanal seçimi kural formatında yok: kampanya, izni olan herkese hem SMS hem email olarak gider. WhatsApp ve push desteklenmiyor."},
		{regexp.MustCompile(`almanca|fransizca|arapca|rusca|ispanyolca|italyanca|kurtce`),
			"Bu dil veride yok. Kullanıcıların dili Türkçe ya da İngilizce."},
		{regexp.MustCompile(`(^|\s)-\s?\d+`),
			"Negatif bir değer kitle koşulu olamaz."},
		{regexp.MustCompile(`\b(users|customers|send|discount|who|the|last \d+ days)\b`),
			"Mock LLM yalnızca Türkçe brief'leri tanır; başka dilde brief için gerçek modeli kullanın (ANTHROPIC_API_KEY)."},
	}

	categoryHints = []struct{ hint, category string }{
		{"elektronik", "electronics"}, {"moda", "fashion"}, {"giyim", "fashion"},
		{"kozmetik", "cosmetics"}, {"mobilya", "home"}, {"spor", "sports"}, {"kitap", "books"},
	}
	notPurchased = []string{"alisveris yapmayan", "satin almayan", "alisveris yapmamis", "satin almamis", "alisveris yapmadi", "satin almadi", "almayan"}
	purchased    = []string{"alisveris yapan", "satin alan", "alisveris yapmis", "satin almis", "alisveris yapti", "satin aldi"}
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
				"reason":  "Kural formatı sıralama, doğum günü, bronz / gümüş / altın dışındaki sadakat seviyeleri, cihaz ya da ürün görüntüleme koşullarını ifade edemiyor.",
				"phrases": []string{original(brief, text, hint)},
			}
		}
	}

	// A requirement this matcher cannot express must not simply be dropped:
	// the rest of the brief would then select a wider audience than was asked
	// for, or send on a channel that was not. Refuse and say why.
	for _, r := range refusals {
		if m := r.pattern.FindString(text); m != "" {
			return map[string]any{"kind": "unsupported", "reason": r.reason, "phrases": []string{original(brief, text, strings.TrimSpace(m))}}
		}
	}
	if _, yes := firstMatch(text, purchased); yes {
		if no, also := firstMatch(text, notPurchased); also {
			return map[string]any{
				"kind":    "unsupported",
				"reason":  "Brief hem alışveriş yapanları hem yapmayanları istiyor; bu iki koşul aynı kişide birlikte sağlanamaz.",
				"phrases": []string{original(brief, text, no)},
			}
		}
	}

	// A city the data does not cover must not simply be ignored: the rest of
	// the brief would then select a much wider audience than was asked for.
	days := 30
	windows := map[int]string{}
	for _, m := range windowRe.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		n *= map[string]int{"gun": 1, "hafta": 7, "ay": 30}[m[2]]
		if len(windows) == 0 {
			days = n
		}
		windows[n] = m[0]
	}
	if len(windows) > 1 {
		// This matcher has one window for the whole brief. Using the first for
		// every condition would change what the second one means.
		var phrases []string
		for _, phrase := range windows {
			phrases = append(phrases, original(brief, text, phrase))
		}
		slices.Sort(phrases)
		return map[string]any{
			"kind":    "unsupported",
			"reason":  "Mock LLM bir brief'te tek bir dönem (\"son N gün\") tanır; koşullar farklı dönemler taşıyorsa gerçek modeli kullanın.",
			"phrases": phrases,
		}
	}

	category, categoryHint := "", ""
	for _, h := range categoryHints {
		if strings.Contains(text, h.hint) {
			category, categoryHint = h.category, h.hint
			break
		}
	}

	// matchIn recognises the conditions in text. It is the whole brief, or
	// one side of an "or" with the other side blanked out, so positions still
	// line up with the brief.
	matchIn := func(text string) ([]segment.Condition, []segment.Assumption) {
		var conditions []segment.Condition
		var assumptions []segment.Assumption

		cities := citiesIn(text)
		if len(cities) > 0 {
			// "İstanbul dışındaki", "Ankara hariç": everyone but these cities.
			op := segment.OpIn
			if excludeRe.MatchString(text) {
				op = segment.OpNotIn
			}
			conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "city", Op: op, Values: cities})
		}
		switch english, turkish := strings.Contains(text, "ingilizce"), strings.Contains(text, "turkce"); {
		case english && !turkish:
			conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "language", Op: segment.OpIn, Values: []string{"en"}})
		case turkish && !english:
			conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "language", Op: segment.OpIn, Values: []string{"tr"}})
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
		var tiers []string
		for _, phrase := range tierPhraseRe.FindAllString(text, -1) {
			for _, t := range tierWords {
				if regexp.MustCompile(t.words).MatchString(phrase) && !slices.Contains(tiers, t.tier) {
					tiers = append(tiers, t.tier)
				}
			}
		}
		// Lowest first, whatever order the brief names them in.
		slices.SortFunc(tiers, func(a, b string) int { return slices.Index(seed.LoyaltyTiers, a) - slices.Index(seed.LoyaltyTiers, b) })
		// All three named is everyone with a tier, which is everyone: not a
		// condition, and the validator would say so.
		if len(tiers) > 0 && len(tiers) < len(tierWords) {
			conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: "loyalty_tier", Op: segment.OpIn, Values: tiers})
		}
		if m := ageRe.FindStringSubmatch(text); m != nil {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: lo, Max: hi})
		} else if m := ageSideRe.FindStringSubmatch(text); m != nil {
			// "25 yaşından büyük", "40 yaş altı": one bound; the other is the format's own.
			age, _ := strconv.Atoi(m[1])
			lo, hi, meaning := age+1, segment.MaxAge, fmt.Sprintf("%d yaş ve üzeri", age+1)
			if strings.HasPrefix(m[2], "kucuk") || strings.HasPrefix(m[2], "alt") {
				lo, hi, meaning = segment.MinAge, age-1, fmt.Sprintf("%d yaş ve altı", age-1)
			}
			conditions = append(conditions, segment.Condition{Type: segment.TypeAttribute, Field: segment.FieldAge, Op: segment.OpBetween, Min: lo, Max: hi})
			assumptions = append(assumptions, segment.Assumption{
				ConditionIndex: len(conditions) - 1, Phrase: original(brief, text, m[0]),
				Interpretation: meaning + " olarak yorumlandı; " + strconv.Itoa(age) + " yaşın kendisi dahil değil.",
			})
		}

		if signupRe.MatchString(text) {
			conditions = append(conditions, segment.Condition{Type: segment.TypeSignup, Op: segment.OpWithin, Days: days})
		}
		average := avgRe.MatchString(text)
		if m := spentRe.FindStringSubmatch(text); m != nil {
			value, _ := strconv.Atoi(m[1])
			op, bound := segment.OpGTE, "en az"
			if strings.HasPrefix(m[3], "alt") || m[3] == "az" {
				op, bound = segment.OpLTE, "en fazla"
			}
			metric, name := segment.MetricTotalSpent, "toplam harcama"
			if average {
				metric, name = segment.MetricAvgOrderValue, "ortalama sipariş tutarı"
			}
			conditions = append(conditions, segment.Condition{Type: segment.TypeMetric, Metric: metric, Op: op, Value: value})
			assumptions = append(assumptions, segment.Assumption{
				ConditionIndex: len(conditions) - 1, Phrase: original(brief, text, m[0]),
				Interpretation: fmt.Sprintf("Tüm zamanlardaki %s %s %d TL olarak yorumlandı; %d TL'nin kendisi dahil.", name, bound, value, value),
			})
		}
		// From here on "ortalama sepet tutarı" must not read as a cart event.
		text = avgRe.ReplaceAllStringFunc(text, func(m string) string { return strings.Repeat(" ", len(m)) })
		if m := ordersRe.FindStringSubmatch(text); m != nil {
			value, _ := strconv.Atoi(m[1] + m[2])
			if m[2] != "" {
				value++ // "3'ten fazla" means at least 4
			}
			conditions = append(conditions, segment.Condition{Type: segment.TypeMetric, Metric: segment.MetricOrderCount, Op: segment.OpGTE, Value: value})
		}
		minCount, maxCount := 0, 0
		if m := timesRe.FindStringSubmatch(text); m != nil {
			minCount, _ = strconv.Atoi(m[1])
		}
		if m := atMostRe.FindStringSubmatch(text); m != nil {
			maxCount, _ = strconv.Atoi(m[1])
		}
		if m := onlyRe.FindStringSubmatch(text); m != nil {
			n := 1
			if m[2] != "bir" {
				n, _ = strconv.Atoi(m[2])
			}
			maxCount = n
			if n > 1 {
				minCount = n
			}
		}

		hasCart := strings.Contains(text, "sepet")
		if hasCart {
			conditions = append(conditions, segment.Condition{Type: segment.TypeEvent, Event: "cart_add", Category: category, WithinDays: days, Exists: true, MinCount: minCount, MaxCount: maxCount})
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
			count, most := minCount, maxCount
			if hasCart {
				count, most = 0, 0 // "en az N kez" already went to the cart condition
			}
			conditions = append(conditions, segment.Condition{Type: segment.TypeEvent, Event: "purchase", Category: cat, WithinDays: days, Exists: true, MinCount: count, MaxCount: most})
		}

		// A category with no cart or purchase condition to carry it belongs to
		// the spend / order figures: "elektronikte 2000 TL üzeri harcayanlar".
		if category != "" {
			carried := false
			for _, c := range conditions {
				carried = carried || (c.Type == segment.TypeEvent && c.Category == category)
			}
			for i := range conditions {
				if carried || conditions[i].Type != segment.TypeMetric {
					continue
				}
				conditions[i].Category = category
				line := segment.Describe(segment.Rule{Match: segment.MatchAll, Conditions: conditions[i : i+1]}).Conditions[0]
				assumptions = append(assumptions, segment.Assumption{
					ConditionIndex: i, Phrase: original(brief, text, categoryHint),
					Interpretation: "Yalnızca bu kategorideki alışverişler sayıldı: " + line + ".",
				})
			}
		}
		return conditions, assumptions
	}

	match := segment.MatchAll
	conditions, assumptions := matchIn(text)

	// "A veya B" between two cities is one condition (city in A, B). Any other
	// "or" joins different conditions. Treating it as "and" would select a
	// different audience, so it becomes what it says: an "or" of the condition
	// just before it and the one just after it (DECISIONS 38).
	if ors := orsBetweenConditions(text); len(ors) > 1 {
		return map[string]any{
			"kind":    "unsupported",
			"reason":  "Mock LLM bir brief'te farklı koşulları bağlayan tek bir \"veya\" tanır; birden fazlası için gerçek modeli kullanın.",
			"phrases": []string{original(brief, text, ors[1].phrase)},
		}
	} else if sides, notes, ok := clauses(brief, text, ors, matchIn); ok {
		// Each side of the "or" names its own city: two separate audiences.
		match, conditions, assumptions = segment.MatchAny, sides, notes
	} else if len(ors) == 1 {
		// The pair is the recognised condition nearest before the "or" and the
		// nearest after it, by where their words are in the sentence.
		or := ors[0]
		left, right := -1, -1
		leftAt, rightAt := -1, len(text)+1
		for i, c := range conditions {
			switch p := positionOf(c, text); {
			case p < 0:
			case p < or.start && p > leftAt:
				left, leftAt = i, p
			case p >= or.end && p < rightAt:
				right, rightAt = i, p
			}
		}
		if left < 0 || right < 0 {
			return map[string]any{
				"kind":    "unsupported",
				"reason":  "\"veya\"nın iki yanında da tanınan bir koşul olmalı; mock LLM birini bulamadı.",
				"phrases": []string{original(brief, text, or.phrase)},
			}
		}
		if len(conditions) == 2 {
			// Nothing else in the brief: a plain "any" rule, no group needed.
			match = segment.MatchAny
		} else {
			first, second := min(left, right), max(left, right)
			either := []segment.Condition{conditions[left], conditions[right]}
			lines := segment.Describe(segment.Rule{Match: segment.MatchAny, Conditions: either}).Conditions
			// The group takes the place of whichever of the two came first.
			moved := make([]int, len(conditions))
			var grouped []segment.Condition
			for i, c := range conditions {
				switch i {
				case first:
					moved[i] = len(grouped)
					grouped = append(grouped, segment.Condition{Type: segment.TypeGroup, Match: segment.MatchAny, Conditions: either})
				case second:
					moved[i] = moved[first]
				default:
					moved[i] = len(grouped)
					grouped = append(grouped, c)
				}
			}
			for i := range assumptions {
				assumptions[i].ConditionIndex = moved[assumptions[i].ConditionIndex]
			}
			assumptions = append(assumptions, segment.Assumption{
				ConditionIndex: moved[first], Phrase: original(brief, text, or.phrase),
				Interpretation: fmt.Sprintf("“%s” ile “%s” koşullarından en az biri yeterli sayıldı; diğer koşulların hepsi ayrıca sağlanmalı.", lines[0], lines[1]),
			})
			conditions = grouped
		}
	}

	// A purchase that was mentioned but not recognised must not vanish.
	if strings.Contains(text, "alisveris") || strings.Contains(text, "satin al") {
		understood := false
		for _, c := range (segment.Rule{Conditions: conditions}).Leaves() {
			understood = understood || (c.Type == segment.TypeEvent && c.Event == "purchase")
		}
		if !understood {
			return map[string]any{
				"kind":    "unsupported",
				"reason":  "Brief bir alışverişten söz ediyor ama mock LLM bu ifadeyi tanımadı; \"alışveriş yapan\" ya da \"satın almayan\" gibi yazın ya da gerçek modeli kullanın.",
				"phrases": []string{},
			}
		}
	}

	if len(conditions) == 0 {
		// Asked for in so many words, and nothing else was: everyone. With a
		// condition found, "herkese" is just how the sentence ends ("İstanbul'da
		// yaşayan herkese") and the conditions above are the rule.
		if everyoneRe.MatchString(text) {
			return map[string]any{
				"kind":        "rule",
				"rule":        segment.Rule{Match: segment.MatchAll, Everyone: true},
				"assumptions": []segment.Assumption{},
				"schedule":    schedule,
			}
		}
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
		"rule":        segment.Rule{Match: match, Conditions: conditions},
		"assumptions": assumptions,
		"schedule":    schedule,
	}
}

// clauses reads a brief whose single "or" separates two audiences, each with
// a city of its own: "İstanbul'da sepete ekleyen veya Ankara'da 1000 TL
// harcayan". Reading the whole sentence at once would merge the two cities
// into one condition and lose which requirement belongs to which. Each side
// is matched on its own and becomes one condition of an "any" rule: a group
// when it has several conditions, the condition itself when it has one.
func clauses(brief, text string, ors []orPhrase, matchIn func(string) ([]segment.Condition, []segment.Assumption)) ([]segment.Condition, []segment.Assumption, bool) {
	if len(ors) != 1 {
		return nil, nil, false
	}
	or := ors[0]
	// The other side is blanked, not cut, so positions still match the brief.
	left := text[:or.start] + strings.Repeat(" ", len(text)-or.start)
	right := strings.Repeat(" ", or.end) + text[or.end:]
	if len(citiesIn(left)) == 0 || len(citiesIn(right)) == 0 {
		return nil, nil, false
	}
	var sides []segment.Condition
	var notes []segment.Assumption
	var lines []string
	several := false
	for _, side := range []string{left, right} {
		conditions, assumptions := matchIn(side)
		for _, a := range assumptions {
			a.ConditionIndex = len(sides)
			notes = append(notes, a)
		}
		lines = append(lines, strings.Join(segment.Describe(segment.Rule{Match: segment.MatchAll, Conditions: conditions}).Conditions, " + "))
		if len(conditions) == 1 {
			sides = append(sides, conditions[0])
			continue
		}
		several = true
		sides = append(sides, segment.Condition{Type: segment.TypeGroup, Match: segment.MatchAll, Conditions: conditions})
	}
	if !several {
		// Two bare cities: the ordinary reading (city in A, B) already says it.
		return nil, nil, false
	}
	notes = append(notes, segment.Assumption{
		ConditionIndex: 0, Phrase: original(brief, text, or.phrase),
		Interpretation: fmt.Sprintf("Brief iki ayrı kitle olarak okundu: “%s” ya da “%s”. Birini sağlayan kullanıcı kitleye girer.", lines[0], lines[1]),
	})
	return sides, notes, true
}

// positionOf says where in the folded brief a condition was read from, so
// conditions can be related to the words around them. -1 when unknown.
func positionOf(c segment.Condition, text string) int {
	first := func(words ...string) int {
		at := -1
		for _, w := range words {
			if i := strings.Index(text, w); i >= 0 && (at < 0 || i < at) {
				at = i
			}
		}
		return at
	}
	match := func(re *regexp.Regexp) int {
		if loc := re.FindStringIndex(text); loc != nil {
			return loc[0]
		}
		return -1
	}
	switch {
	case c.Type == segment.TypeSignup:
		return match(signupRe)
	case c.Type == segment.TypeMetric && c.Metric != segment.MetricOrderCount:
		return match(spentRe)
	case c.Type == segment.TypeMetric:
		return match(ordersRe)
	case c.Type == segment.TypeEvent && c.Event == "cart_add":
		return first("sepet")
	case c.Type == segment.TypeEvent:
		return first(append(append([]string{}, notPurchased...), purchased...)...)
	case c.Field == segment.FieldAge:
		if at := match(ageRe); at >= 0 {
			return at
		}
		return match(ageSideRe)
	case c.Field == "city":
		for _, span := range wordAtRe.FindAllStringIndex(text, -1) {
			if city := wordCity(text[span[0]:span[1]]); city != "" && slices.Contains(c.Values, city) {
				return span[0]
			}
		}
		return -1
	case c.Field == "language":
		return first("ingilizce", "turkce")
	case c.Field == "gender":
		return first("kadin", "erkek")
	case c.Field == "loyalty_tier":
		return first("bronz", "gumus", "silver", "altin", "gold")
	}
	return -1
}

// cityAliases are common other names of a province, folded.
var cityAliases = map[string]string{
	"afyon": "Afyonkarahisar", "urfa": "Şanlıurfa", "maras": "Kahramanmaraş", "antep": "Gaziantep",
	"izmit": "Kocaeli", "icel": "Mersin", "adapazari": "Sakarya", "antakya": "Hatay",
}

// citySuffixRe is what may follow a city name written without an apostrophe:
// "istanbulda", "ankaradakilere", "izmirli", "istanbulluları". Bare vowels
// and "-luk" are left out on purpose, so "karşı" is not Kars, "müşteri" is
// not Muş and "musluk" is not either.
var citySuffixRe = regexp.MustCompile(`^([dt][ae]|[dt][ae]ki[a-z]*|[dt][ae]n|l[iu]|l[iu]y[aeiu]|l[iu]l[ae]r[a-z]{0,4})$`)

var cityByFold = func() map[string]string {
	out := map[string]string{}
	for _, city := range seed.Cities() {
		out[segment.Fold(city)] = city
	}
	for alias, city := range cityAliases {
		out[alias] = city
	}
	return out
}()

// wordCity returns the city a folded word names, or "". With 81 provinces a
// substring search would find Muş in "müşteri" and Ordu in "ordusu", so a
// word counts only when it is the name itself, the name before an
// apostrophe, or the name with one of a few suffixes.
func wordCity(word string) string {
	w := strings.Trim(word, ".,;:!?()\"")
	if i := strings.IndexAny(w, "'’"); i >= 0 {
		return cityByFold[w[:i]]
	}
	if city, ok := cityByFold[w]; ok {
		return city
	}
	best := ""
	for name := range cityByFold {
		if len(name) > len(best) && strings.HasPrefix(w, name) && citySuffixRe.MatchString(w[len(name):]) {
			best = name
		}
	}
	return cityByFold[best]
}

// citiesIn lists the cities a folded text names, in the order of the data.
func citiesIn(text string) []string {
	named := map[string]bool{}
	for _, w := range strings.Fields(text) {
		if city := wordCity(w); city != "" {
			named[city] = true
		}
	}
	var out []string
	for _, city := range seed.Cities() {
		if named[city] {
			out = append(out, city)
		}
	}
	return out
}

// orPhrase is an "or" in the brief that joins two different conditions.
type orPhrase struct {
	phrase     string
	start, end int // byte offsets in the folded text
}

var wordAtRe = regexp.MustCompile(`\S+`)

// orsBetweenConditions finds every "veya" / "ya da" that is not between two
// cities (which is a single condition: city in A, B).
func orsBetweenConditions(text string) []orPhrase {
	isCity := func(word string) bool { return wordCity(word) != "" }
	spans := wordAtRe.FindAllStringIndex(text, -1)
	word := func(i int) string { return text[spans[i][0]:spans[i][1]] }
	var found []orPhrase
	for i := range spans {
		or, next := orPhrase{start: spans[i][0]}, i+1
		switch {
		case word(i) == "veya":
			or.phrase, or.end = "veya", spans[i][1]
		case word(i) == "ya" && i+1 < len(spans) && word(i+1) == "da":
			or.phrase, or.end, next = "ya da", spans[i+1][1], i+2
		default:
			continue
		}
		if i == 0 || next >= len(spans) || !isCity(word(i-1)) || !isCity(word(next)) {
			found = append(found, or)
		}
	}
	return found
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
