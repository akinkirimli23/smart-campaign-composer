package segment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/validation"
)

// FieldError and ValidationError are the shared validation error types; the
// aliases keep this package's API in one place.
type (
	FieldError      = validation.FieldError
	ValidationError = validation.Errors
)

type collector struct{ errs []FieldError }

func (c *collector) add(path, format string, args ...any) {
	c.errs = append(c.errs, FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
}

func joinPath(prefix, path string) string { return validation.JoinPath(prefix, path) }

// Parse decodes and validates a rule. It is strict: unknown keys, keys that
// belong to another condition variant, wrong types and out-of-set values are
// all errors. A nil error means the rule can be compiled.
func Parse(data []byte) (Rule, error) {
	var col collector
	rule, malformed, ok := parseRule(&col, data)
	if ok {
		// Conditions that failed structurally are skipped, the rest are still
		// checked, so a single reply lists every problem.
		col.errs = append(col.errs, rule.validate(malformed)...)
	}
	if len(col.errs) > 0 {
		return Rule{}, ValidationError(col.errs)
	}
	return rule, nil
}

// parseRule decodes the structure of a rule. malformed holds the indexes of
// conditions that could not be decoded; they stay in the slice as placeholders
// so the indexes in later error paths match the input.
func parseRule(col *collector, data []byte) (rule Rule, malformed map[int]bool, ok bool) {
	obj, ok := object(col, "", data)
	if !ok {
		return Rule{}, nil, false
	}
	malformed = map[int]bool{}
	checkKeys(col, "", obj, []string{"match", "conditions"}, nil)

	matchOK := false
	if raw, present := obj["match"]; present {
		if s, ok := str(col, "match", raw); ok {
			rule.Match, matchOK = Match(s), true
		}
	}
	if !matchOK {
		rule.Match = MatchAll // already reported; keeps validate from reporting it twice
	}
	if raw, present := obj["conditions"]; present {
		var items []json.RawMessage
		if isNull(raw) || json.Unmarshal(raw, &items) != nil {
			col.add("conditions", "dizi olmalı")
			malformed[-1] = true
		}
		for i, item := range items {
			c, ok := parseCondition(col, fmt.Sprintf("conditions[%d]", i), item)
			if !ok {
				malformed[i] = true
			}
			rule.Conditions = append(rule.Conditions, c)
		}
	} else {
		malformed[-1] = true // conditions key missing: reported by checkKeys
	}
	return rule, malformed, true
}

func parseCondition(col *collector, path string, data []byte) (Condition, bool) {
	obj, ok := object(col, path, data)
	if !ok {
		return Condition{}, false
	}
	rawType, present := obj["type"]
	if !present {
		col.add(joinPath(path, "type"), "zorunlu alan eksik; izinli değerler: %s", strings.Join(conditionTypes, ", "))
		return Condition{}, false
	}
	typ, ok := str(col, joinPath(path, "type"), rawType)
	if !ok {
		return Condition{}, false
	}

	before := len(col.errs)
	c := Condition{Type: typ}
	switch typ {
	case TypeAttribute:
		rawField, present := obj["field"]
		if !present {
			col.add(joinPath(path, "field"), "zorunlu alan eksik; izinli alanlar: %s", strings.Join(attributeNames, ", "))
			return Condition{}, false
		}
		field, ok := str(col, joinPath(path, "field"), rawField)
		if !ok {
			return Condition{}, false
		}
		c.Field = field
		if _, isSet := setFields[field]; !isSet && field != FieldAge {
			col.add(joinPath(path, "field"), "%q bilinmeyen bir alan; izinli alanlar: %s", field, strings.Join(attributeNames, ", "))
			return Condition{}, false
		}
		if field == FieldAge {
			checkKeys(col, path, obj, []string{"type", "field", "op", "min", "max"}, nil)
			c.Min, _ = integer(col, joinPath(path, "min"), obj["min"])
			c.Max, _ = integer(col, joinPath(path, "max"), obj["max"])
		} else {
			checkKeys(col, path, obj, []string{"type", "field", "op", "values"}, nil)
			c.Values = stringList(col, joinPath(path, "values"), obj["values"])
		}
		c.Op, _ = str(col, joinPath(path, "op"), obj["op"])
	case TypeEvent:
		checkKeys(col, path, obj, []string{"type", "event", "within_days", "exists"}, []string{"category", "min_count"})
		c.Event, _ = str(col, joinPath(path, "event"), obj["event"])
		c.WithinDays, _ = integer(col, joinPath(path, "within_days"), obj["within_days"])
		c.Exists, _ = boolean(col, joinPath(path, "exists"), obj["exists"])
		// category is optional; null means "any category", like leaving it out.
		if raw, present := obj["category"]; present && !isNull(raw) {
			c.Category, _ = str(col, joinPath(path, "category"), raw)
			if c.Category == "" && len(col.errs) == before {
				col.add(joinPath(path, "category"), "boş olamaz; tüm kategoriler için alanı kaldırın")
			}
		}
		// min_count is optional; null means "at least once", like leaving it out.
		if raw, present := obj["min_count"]; present && !isNull(raw) {
			if n, ok := integer(col, joinPath(path, "min_count"), raw); ok {
				if n < 1 {
					col.add(joinPath(path, "min_count"), "%d geçersiz; 1 ile %d arasında olmalı", n, MaxMinCount)
				}
				c.MinCount = n
			}
		}
	case TypeSignup:
		checkKeys(col, path, obj, []string{"type", "op", "days"}, nil)
		c.Op, _ = str(col, joinPath(path, "op"), obj["op"])
		c.Days, _ = integer(col, joinPath(path, "days"), obj["days"])
	case TypeMetric:
		checkKeys(col, path, obj, []string{"type", "metric", "op", "value"}, []string{"within_days"})
		c.Metric, _ = str(col, joinPath(path, "metric"), obj["metric"])
		c.Op, _ = str(col, joinPath(path, "op"), obj["op"])
		c.Value, _ = integer(col, joinPath(path, "value"), obj["value"])
		// within_days is optional; null means "all time", like leaving it out.
		if raw, present := obj["within_days"]; present && !isNull(raw) {
			if n, ok := integer(col, joinPath(path, "within_days"), raw); ok {
				if n < MinWithinDays {
					col.add(joinPath(path, "within_days"), "%d geçersiz; %d ile %d arasında olmalı (tüm zamanlar için alanı kaldırın)", n, MinWithinDays, MaxWithinDays)
				}
				c.WithinDays = n
			}
		}
	default:
		col.add(joinPath(path, "type"), "%q bilinmeyen bir koşul tipi; izinli değerler: %s", typ, strings.Join(conditionTypes, ", "))
		return Condition{}, false
	}
	return c, len(col.errs) == before
}

// --- strict JSON helpers ------------------------------------------------------
//
// encoding/json silently ignores unknown keys and converts null to zero values.
// These helpers make every such case an explicit error with a path.

func object(col *collector, path string, data []byte) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		if path == "" && !json.Valid(data) {
			col.add(path, "geçerli JSON değil: %v", err)
		} else {
			col.add(path, "JSON nesnesi olmalı")
		}
		return nil, false
	}
	return obj, true
}

