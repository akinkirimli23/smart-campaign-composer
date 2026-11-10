// Package segment defines the audience rule an LLM may propose, validates it
// and compiles it to parameterised SQL.
//
// The rule is a flat list of typed conditions (DECISIONS 13). Nothing the LLM
// or a user writes is ever concatenated into SQL: field names and operators are
// looked up in the tables below, and every value travels as a bind parameter.
package segment

import (
	"encoding/json"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
)

// Limits enforced by Validate. The LLM-facing JSON schema cannot express
// numeric or array bounds, so the backend is the only place they are checked.
const (
	MaxConditions = 10
	MinWithinDays = 1
	MaxWithinDays = 365
	MinAge        = 0
	MaxAge        = 120
)

type Match string

const (
	MatchAll Match = "all" // every condition must hold
	MatchAny Match = "any" // at least one condition must hold
)

const (
	TypeAttribute = "attribute"
	TypeEvent     = "event"

	OpIn      = "in"
	OpNotIn   = "not_in"
	OpBetween = "between"

	// FieldAge is the only attribute that is a range rather than a closed set.
	// It is derived from birth_year so the LLM never does date arithmetic.
	FieldAge = "age"
)

// Rule selects users. It is the unit a marketer approves and a campaign stores.
type Rule struct {
	Match      Match       `json:"match"`
	Conditions []Condition `json:"conditions"`
}

// Condition is either an attribute condition or an event condition, told apart
// by Type. Only the fields of that variant are meaningful; Parse rejects JSON
// that sets a field belonging to another variant.
type Condition struct {
	Type string

	// Attribute variant.
	Field  string
	Op     string
	Values []string // closed-set fields (city, language, gender)
	Min    int      // age
	Max    int      // age

	// Event variant: "did (not) do Event (in Category) within the last WithinDays days".
	Event      string
	Category   string // empty: any category
	WithinDays int
	Exists     bool
}

// setField is an attribute whose values come from a closed set.
type setField struct {
	column string          // SQL column; a constant, never derived from input
	values func() []string // the closed set
	noun   string          // Turkish noun used in error messages
}

var setFields = map[string]setField{
	"city":     {column: "u.city", values: seed.Cities, noun: "şehir"},
	"language": {column: "u.language", values: func() []string { return []string{"tr", "en"} }, noun: "dil"},
	"gender":   {column: "u.gender", values: func() []string { return []string{"female", "male"} }, noun: "cinsiyet"},
}

// Stable orders for schemas and messages (map iteration is random).
var (
	setFieldOrder  = []string{"city", "language", "gender"}
	attributeNames = []string{"city", "language", "gender", FieldAge}
	eventTypes     = []string{"cart_add", "purchase"}
)

func categories() []string { return seed.Categories }

// MarshalJSON emits only the fields of the condition's variant, in the same
// shape Parse accepts, so a rule round-trips through the frontend unchanged.
func (c Condition) MarshalJSON() ([]byte, error) {
	switch {
	case c.Type == TypeEvent:
		var category *string
		if c.Category != "" {
			category = &c.Category
		}
		return json.Marshal(struct {
			Type       string  `json:"type"`
			Event      string  `json:"event"`
			Category   *string `json:"category"`
			WithinDays int     `json:"within_days"`
			Exists     bool    `json:"exists"`
		}{c.Type, c.Event, category, c.WithinDays, c.Exists})
	case c.Field == FieldAge:
		return json.Marshal(struct {
			Type  string `json:"type"`
			Field string `json:"field"`
			Op    string `json:"op"`
			Min   int    `json:"min"`
			Max   int    `json:"max"`
		}{c.Type, c.Field, c.Op, c.Min, c.Max})
	default:
		return json.Marshal(struct {
			Type   string   `json:"type"`
			Field  string   `json:"field"`
			Op     string   `json:"op"`
			Values []string `json:"values"`
		}{c.Type, c.Field, c.Op, c.Values})
	}
}

// UnmarshalJSON routes through the strict parser so no code path can decode a
// condition leniently.
func (c *Condition) UnmarshalJSON(data []byte) error {
	var col collector
	parsed, ok := parseCondition(&col, "", data)
	if len(col.errs) > 0 {
		return ValidationError(col.errs)
	}
	if ok {
		*c = parsed
	}
	return nil
}
