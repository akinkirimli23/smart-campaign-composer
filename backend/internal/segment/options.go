package segment

// Option is one allowed value and the label a marketer sees for it.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Limits are the numeric bounds Validate enforces.
type Limits struct {
	MaxConditions int `json:"max_conditions"`
	MinWithinDays int `json:"min_within_days"`
	MaxWithinDays int `json:"max_within_days"`
	MinAge        int `json:"min_age"`
	MaxAge        int `json:"max_age"`
	MaxMinCount   int `json:"max_min_count"`
	MaxSignupDays int `json:"max_signup_days"`
	MaxSpent      int `json:"max_spent"`
	MaxOrders     int `json:"max_orders"`
}

// Options is everything a form needs to edit a rule without free text: the
// closed sets and the bounds. It is built from the tables Validate uses, so a
// rule assembled from these options is valid field by field. Combinations
// (contradictions, duplicates) are still checked by Parse on submit.
type Options struct {
	Cities     []Option `json:"cities"`
	Languages  []Option `json:"languages"`
	Genders    []Option `json:"genders"`
	Categories []Option `json:"categories"`
	Events     []Option `json:"events"`
	Metrics    []Option `json:"metrics"`
	Limits     Limits   `json:"limits"`
}

var (
	eventLabels  = map[string]string{"cart_add": "Sepete ürün ekleme", "purchase": "Satın alma"}
	metricLabels = map[string]string{MetricTotalSpent: "Toplam harcama (TL)", MetricOrderCount: "Sipariş sayısı"}
)

// RuleOptions returns the values and bounds a rule may use.
func RuleOptions() Options {
	options := func(values []string, labels map[string]string) []Option {
		out := make([]Option, len(values))
		for i, v := range values {
			out[i] = Option{Value: v, Label: capitalise(label(labels, v))}
		}
		return out
	}
	return Options{
		Cities:     options(setFields["city"].values(), nil),
		Languages:  options(setFields["language"].values(), valueLabels["language"]),
		Genders:    options(setFields["gender"].values(), valueLabels["gender"]),
		Categories: options(categories(), categoryLabels),
		Events:     options(eventTypes, eventLabels),
		Metrics:    options(metricNames, metricLabels),
		Limits: Limits{
			MaxConditions: MaxConditions,
			MinWithinDays: MinWithinDays, MaxWithinDays: MaxWithinDays,
			MinAge: MinAge, MaxAge: MaxAge,
			MaxMinCount: MaxMinCount, MaxSignupDays: MaxSignupDays,
			MaxSpent: MaxSpent, MaxOrders: MaxOrders,
		},
	}
}

// capitalise upper-cases the first letter; the labels in this package are
// lower-case because they are also used mid-sentence.
func capitalise(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	switch r[0] {
	case 'i':
		r[0] = 'İ' // Turkish dotted capital
	case 'ı':
		r[0] = 'I'
	default:
		if r[0] >= 'a' && r[0] <= 'z' {
			r[0] -= 'a' - 'A'
		}
	}
	return string(r)
}
