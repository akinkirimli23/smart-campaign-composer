package segment

import (
	"fmt"
	"strings"
)

// Description is the human-readable form of a rule shown to the marketer.
type Description struct {
	Summary    string   `json:"summary"`
	Conditions []string `json:"conditions"` // one line per condition, same order as the rule
}

var (
	valueLabels = map[string]map[string]string{
		"language": {"tr": "Türkçe", "en": "İngilizce"},
		"gender":   {"female": "Kadın", "male": "Erkek"},
	}
	fieldLabels    = map[string]string{"city": "Şehir", "language": "Dil", "gender": "Cinsiyet"}
	categoryLabels = map[string]string{
		"electronics": "elektronik", "fashion": "moda", "cosmetics": "kozmetik",
		"home": "ev", "sports": "spor", "books": "kitap",
	}
)

// Describe renders the rule in Turkish. It is derived from the rule alone, never
// from LLM text, so what the marketer reads and approves is exactly what Compile
// turns into SQL (DECISIONS 14).
func Describe(r Rule) Description {
	d := Description{Summary: "Aşağıdaki koşulların hepsini sağlayan kullanıcılar"}
	if r.Match == MatchAny {
		d.Summary = "Aşağıdaki koşullardan en az birini sağlayan kullanıcılar"
	}
	if len(r.Conditions) == 1 {
		d.Summary = "Aşağıdaki koşulu sağlayan kullanıcılar"
	}
	for _, c := range r.Conditions {
		d.Conditions = append(d.Conditions, describeCondition(c))
	}
	return d
}

func describeCondition(c Condition) string {
	switch {
	case c.Type == TypeEvent:
		scope := fmt.Sprintf("Son %d günde", c.WithinDays)
		if c.Category != "" {
			scope += " " + label(categoryLabels, c.Category) + " kategorisinde"
		}
		if c.Exists && c.MinCount > 1 {
			scope += fmt.Sprintf(" en az %d kez", c.MinCount)
		}
		switch {
		case c.Event == "cart_add" && c.Exists:
			return scope + " sepete ürün ekledi"
		case c.Event == "cart_add":
			return scope + " sepete ürün eklemedi"
		case c.Exists:
			return scope + " satın alma yaptı"
		default:
			return scope + " hiç satın alma yapmadı"
		}
	case c.Type == TypeSignup:
		if c.Op == OpWithin {
			return fmt.Sprintf("Son %d günde üye oldu", c.Days)
		}
		return fmt.Sprintf("%d günden daha önce üye oldu", c.Days)
	case c.Type == TypeMetric:
		bound := "en az"
		if c.Op == OpLTE {
			bound = "en fazla"
		}
		text := fmt.Sprintf("Sipariş sayısı: %s %d", bound, c.Value)
		if c.Metric == MetricTotalSpent {
			text = fmt.Sprintf("Toplam harcama: %s %d TL", bound, c.Value)
		}
		if c.WithinDays > 0 {
			return text + fmt.Sprintf(" (son %d günde)", c.WithinDays)
		}
		return text + " (tüm zamanlar)"
	case c.Field == FieldAge:
		return fmt.Sprintf("Yaş: %d–%d (doğum yılına göre)", c.Min, c.Max)
	default:
		names := make([]string, len(c.Values))
		for i, v := range c.Values {
			names[i] = label(valueLabels[c.Field], v)
		}
		if c.Op == OpNotIn {
			return fmt.Sprintf("%s: %s dışında", fieldLabels[c.Field], strings.Join(names, ", "))
		}
		return fmt.Sprintf("%s: %s", fieldLabels[c.Field], strings.Join(names, " veya "))
	}
}

func label(labels map[string]string, v string) string {
	if l, ok := labels[v]; ok {
		return l
	}
	return v
}