// checkKeys reports unknown keys and missing required ones.
func checkKeys(col *collector, path string, obj map[string]json.RawMessage, required, optional []string) {
	allowed := append(append([]string{}, required...), optional...)
	known := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		known[k] = true
	}
	var unknown []string
	for k := range obj {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown) // deterministic error order
	for _, k := range unknown {
		col.add(joinPath(path, k), "bilinmeyen alan; izinli alanlar: %s", strings.Join(allowed, ", "))
	}
	for _, k := range required {
		if _, ok := obj[k]; !ok {
			col.add(joinPath(path, k), "zorunlu alan eksik")
		}
	}
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// The typed helpers return ok=false without reporting when raw is absent:
// checkKeys has already reported the missing key.

func str(col *collector, path string, raw json.RawMessage) (string, bool) {
	if raw == nil {
		return "", false
	}
	var s string
	if isNull(raw) || json.Unmarshal(raw, &s) != nil {
		col.add(path, "metin olmalı")
		return "", false
	}
	return s, true
}

func integer(col *collector, path string, raw json.RawMessage) (int, bool) {
	if raw == nil {
		return 0, false
	}
	var n int
	if isNull(raw) || json.Unmarshal(raw, &n) != nil {
		col.add(path, "tam sayı olmalı")
		return 0, false
	}
	return n, true
}

func boolean(col *collector, path string, raw json.RawMessage) (bool, bool) {
	if raw == nil {
		return false, false
	}
	var b bool
	if isNull(raw) || json.Unmarshal(raw, &b) != nil {
		col.add(path, "true ya da false olmalı")
		return false, false
	}
	return b, true
}

func stringList(col *collector, path string, raw json.RawMessage) []string {
	if raw == nil {
		return nil
	}
	var items []json.RawMessage
	if isNull(raw) || json.Unmarshal(raw, &items) != nil {
		col.add(path, "metin dizisi olmalı")
		return nil
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		if s, ok := str(col, fmt.Sprintf("%s[%d]", path, i), item); ok {
			out = append(out, s)
		}
	}
	return out
}
