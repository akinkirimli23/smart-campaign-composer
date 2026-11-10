package segment

import (
	"fmt"
	"slices"
	"strings"
)

// Validate checks a rule built in Go the same way Parse checks one from JSON.
// Compile calls it, so no rule reaches SQL unvalidated whatever its origin.
func (r Rule) Validate() error {
	if errs := r.validate(nil); len(errs) > 0 {
		return ValidationError(errs)
	}
	return nil
}

// validate checks everything Parse's structural pass cannot. skip lists
// condition indexes that already failed structurally (and -1 when the
// conditions key itself was missing or not an array).
func (r Rule) validate(skip map[int]bool) []FieldError {
	var col collector

	if r.Match != MatchAll && r.Match != MatchAny {
		col.add("match", "%q geçersiz; izinli değerler: %s, %s", r.Match, MatchAll, MatchAny)
	}
	switch n := len(r.Conditions); {
	case skip[-1]:
	case n == 0:
		// An empty rule would select every user; that must be an explicit choice
		// in the product, not something an LLM can produce by omission.
		col.add("conditions", "en az bir koşul gerekli")
	case n > MaxConditions:
		col.add("conditions", "en fazla %d koşul olabilir (%d verildi)", MaxConditions, n)
	}

	for i, c := range r.Conditions {
		path := fmt.Sprintf("conditions[%d]", i)
		if skip[i] {
			continue
		}
		switch c.Type {
		case TypeAttribute:
			if c.Field == FieldAge {
				validateAge(&col, path, c)
			} else if f, ok := setFields[c.Field]; ok {
				validateSet(&col, path, c, f)
			} else {
				col.add(path+".field", "%q bilinmeyen bir alan; izinli alanlar: %s", c.Field, strings.Join(attributeNames, ", "))
			}
		case TypeEvent:
			validateEvent(&col, path, c)
		default:
			col.add(path+".type", "%q bilinmeyen bir koşul tipi; izinli değerler: %s, %s", c.Type, TypeAttribute, TypeEvent)
		}
	}

	// Cross-condition checks only make sense once each condition is valid.
	if len(col.errs) == 0 && len(skip) == 0 {
		r.validateTogether(&col)
	}
	return col.errs
}

func validateSet(col *collector, path string, c Condition, f setField) {
	if c.Op != OpIn && c.Op != OpNotIn {
		col.add(path+".op", "%q operatörü %s alanında kullanılamaz; izinli operatörler: %s, %s", c.Op, c.Field, OpIn, OpNotIn)
	}
	allowed := f.values()
	if len(c.Values) == 0 {
		col.add(path+".values", "en az bir değer gerekli; geçerli değerler: %s", strings.Join(allowed, ", "))
		return
	}
	seen := map[string]bool{}
	valid := true
	for i, v := range c.Values {
		vpath := fmt.Sprintf("%s.values[%d]", path, i)
		switch {
		case !slices.Contains(allowed, v):
			valid = false
			col.add(vpath, "%q geçerli bir %s değil%s; geçerli değerler: %s", v, f.noun, suggestion(v, allowed), strings.Join(allowed, ", "))
		case seen[v]:
			valid = false
			col.add(vpath, "%q birden fazla kez yazılmış", v)
		}
		seen[v] = true
	}
	// Listing the whole closed set makes the condition select everyone (in) or
	// no one (not_in); neither is something a marketer means.
	if valid && len(c.Values) == len(allowed) {
		if c.Op == OpIn {
			col.add(path+".values", "tüm %s değerleri seçilmiş, koşul hiçbir şeyi filtrelemiyor; koşulu kaldırın", f.noun)
		} else if c.Op == OpNotIn {
			col.add(path+".values", "tüm %s değerleri dışlanmış, koşul kimseyi seçmez", f.noun)
		}
	}
}

