package segment

import (
	"bytes"
	"encoding/json"
	"strings"
)

// What the model writes where a stored rule has null. Nullable fields are
// what makes the compiled grammar of a structured output grow: with them the
// real API rejects this schema as too large (DECISIONS 41). So the model's
// format has no nulls, and fromModel puts them back before Parse sees the rule.
const (
	// modelAnyCategory stands for "no category limit".
	modelAnyCategory = "any"
	// modelNone stands for "no bound" in min_count / max_count and for "all
	// time" in a metric's within_days.
	modelNone = 0
	// conditionRef names the one definition of the plain condition variants.
	// sharedSetMax is the longest value list a closed-set attribute may have
	// and still share the one variant of the short sets (see plainVariants).
	sharedSetMax = 10
	conditionDef = "condition"
	conditionRef = "#/$defs/" + conditionDef
)

// JSONSchema describes a Rule for the LLM's structured output.
//
// It is generated from the same tables Validate uses, so the closed sets cannot
// drift apart. The schema is a first line of defence only: structured outputs
// cannot express numeric or array bounds or cross-condition rules, and the mock
// LLM is not bound by it at all. Parse remains the authority.
//
// The plain condition variants are defined once, under $defs, and referred to
// from the rule and from a group: listing them twice is the other thing the
// API's grammar limit does not survive. The reference is not recursive: a
// group's members are the plain variants, so one level of nesting is all the
// schema offers (DECISIONS 38). Every object sets additionalProperties:false,
// as structured outputs require.
//
// The result carries its own $defs; a schema that embeds it must move them to
// its root (OutcomeSchema does).
func JSONSchema() map[string]any {
	plain := map[string]any{"$ref": conditionRef}
	group := objectSchema(
		field{"type", constant(TypeGroup)},
		field{"match", map[string]any{
			"type": "string", "enum": []any{string(MatchAll), string(MatchAny)},
			"description": "How the group's own conditions are joined. Must differ from the rule's match.",
		}},
		field{"conditions", map[string]any{
			"type":        "array",
			"description": "2 or more plain conditions. A group cannot contain a group.",
			"items":       plain,
		}},
	)

	rule := objectSchema(
		// First, so the model settles it before it starts writing conditions.
		field{"everyone", map[string]any{
			"type":        "boolean",
			"description": "true only when the brief explicitly asks for all users and gives no audience requirement; conditions is then empty. Otherwise false.",
		}},
		field{"match", map[string]any{
			"type": "string", "enum": []any{string(MatchAll), string(MatchAny)},
			"description": "all: every condition must hold. any: at least one must hold.",
		}},
		field{"conditions", map[string]any{
			"type":        "array",
			"description": "1 to 10 plain conditions in total, counting those inside groups; empty only with everyone true. Each attribute field at most once per list.",
			"items":       map[string]any{"anyOf": []any{plain, group}},
		}},
	)
	rule["$defs"] = map[string]any{conditionDef: map[string]any{"anyOf": plainVariants()}}
	return rule
}

// fromModel rewrites a rule in the model's format into the stored format: the
// stand-ins for "none" become null. Anything it does not recognise is left as
// it is, so Parse reports it.
func fromModel(raw json.RawMessage) json.RawMessage {
	var rule map[string]json.RawMessage
	if json.Unmarshal(raw, &rule) != nil || rule == nil {
		return raw
	}
	conditions, ok := fromModelList(rule["conditions"], true)
	if !ok {
		return raw
	}
	rule["conditions"] = conditions
	out, err := json.Marshal(rule)
	if err != nil {
		return raw
	}
	return out
}

func fromModelList(raw json.RawMessage, groups bool) (json.RawMessage, bool) {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil || list == nil {
		return nil, false
	}
	for i, item := range list {
		var c map[string]json.RawMessage
		if json.Unmarshal(item, &c) != nil || c == nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(c["type"], &typ)
		switch {
		case typ == TypeEvent:
			nullIf(c, "category", `"`+modelAnyCategory+`"`)
			nullIf(c, "min_count", "0")
			nullIf(c, "max_count", "0")
		case typ == TypeMetric:
			nullIf(c, "category", `"`+modelAnyCategory+`"`)
			nullIf(c, "within_days", "0")
		case typ == TypeGroup && groups:
			members, ok := fromModelList(c["conditions"], false)
			if !ok {
				continue
			}
			c["conditions"] = members
		default:
			continue
		}
		if out, err := json.Marshal(c); err == nil {
			list[i] = out
		}
	}
	out, err := json.Marshal(list)
	return out, err == nil
}

func nullIf(c map[string]json.RawMessage, key, standIn string) {
	if string(bytes.TrimSpace(c[key])) == standIn {
		c[key] = json.RawMessage("null")
	}
}

