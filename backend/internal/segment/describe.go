package segment

import (
	"fmt"
	"strings"
)

// Description is the human-readable form of a rule shown to the marketer.
type Description struct {
	Summary string `json:"summary"`
	// Conditions has one line per condition, in the order of the rule. A
	// group is a single line that names all of its members.
	Conditions []string `json:"conditions"`
	// Groups runs parallel to Conditions: for a group, the same content in
	// parts, so a screen can show its members as a list; nil otherwise.
	Groups []*GroupDescription `json:"groups"`
}

// GroupDescription is a group condition in parts.
type GroupDescription struct {
	Label      string   `json:"label"`      // "Şunlardan en az biri"
	Conditions []string `json:"conditions"` // one line per member
}

var (
	valueLabels = map[string]map[string]string{
		"language":     {"tr": "Türkçe", "en": "İngilizce"},
		"gender":       {"female": "Kadın", "male": "Erkek"},
		"loyalty_tier": {"bronze": "Bronz", "silver": "Gümüş", "gold": "Altın"},
	}
	fieldLabels    = map[string]string{"city": "Şehir", "language": "Dil", "gender": "Cinsiyet", "loyalty_tier": "Sadakat seviyesi"}
	categoryLabels = map[string]string{
		"electronics": "elektronik", "fashion": "moda", "cosmetics": "kozmetik",
		"home": "ev", "sports": "spor", "books": "kitap",
	}
)

// Describe renders the rule in Turkish. It is derived from the rule alone, never
// from LLM text, so what the marketer reads and approves is exactly what Compile
// turns into SQL (DECISIONS 14).
func Describe(r Rule) Description {
	if r.Everyone {
		return Description{Summary: "Tüm kullanıcılar (koşul yok)", Conditions: []string{}}
	}
	d := Description{Summary: "Aşağıdaki koşulların hepsini sağlayan kullanıcılar"}
	if r.Match == MatchAny {
		d.Summary = "Aşağıdaki koşullardan en az birini sağlayan kullanıcılar"
	}
	if len(r.Conditions) == 1 {
		d.Summary = "Aşağıdaki koşulu sağlayan kullanıcılar"
	}
	grouped := false
	for _, c := range r.Conditions {
		if c.Type != TypeGroup {
			d.Conditions = append(d.Conditions, describeCondition(c))
			d.Groups = append(d.Groups, nil)
			continue
		}
		g := &GroupDescription{Label: "Şunların hepsi"}
		if c.Match == MatchAny {
			g.Label = "Şunlardan en az biri"
		}
		for _, member := range c.Conditions {
			g.Conditions = append(g.Conditions, describeCondition(member))
		}
		d.Conditions = append(d.Conditions, g.Label+": "+strings.Join(g.Conditions, "; "))
		d.Groups = append(d.Groups, g)
		grouped = true
	}
	if !grouped {
		d.Groups = nil // a flat rule reads exactly as it did before groups existed
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
		switch {
		case !c.Exists:
		case c.MaxCount > 0 && c.MaxCount == max(c.MinCount, 1):
			scope += fmt.Sprintf(" yalnızca %d kez", c.MaxCount)
		case c.MaxCount > 0 && c.MinCount > 1:
			scope += fmt.Sprintf(" %d–%d kez", c.MinCount, c.MaxCount)
		case c.MaxCount > 0:
			scope += fmt.Sprintf(" en fazla %d kez", c.MaxCount)
		case c.MinCount > 1:
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
		name, unit := "Sipariş sayısı", ""
		switch c.Metric {
		case MetricTotalSpent:
			name, unit = "Toplam harcama", " TL"
		case MetricAvgOrderValue:
			name, unit = "Ortalama sipariş tutarı", " TL"
		}
		if c.Category != "" {
			name += " (" + label(categoryLabels, c.Category) + " kategorisinde)"
		}
		text := fmt.Sprintf("%s: %s %d%s", name, bound, c.Value, unit)
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
