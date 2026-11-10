package segment

// JSONSchema describes a Rule for the LLM's structured output.
//
// It is generated from the same tables Validate uses, so the closed sets cannot
// drift apart. The schema is a first line of defence only: structured outputs
// cannot express numeric or array bounds or cross-condition rules, and the mock
// LLM is not bound by it at all. Parse remains the authority.
//
// The schema is deliberately non-recursive and every object sets
// additionalProperties:false, as structured outputs require.
func JSONSchema() map[string]any {
	variants := []any{}
	for _, name := range setFieldOrder {
		variants = append(variants, objectSchema(map[string]any{
			"type":  constant(TypeAttribute),
			"field": constant(name),
			"op":    enum(OpIn, OpNotIn),
			"values": map[string]any{
				"type":  "array",
				"items": enum(setFields[name].values()...),
			},
		}, "type", "field", "op", "values"))
	}
	variants = append(variants,
		objectSchema(map[string]any{
			"type":  constant(TypeAttribute),
			"field": constant(FieldAge),
			"op":    constant(OpBetween),
			"min":   map[string]any{"type": "integer", "description": "Youngest age, inclusive (0-120)."},
			"max":   map[string]any{"type": "integer", "description": "Oldest age, inclusive (0-120)."},
		}, "type", "field", "op", "min", "max"),
		objectSchema(map[string]any{
			"type":  constant(TypeEvent),
			"event": enum(eventTypes...),
			"category": map[string]any{
				"description": "Product category, or null for any category.",
				"anyOf":       []any{enum(categories()...), map[string]any{"type": "null"}},
			},
			"within_days": map[string]any{"type": "integer", "description": "Look-back window in days (1-365)."},
			"exists": map[string]any{
				"type":        "boolean",
				"description": "true: the user did this in the window. false: the user did not do this at all in the window.",
			},
		}, "type", "event", "category", "within_days", "exists"),
	)

	return objectSchema(map[string]any{
		"match": map[string]any{
			"type": "string", "enum": []any{string(MatchAll), string(MatchAny)},
			"description": "all: every condition must hold. any: at least one must hold.",
		},
		"conditions": map[string]any{
			"type":        "array",
			"description": "1 to 10 conditions. Each attribute field at most once.",
			"items":       map[string]any{"anyOf": variants},
		},
	}, "match", "conditions")
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func constant(v string) map[string]any { return map[string]any{"type": "string", "const": v} }

func enum(values ...string) map[string]any {
	vs := make([]any, len(values))
	for i, v := range values {
		vs[i] = v
	}
	return map[string]any{"type": "string", "enum": vs}
}
