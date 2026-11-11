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
			"min_count": map[string]any{
				"description": "With exists true: the event happened at least this many times in the window (2-100). null for at least once. Must be null when exists is false.",
				"anyOf":       []any{map[string]any{"type": "integer"}, map[string]any{"type": "null"}},
			},
		}, "type", "event", "category", "within_days", "exists", "min_count"),
		objectSchema(map[string]any{
			"type": constant(TypeSignup),
			"op": map[string]any{
				"type": "string", "enum": []any{OpWithin, OpOlderThan},
				"description": "within: signed up during the last `days` days. older_than: signed up more than `days` days ago.",
			},
			"days": map[string]any{"type": "integer", "description": "Number of days (1-3650)."},
		}, "type", "op", "days"),
		objectSchema(map[string]any{
			"type": constant(TypeMetric),
			"metric": map[string]any{
				"type": "string", "enum": []any{MetricTotalSpent, MetricOrderCount},
				"description": "total_spent: sum of purchase amounts in TL. order_count: number of purchases.",
			},
			"op": map[string]any{
				"type": "string", "enum": []any{OpGTE, OpLTE},
				"description": "gte: at least value. lte: at most value. Both include the value itself.",
			},
			"value": map[string]any{"type": "integer", "description": "Threshold: whole TL for total_spent, a count for order_count."},
			"within_days": map[string]any{
				"description": "Only purchases in the last N days (1-365), or null for all time.",
				"anyOf":       []any{map[string]any{"type": "integer"}, map[string]any{"type": "null"}},
			},
		}, "type", "metric", "op", "value", "within_days"),
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