// plainVariants are the schemas of the conditions that are not groups.
//
// The closed-set attributes with a short value list share one variant: its
// field is one of them and its values come from their lists put together.
// Each variant is a branch of the compiled grammar, and one branch per
// attribute is what pushed this schema over the API's limit again when the
// loyalty tier was added (DECISIONS 45). The shared variant no longer stops
// the model from pairing a field with another field's value ("gender: gold");
// Parse does, as it does for everything else the schema cannot say. A field
// with a long list (the provinces) keeps a variant of its own, so the list a
// model is most likely to get wrong is still tied to its field.
func plainVariants() []any {
	variants := []any{}
	var shared, sharedValues []any
	var guide []string
	for _, name := range setFieldOrder {
		values := setFields[name].values()
		if len(values) <= sharedSetMax {
			shared = append(shared, name)
			for _, v := range values {
				sharedValues = append(sharedValues, v)
			}
			guide = append(guide, name+": "+strings.Join(values, ", "))
			continue
		}
		variants = append(variants, objectSchema(
			field{"type", constant(TypeAttribute)},
			field{"field", constant(name)},
			field{"op", enum(OpIn, OpNotIn)},
			field{"values", map[string]any{
				"type":  "array",
				"items": enum(values...),
			}},
		))
	}
	variants = append(variants, objectSchema(
		field{"type", constant(TypeAttribute)},
		field{"field", map[string]any{"type": "string", "enum": shared}},
		field{"op", enum(OpIn, OpNotIn)},
		field{"values", map[string]any{
			"type":        "array",
			"description": "Only values of the chosen field. " + strings.Join(guide, "; ") + ".",
			"items":       map[string]any{"type": "string", "enum": sharedValues},
		}},
	))
	variants = append(variants,
		objectSchema(
			field{"type", constant(TypeAttribute)},
			field{"field", constant(FieldAge)},
			field{"op", constant(OpBetween)},
			field{"min", map[string]any{"type": "integer", "description": "Youngest age, inclusive (0-120)."}},
			field{"max", map[string]any{"type": "integer", "description": "Oldest age, inclusive (0-120)."}},
		),
		objectSchema(
			field{"type", constant(TypeEvent)},
			field{"event", enum(eventTypes...)},
			field{"category", anyCategory("Product category, or \"any\" for no category limit.")},
			field{"within_days", map[string]any{"type": "integer", "description": "Look-back window in days (1-365)."}},
			field{"exists", map[string]any{
				"type":        "boolean",
				"description": "true: the user did this in the window. false: the user did not do this at all in the window.",
			}},
			field{"min_count", map[string]any{
				"type":        "integer",
				"description": "With exists true: the event happened at least this many times in the window (2-100). 0 for at least once. Must be 0 when exists is false.",
			}},
			field{"max_count", map[string]any{
				"type":        "integer",
				"description": "With exists true: the event happened at most this many times in the window (1-100), and still at least once (or min_count times). 0 for no upper bound. Must be 0 when exists is false. \"Bought exactly once\" is exists true with max_count 1.",
			}},
		),
		objectSchema(
			field{"type", constant(TypeSignup)},
			field{"op", map[string]any{
				"type": "string", "enum": []any{OpWithin, OpOlderThan},
				"description": "within: signed up during the last `days` days. older_than: signed up more than `days` days ago.",
			}},
			field{"days", map[string]any{"type": "integer", "description": "Number of days (1-3650)."}},
		),
		objectSchema(
			field{"type", constant(TypeMetric)},
			field{"metric", map[string]any{
				"type": "string", "enum": []any{MetricTotalSpent, MetricOrderCount, MetricAvgOrderValue},
				"description": "total_spent: sum of purchase amounts in TL. order_count: number of purchases. avg_order_value: mean purchase amount in TL; a user without purchases matches neither bound.",
			}},
			field{"op", map[string]any{
				"type": "string", "enum": []any{OpGTE, OpLTE},
				"description": "gte: at least value. lte: at most value. Both include the value itself.",
			}},
			field{"value", map[string]any{"type": "integer", "description": "Threshold: whole TL for total_spent and avg_order_value, a count for order_count."}},
			field{"within_days", map[string]any{
				"type":        "integer",
				"description": "Only purchases in the last N days (1-365), or 0 for all time.",
			}},
			field{"category", anyCategory("Only purchases in this product category, or \"any\" for all categories.")},
		),
	)

	return variants
}

// field is one property of an object schema; fields keeps them in order.
//
// The order is part of the schema: constrained output writes an object's keys
// in the order the schema lists them, and a Go map would list them
// alphabetically. An alternative whose discriminator is not its first key
// cannot be chosen by a model that starts with the discriminator: with "kind"
// sorted behind "assumptions", the real model could only ever answer
// "unsupported" (DECISIONS 41).
type field struct {
	Name   string
	Schema any
}

type fields []field

// MarshalJSON writes the properties as a JSON object, in order.
func (f fields) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range f {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(p.Name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(p.Schema)
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// get returns the schema of the named property, or nil.
func (f fields) get(name string) any {
	for _, p := range f {
		if p.Name == name {
			return p.Schema
		}
	}
	return nil
}

// objectSchema is an object with exactly these properties, all required, in
// this order.
func objectSchema(properties ...field) map[string]any {
	required := make([]string, len(properties))
	for i, p := range properties {
		required[i] = p.Name
	}
	return map[string]any{
		"type":                 "object",
		"properties":           fields(properties),
		"required":             required,
		"additionalProperties": false,
	}
}

// anyCategory is a category field in the model's format: the closed set plus
// the stand-in for "no limit".
func anyCategory(description string) map[string]any {
	field := enum(append(categories(), modelAnyCategory)...)
	field["description"] = description
	return field
}

func constant(v string) map[string]any { return map[string]any{"type": "string", "const": v} }

func enum(values ...string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "enum": vs}
}