func validateAge(col *collector, path string, c Condition) {
	if c.Op != OpBetween {
		col.add(path+".op", "%q operatörü age alanında kullanılamaz; izinli operatör: %s", c.Op, OpBetween)
	}
	if c.Min < MinAge || c.Min > MaxAge {
		col.add(path+".min", "%d geçersiz; %d ile %d arasında olmalı", c.Min, MinAge, MaxAge)
	}
	if c.Max < MinAge || c.Max > MaxAge {
		col.add(path+".max", "%d geçersiz; %d ile %d arasında olmalı", c.Max, MinAge, MaxAge)
	}
	if c.Min > c.Max {
		col.add(path+".min", "min (%d) max'tan (%d) büyük olamaz", c.Min, c.Max)
	}
}

func validateEvent(col *collector, path string, c Condition) {
	if !slices.Contains(eventTypes, c.Event) {
		col.add(path+".event", "%q geçerli bir olay değil; geçerli değerler: %s", c.Event, strings.Join(eventTypes, ", "))
	}
	if c.Category != "" && !slices.Contains(categories(), c.Category) {
		col.add(path+".category", "%q geçerli bir kategori değil%s; geçerli değerler: %s",
			c.Category, suggestion(c.Category, categories()), strings.Join(categories(), ", "))
	}
	if c.WithinDays < MinWithinDays || c.WithinDays > MaxWithinDays {
		col.add(path+".within_days", "%d geçersiz; %d ile %d arasında olmalı", c.WithinDays, MinWithinDays, MaxWithinDays)
	}
}

// validateTogether rejects condition combinations that are individually valid
// but redundant or contradictory, so a compiled rule always means something.
func (r Rule) validateTogether(col *collector) {
	firstAttr := map[string]int{}
	for i, c := range r.Conditions {
		path := fmt.Sprintf("conditions[%d]", i)
		if c.Type == TypeAttribute {
			if j, dup := firstAttr[c.Field]; dup {
				col.add(path+".field", "%s alanı conditions[%d] içinde zaten kullanılmış; tek koşulda birleştirin", c.Field, j)
			} else {
				firstAttr[c.Field] = i
			}
			continue
		}
		for j, other := range r.Conditions[:i] {
			if other.Type != TypeEvent || other.Event != c.Event {
				continue
			}
			if other.Category == c.Category && other.WithinDays == c.WithinDays {
				if other.Exists == c.Exists {
					col.add(path, "conditions[%d] ile aynı koşul", j)
				} else if r.Match == MatchAll {
					col.add(path, "conditions[%d] ile çelişiyor; ikisi birlikte kimseyi seçmez", j)
				}
				continue
			}
			if r.Match == MatchAll && other.Exists != c.Exists {
				pos, neg := other, c
				if c.Exists {
					pos, neg = c, other
				}
				// "did X within 7 days" AND "did not do X within 30 days" is empty
				// whenever the negative condition covers the positive one.
				if neg.WithinDays >= pos.WithinDays && (neg.Category == "" || neg.Category == pos.Category) {
					col.add(path, "conditions[%d] ile çelişiyor; ikisi birlikte kimseyi seçmez", j)
				}
			}
		}
	}
}

// suggestion returns a " (X mi demek istediniz?)" hint when v matches an allowed
// value once Turkish letters and case are folded, e.g. "Istanbul" -> "İstanbul".
func suggestion(v string, allowed []string) string {
	folded := Fold(v)
	for _, a := range allowed {
		if Fold(a) == folded {
			return fmt.Sprintf(" (%q mi demek istediniz?)", a)
		}
	}
	return ""
}

var trFold = strings.NewReplacer("ç", "c", "ğ", "g", "ı", "i", "İ", "i", "ö", "o", "ş", "s", "ü", "u",
	"Ç", "c", "Ğ", "g", "Ö", "o", "Ş", "s", "Ü", "u", "I", "i")

// Fold lower-cases s and maps Turkish letters to ASCII, for loose comparison only.
func Fold(s string) string {
	return strings.ToLower(trFold.Replace(strings.TrimSpace(s)))
}
