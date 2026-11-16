package segment

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// "In İstanbul, and either added to the cart or spent at least 1000 TL":
// the brief the flat format could not express.
const groupJSON = `{
  "match": "all",
  "conditions": [
    {"type": "attribute", "field": "city", "op": "in", "values": ["İstanbul"]},
    {"type": "group", "match": "any", "conditions": [
      {"type": "event", "event": "cart_add", "within_days": 30, "exists": true},
      {"type": "metric", "metric": "total_spent", "op": "gte", "value": 1000}
    ]}
  ]
}`

func TestParseGroup(t *testing.T) {
	rule, err := Parse([]byte(groupJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := Rule{Match: MatchAll, Conditions: []Condition{
		{Type: TypeAttribute, Field: "city", Op: OpIn, Values: []string{"İstanbul"}},
		{Type: TypeGroup, Match: MatchAny, Conditions: []Condition{
			{Type: TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true},
			{Type: TypeMetric, Metric: MetricTotalSpent, Op: OpGTE, Value: 1000},
		}},
	}}
	if !reflect.DeepEqual(rule, want) {
		t.Fatalf("got %+v\nwant %+v", rule, want)
	}
	if n := len(rule.Leaves()); n != 3 {
		t.Errorf("%d plain conditions, want 3", n)
	}

	// What the backend sends to the browser comes back as the same rule.
	raw, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(raw)
	if err != nil || !reflect.DeepEqual(again, rule) {
		t.Fatalf("round trip: %+v, err %v\njson %s", again, err, raw)
	}
}

// The group becomes one parenthesised term, and the bind parameters keep
// counting through it: nothing from the rule is in the SQL text.
func TestCompileGroup(t *testing.T) {
	rule, err := Parse([]byte(groupJSON))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(rule, asOf)
	if err != nil {
		t.Fatal(err)
	}
	want := "(u.city = ANY($1)" +
		" AND (EXISTS (SELECT 1 FROM events e WHERE e.user_id = u.id AND e.event_type = $2 AND e.occurred_at >= $3 AND e.occurred_at <= $4)" +
		" OR (SELECT COALESCE(sum(e.amount), 0) FROM events e WHERE e.user_id = u.id AND e.event_type = $5 AND e.occurred_at <= $6) >= $7::bigint))"
	if q.Where != want {
		t.Fatalf("SQL:\n got %s\nwant %s", q.Where, want)
	}
	wantArgs := []any{[]string{"İstanbul"}, "cart_add", asOf.AddDate(0, 0, -30), asOf, "purchase", asOf, 1000}
	if !reflect.DeepEqual(q.Args, wantArgs) {
		t.Fatalf("args:\n got %v\nwant %v", q.Args, wantArgs)
	}

	// The other way round: "(A and B) or C".
	or := Rule{Match: MatchAny, Conditions: []Condition{
		{Type: TypeGroup, Match: MatchAll, Conditions: []Condition{
			{Type: TypeAttribute, Field: "city", Op: OpIn, Values: []string{"Ankara"}},
			{Type: TypeAttribute, Field: "gender", Op: OpIn, Values: []string{"female"}},
		}},
		{Type: TypeSignup, Op: OpWithin, Days: 30},
	}}
	q, err = Compile(or, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if want := "((u.city = ANY($1) AND u.gender = ANY($2)) OR (u.signup_at >= $3 AND u.signup_at <= $4))"; q.Where != want {
		t.Fatalf("SQL:\n got %s\nwant %s", q.Where, want)
	}
}

func TestGroupRejected(t *testing.T) {
	city := `{"type":"attribute","field":"city","op":"in","values":["İstanbul"]}`
	cart := `{"type":"event","event":"cart_add","within_days":30,"exists":true}`
	bought := `{"type":"event","event":"purchase","within_days":30,"exists":true}`
	notBought := `{"type":"event","event":"purchase","within_days":30,"exists":false}`
	group := func(match string, members ...string) string {
		return `{"type":"group","match":"` + match + `","conditions":[` + strings.Join(members, ",") + `]}`
	}
	rule := func(match string, conditions ...string) string {
		return `{"match":"` + match + `","conditions":[` + strings.Join(conditions, ",") + `]}`
	}
	many := make([]string, 9)
	for i := range many {
		many[i] = `{"type":"signup","op":"within","days":` + string(rune('1'+i)) + `0}`
	}

	tests := []struct {
		name, json, path, message string
	}{
		{"a group inside a group", rule("all", city, group("any", cart, group("all", bought, city))),
			"conditions[1].conditions[1].type", "grup içinde grup olamaz"},
		{"a group that repeats the rule's own match", rule("all", city, group("all", cart, bought)),
			"conditions[1].match", "aynı bağlacı (all)"},
		{"a group of one", rule("all", city, group("any", cart)),
			"conditions[1].conditions", "en az 2 koşul"},
		{"an empty group", rule("all", city, group("any")),
			"conditions[1].conditions", "en az 2 koşul"},
		{"a member outside the closed set", rule("all", group("any", `{"type":"attribute","field":"city","op":"in","values":["Atlantis"]}`, cart)),
			"conditions[0].conditions[0].values[0]", "geçerli bir şehir değil"},
		{"a member with a field that is not a column", rule("all", group("any", `{"type":"attribute","field":"city; DROP TABLE users","op":"in","values":["İstanbul"]}`, cart)),
			"conditions[0].conditions[0].field", "bilinmeyen bir alan"},
		{"an unknown key on the group", `{"match":"all","conditions":[{"type":"group","match":"any","conditions":[` + cart + `,` + bought + `],"sql":"1=1"}]}`,
			"conditions[0].sql", "bilinmeyen alan"},
		{"a group without a match", `{"match":"all","conditions":[{"type":"group","conditions":[` + cart + `,` + bought + `]}]}`,
			"conditions[0].match", "zorunlu alan eksik"},
		{"a group whose match is neither", rule("all", group("xor", cart, bought)),
			"conditions[0].match", "geçersiz"},
		{"members that exclude each other", rule("any", group("all", bought, notBought), city),
			"conditions[0].conditions[1]", "çelişiyor"},
		{"the same attribute twice in one group", rule("all", group("any", city, `{"type":"attribute","field":"city","op":"in","values":["Ankara"]}`)),
			"conditions[0].conditions[1].field", "zaten kullanılmış"},
		{"the same group twice", rule("all", group("any", cart, bought), group("any", cart, bought)),
			"conditions[1]", "aynı grup"},
		{"more plain conditions than the limit, counting groups", rule("all", city, cart, group("any", many...)),
			"conditions", "en fazla 10 koşul"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			errs, ok := err.(ValidationError)
			if !ok {
				t.Fatalf("accepted; err = %v", err)
			}
			for _, e := range errs {
				if e.Path == tt.path && strings.Contains(e.Message, tt.message) {
					return
				}
			}
			t.Errorf("no error at %q containing %q in:\n%v", tt.path, tt.message, errs)
		})
	}
}

// Things a group makes possible that are not contradictions: the same
// quantity in the rule and in a group, and a negative next to a positive when
// only one of them has to hold.
func TestGroupAccepted(t *testing.T) {
	for name, raw := range map[string]string{
		"a city in the rule and cities in a group": `{"match":"any","conditions":[
			{"type":"attribute","field":"city","op":"in","values":["İstanbul"]},
			{"type":"group","match":"all","conditions":[
				{"type":"attribute","field":"city","op":"in","values":["Ankara"]},
				{"type":"attribute","field":"gender","op":"in","values":["female"]}]}]}`,
		"bought or did not buy, inside an or": `{"match":"all","conditions":[
			{"type":"attribute","field":"city","op":"in","values":["İstanbul"]},
			{"type":"group","match":"any","conditions":[
				{"type":"event","event":"purchase","within_days":30,"exists":true},
				{"type":"event","event":"cart_add","within_days":30,"exists":false}]}]}`,
		"two groups": `{"match":"all","conditions":[
			{"type":"group","match":"any","conditions":[
				{"type":"attribute","field":"city","op":"in","values":["İstanbul"]},
				{"type":"attribute","field":"language","op":"in","values":["en"]}]},
			{"type":"group","match":"any","conditions":[
				{"type":"signup","op":"within","days":30},
				{"type":"metric","metric":"order_count","op":"gte","value":3}]}]}`,
		"a rule that is only a group": `{"match":"all","conditions":[
			{"type":"group","match":"any","conditions":[
				{"type":"signup","op":"within","days":30},
				{"type":"metric","metric":"order_count","op":"gte","value":3}]}]}`,
	} {
		if _, err := Parse([]byte(raw)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A rule built in Go gets the same checks as one from JSON, so no group
// reaches SQL through another door.
func TestCompileRejectsInvalidGroup(t *testing.T) {
	nested := Rule{Match: MatchAll, Conditions: []Condition{
		{Type: TypeGroup, Match: MatchAny, Conditions: []Condition{
			{Type: TypeSignup, Op: OpWithin, Days: 30},
			{Type: TypeGroup, Match: MatchAll, Conditions: []Condition{
				{Type: TypeSignup, Op: OpWithin, Days: 60},
				{Type: TypeMetric, Metric: MetricOrderCount, Op: OpGTE, Value: 3},
			}},
		}},
	}}
	if _, err := Compile(nested, asOf); err == nil {
		t.Error("a group inside a group was compiled")
	}
}

func TestDescribeGroup(t *testing.T) {
	rule, err := Parse([]byte(groupJSON))
	if err != nil {
		t.Fatal(err)
	}
	d := Describe(rule)
	wantLine := "Şunlardan en az biri: Son 30 günde sepete ürün ekledi; Toplam harcama: en az 1000 TL (tüm zamanlar)"
	if len(d.Conditions) != 2 || d.Conditions[0] != "Şehir: İstanbul" || d.Conditions[1] != wantLine {
		t.Fatalf("conditions = %q", d.Conditions)
	}
	if len(d.Groups) != 2 || d.Groups[0] != nil || d.Groups[1] == nil {
		t.Fatalf("groups = %+v", d.Groups)
	}
	if g := d.Groups[1]; g.Label != "Şunlardan en az biri" || len(g.Conditions) != 2 || g.Conditions[1] != "Toplam harcama: en az 1000 TL (tüm zamanlar)" {
		t.Errorf("group = %+v", g)
	}

	all := Describe(Rule{Match: MatchAny, Conditions: []Condition{
		{Type: TypeGroup, Match: MatchAll, Conditions: []Condition{
			{Type: TypeAttribute, Field: "city", Op: OpIn, Values: []string{"Ankara"}},
			{Type: TypeAttribute, Field: "gender", Op: OpIn, Values: []string{"female"}},
		}},
		{Type: TypeSignup, Op: OpWithin, Days: 30},
	}})
	if all.Conditions[0] != "Şunların hepsi: Şehir: Ankara; Cinsiyet: Kadın" || all.Summary != "Aşağıdaki koşullardan en az birini sağlayan kullanıcılar" {
		t.Errorf("description = %+v", all)
	}
}

// The schema offers a group at the top level only: a group's own members
// are the plain variants, so the model cannot be asked for deeper nesting.
func TestSchemaGroupIsOneLevel(t *testing.T) {
	schema := JSONSchema()
	items := schema["properties"].(fields).get("conditions").(map[string]any)["items"].(map[string]any)["anyOf"].([]any)
	if len(items) != 2 {
		t.Fatalf("the rule offers %d alternatives, want the plain conditions and a group", len(items))
	}
	if ref := items[0].(map[string]any)["$ref"]; ref != conditionRef {
		t.Errorf("plain conditions = %v", items[0])
	}
	group := items[1].(map[string]any)
	if group["properties"].(fields).get("type").(map[string]any)["const"] != TypeGroup {
		t.Fatalf("second alternative = %v", group)
	}
	// A group's members are the plain variants, by the same reference.
	if ref := group["properties"].(fields).get("conditions").(map[string]any)["items"].(map[string]any)["$ref"]; ref != conditionRef {
		t.Errorf("group members = %v", group)
	}
	// The definition itself holds no group and no reference: not recursive.
	plain := schema["$defs"].(map[string]any)[conditionDef].(map[string]any)["anyOf"].([]any)
	for _, v := range plain {
		if v.(map[string]any)["properties"].(fields).get("type").(map[string]any)["const"] == TypeGroup {
			t.Error("a group may contain a group")
		}
	}
	if raw, _ := json.Marshal(plain); strings.Contains(string(raw), "$ref") {
		t.Error("the schema is recursive")
	}
}
