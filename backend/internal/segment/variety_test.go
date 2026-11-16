package segment

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Counts with an upper bound, the average order value and metrics limited to
// one category (DECISIONS 40).
const varietyJSON = `{"match":"all","conditions":[
  {"type":"event","event":"purchase","within_days":90,"exists":true,"max_count":1},
  {"type":"event","event":"cart_add","category":"fashion","within_days":30,"exists":true,"min_count":2,"max_count":5},
  {"type":"metric","metric":"avg_order_value","op":"gte","value":750},
  {"type":"metric","metric":"total_spent","op":"gte","value":2000,"within_days":180,"category":"electronics"},
  {"type":"metric","metric":"order_count","op":"lte","value":3,"category":"books"}]}`

func TestVarietyRoundTrip(t *testing.T) {
	rule, err := Parse([]byte(varietyJSON))
	if err != nil {
		t.Fatal(err)
	}
	if c := rule.Conditions[0]; c.MaxCount != 1 || c.MinCount != 0 {
		t.Errorf("first = %+v", c)
	}
	if c := rule.Conditions[3]; c.Category != "electronics" || c.WithinDays != 180 {
		t.Errorf("fourth = %+v", c)
	}
	out, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(rule, again) {
		t.Fatalf("round trip changed the rule:\n%+v\n%+v", rule, again)
	}
	// Nulls mean "not set", so the form and the model can always send the keys.
	if _, err := Parse([]byte(`{"match":"all","conditions":[
	  {"type":"event","event":"purchase","category":null,"within_days":30,"exists":false,"min_count":null,"max_count":null},
	  {"type":"metric","metric":"avg_order_value","op":"lte","value":200,"within_days":null,"category":null}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestVarietyCompile(t *testing.T) {
	rule, err := Parse([]byte(varietyJSON))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(rule, asOf)
	if err != nil {
		t.Fatal(err)
	}
	want := "((SELECT count(*) FROM events e WHERE e.user_id = u.id AND e.event_type = $1 AND e.occurred_at >= $2 AND e.occurred_at <= $3) BETWEEN $4::bigint AND $5::bigint" +
		" AND (SELECT count(*) FROM events e WHERE e.user_id = u.id AND e.event_type = $6 AND e.occurred_at >= $7 AND e.occurred_at <= $8 AND e.category = $9) BETWEEN $10::bigint AND $11::bigint" +
		" AND COALESCE((SELECT avg(e.amount) FROM events e WHERE e.user_id = u.id AND e.event_type = $12 AND e.occurred_at <= $13) >= $14::bigint, false)" +
		" AND (SELECT COALESCE(sum(e.amount), 0) FROM events e WHERE e.user_id = u.id AND e.event_type = $15 AND e.occurred_at <= $16 AND e.occurred_at >= $17 AND e.category = $18) >= $19::bigint" +
		" AND (SELECT count(*) FROM events e WHERE e.user_id = u.id AND e.event_type = $20 AND e.occurred_at <= $21 AND e.category = $22) <= $23::bigint)"
	if q.Where != want {
		t.Fatalf("SQL:\n got %s\nwant %s", q.Where, want)
	}
	wantArgs := []any{
		"purchase", asOf.AddDate(0, 0, -90), asOf, 1, 1,
		"cart_add", asOf.AddDate(0, 0, -30), asOf, "fashion", 2, 5,
		"purchase", asOf, 750,
		"purchase", asOf, asOf.AddDate(0, 0, -180), "electronics", 2000,
		"purchase", asOf, "books", 3,
	}
	if !reflect.DeepEqual(q.Args, wantArgs) {
		t.Fatalf("args:\n got %v\nwant %v", q.Args, wantArgs)
	}
	for _, leaked := range []string{"750", "2000", "electronics", "fashion", "books", "avg_order_value", "'"} {
		if strings.Contains(q.Where, leaked) {
			t.Errorf("SQL contains %q", leaked)
		}
	}
}

func TestVarietyDescribe(t *testing.T) {
	rule, err := Parse([]byte(varietyJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Son 90 günde yalnızca 1 kez satın alma yaptı",
		"Son 30 günde moda kategorisinde 2–5 kez sepete ürün ekledi",
		"Ortalama sipariş tutarı: en az 750 TL (tüm zamanlar)",
		"Toplam harcama (elektronik kategorisinde): en az 2000 TL (son 180 günde)",
		"Sipariş sayısı (kitap kategorisinde): en fazla 3 (tüm zamanlar)",
	}
	if got := Describe(rule).Conditions; !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	atMost := Describe(Rule{Match: MatchAll, Conditions: []Condition{{Type: TypeEvent, Event: "purchase", WithinDays: 60, Exists: true, MaxCount: 3}}})
	if atMost.Conditions[0] != "Son 60 günde en fazla 3 kez satın alma yaptı" {
		t.Errorf("got %q", atMost.Conditions[0])
	}
}

// The same metric in two categories is two different quantities, and the
// schema offers the model what the parser accepts.
func TestVarietyAcceptedAndInSchema(t *testing.T) {
	if _, err := Parse([]byte(`{"match":"all","conditions":[
	  {"type":"metric","metric":"total_spent","op":"gte","value":500,"category":"books"},
	  {"type":"metric","metric":"total_spent","op":"gte","value":900,"category":"fashion"},
	  {"type":"metric","metric":"total_spent","op":"gte","value":3000}]}`)); err != nil {
		t.Fatal(err)
	}
	schema, err := json.Marshal(JSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"max_count"`, `"avg_order_value"`} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("schema lacks %s", want)
		}
	}
}

// The model's format has no nulls (DECISIONS 41): its stand-ins for "none"
// are turned back into null before the rule is parsed, inside groups too.
func TestModelStandInsBecomeNull(t *testing.T) {
	reply := `{"result":{"kind":"rule","assumptions":[],"schedule":null,"rule":{"match":"any","conditions":[
	  {"type":"event","event":"purchase","category":"any","within_days":90,"exists":true,"min_count":0,"max_count":1},
	  {"type":"group","match":"all","conditions":[
	    {"type":"metric","metric":"total_spent","op":"gte","value":1000,"within_days":0,"category":"any"},
	    {"type":"event","event":"cart_add","category":"books","within_days":30,"exists":false,"min_count":0,"max_count":0}]}]}}}`
	out, errs, _ := parseOutcome([]byte(reply), Clock{})
	if len(errs) > 0 || out.Rule == nil {
		t.Fatalf("rejected: %v", errs)
	}
	first := out.Rule.Conditions[0]
	if first.Category != "" || first.MinCount != 0 || first.MaxCount != 1 {
		t.Errorf("event = %+v", first)
	}
	members := out.Rule.Conditions[1].Conditions
	if m := members[0]; m.WithinDays != 0 || m.Category != "" || m.Value != 1000 {
		t.Errorf("metric = %+v", m)
	}
	if m := members[1]; m.Category != "books" || m.Exists || m.MinCount != 0 || m.MaxCount != 0 {
		t.Errorf("event in group = %+v", m)
	}

	// The stand-ins are the model's only: a stored rule still says null, and
	// an event's own window has no "none".
	if _, err := Parse([]byte(`{"match":"all","conditions":[{"type":"event","event":"purchase","category":"any","within_days":30,"exists":true,"min_count":null,"max_count":null}]}`)); err == nil {
		t.Error(`Parse accepted category "any"`)
	}
	_, errs, _ = parseOutcome([]byte(`{"result":{"kind":"rule","assumptions":[],"schedule":null,"rule":{"match":"all","conditions":[
	  {"type":"event","event":"purchase","category":"any","within_days":0,"exists":true,"min_count":0,"max_count":0}]}}}`), Clock{})
	if len(errs) == 0 {
		t.Error("an event window of 0 days was accepted")
	}
}

// What the real API's grammar limit does not survive: nullable fields and a
// second copy of the condition variants (DECISIONS 41).
func TestModelSchemaStaysSmall(t *testing.T) {
	for _, c := range categories() {
		if c == modelAnyCategory {
			t.Fatalf("%q is a real category; pick another stand-in", c)
		}
	}
	outcome := OutcomeSchema()
	defs, _ := json.Marshal(outcome["$defs"])
	if strings.Contains(string(defs), `"null"`) {
		t.Error("a condition field is nullable again")
	}
	whole, _ := json.Marshal(outcome)
	if n := strings.Count(string(whole), `"İstanbul"`); n != 1 {
		t.Errorf("the city list appears %d times, want once", n)
	}
	if _, nested := outcome["properties"].(fields).get("result").(map[string]any)["anyOf"].([]any)[0].(map[string]any)["properties"].(fields).get("rule").(map[string]any)["$defs"]; nested {
		t.Error("$defs must sit at the root of the schema that is sent")
	}
}

// Constrained output writes keys in schema order, so every alternative of a
// union must start with what tells it apart (DECISIONS 41).
func TestSchemaDiscriminatorsComeFirst(t *testing.T) {
	raw, err := json.Marshal(OutcomeSchema())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if n := strings.Count(text, `"properties":{"kind":`); n != 2 {
		t.Errorf("%d result alternatives start with kind, want 2", n)
	}
	objects := strings.Count(text, `"properties":{`)
	typed := strings.Count(text, `"properties":{"type":`)
	// Every condition variant and the group start with "type"; the others are
	// the outcome, its two alternatives, the rule, an assumption and a schedule.
	if objects-typed != 6 {
		t.Errorf("%d of %d objects start with type; a condition variant does not", typed, objects)
	}
}

// "Did it or did not do it" is everyone, not an audience (DECISIONS 42).
func TestEveryoneIsNotAnAudience(t *testing.T) {
	event := func(exists bool, days int, extra string) string {
		return fmt.Sprintf(`{"type":"event","event":"purchase","category":null,"within_days":%d,"exists":%t,"min_count":null,"max_count":null%s}`, days, exists, extra)
	}
	rule := func(match string, conditions ...string) []byte {
		return []byte(`{"match":"` + match + `","conditions":[` + strings.Join(conditions, ",") + `]}`)
	}
	city := `{"type":"attribute","field":"city","op":"in","values":["İstanbul"]}`
	electronics := `{"type":"event","event":"purchase","category":"electronics","within_days":30,"exists":true,"min_count":null,"max_count":null}`
	atLeast3 := `{"type":"event","event":"purchase","category":null,"within_days":30,"exists":true,"min_count":3,"max_count":null}`

	rejected := map[string][]byte{
		"same window":           rule("any", event(true, 365, ""), event(false, 365, "")),
		"wider positive window": rule("any", event(false, 30, ""), event(true, 90, "")),
		"inside a group":        rule("all", city, `{"type":"group","match":"any","conditions":[`+event(true, 30, "")+","+event(false, 30, "")+`]}`),
	}
	for name, raw := range rejected {
		_, err := Parse(raw)
		if err == nil || !strings.Contains(err.Error(), "herkesi seçer") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	accepted := map[string][]byte{
		// Someone who bought once or twice matches neither side.
		"positive with a count": rule("any", atLeast3, event(false, 30, "")),
		// Bought 60 days ago: not in the last 30, yet not "none in the last 90".
		"narrower positive window": rule("any", event(true, 30, ""), event(false, 90, "")),
		// Bought something else: no electronics, yet a purchase.
		"narrower positive category": rule("any", electronics, event(false, 30, "")),
	}
	for name, raw := range accepted {
		if _, err := Parse(raw); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// "Everyone" is a flag of its own, never an empty list (DECISIONS 43).
func TestEveryone(t *testing.T) {
	rule, err := Parse([]byte(`{"match":"all","conditions":[],"everyone":true}`))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(rule, time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	if err != nil || q.Where != "TRUE" || len(q.Args) != 0 {
		t.Fatalf("compiled to %q with %d args, err %v", q.Where, len(q.Args), err)
	}
	if d := Describe(rule); !strings.Contains(d.Summary, "Tüm kullanıcılar") || d.Conditions == nil || len(d.Conditions) != 0 {
		t.Errorf("description = %+v", d)
	}
	// Built in Go, the list is nil; it must still be written as a list.
	if out, _ := json.Marshal(Rule{Match: MatchAll, Everyone: true}); string(out) != `{"match":"all","conditions":[],"everyone":true}` {
		t.Errorf("marshalled as %s", out)
	}

	city := `{"type":"attribute","field":"city","op":"in","values":["İstanbul"]}`
	rejected := map[string]struct{ raw, path string }{
		"an empty list is not everyone": {`{"match":"all","conditions":[]}`, "conditions"},
		"nor is the flag set to false":  {`{"match":"all","conditions":[],"everyone":false}`, "conditions"},
		"everyone with a condition":     {`{"match":"all","conditions":[` + city + `],"everyone":true}`, "everyone"},
	}
	for name, c := range rejected {
		_, err := Parse([]byte(c.raw))
		errs, _ := err.(ValidationError)
		if len(errs) != 1 || errs[0].Path != c.path {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	// Every other rule keeps the shape it had before the flag existed.
	plain, err := Parse([]byte(`{"match":"all","conditions":[` + city + `],"everyone":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := json.Marshal(plain); strings.Contains(string(out), "everyone") {
		t.Errorf("flag written for a plain rule: %s", out)
	}
}

// The model has to decide "everyone" before it writes a condition, and may
// only say it with the flag.
func TestEveryoneFromTheModel(t *testing.T) {
	rule := JSONSchema()
	if first := rule["properties"].(fields)[0].Name; first != "everyone" {
		t.Errorf("first rule property = %q", first)
	}
	reply := `{"result":{"kind":"rule","rule":{"everyone":true,"match":"all","conditions":[]},"assumptions":[],"schedule":null}}`
	out, errs, _ := parseOutcome([]byte(reply), Clock{})
	if len(errs) > 0 || out.Rule == nil || !out.Rule.Everyone {
		t.Fatalf("out = %+v, errs = %v", out, errs)
	}
	// An assumption has no condition to point at.
	reply = strings.Replace(reply, `"assumptions":[]`, `"assumptions":[{"condition_index":0,"phrase":"herkese","interpretation":"Tüm kullanıcılar."}]`, 1)
	if _, errs, _ := parseOutcome([]byte(reply), Clock{}); len(errs) != 1 {
		t.Errorf("errs = %v", errs)
	}
}

// The loyalty tier is one more closed-set attribute (DECISIONS 45): it comes
// from the same tables, so parsing, SQL, description and schema all know it.
func TestLoyaltyTier(t *testing.T) {
	rule, err := Parse([]byte(`{"match":"all","conditions":[{"type":"attribute","field":"loyalty_tier","op":"in","values":["silver","gold"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(rule, time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	if err != nil || q.Where != "(u.loyalty_tier = ANY($1))" || !reflect.DeepEqual(q.Args, []any{[]string{"silver", "gold"}}) {
		t.Fatalf("compiled to %q %v, err %v", q.Where, q.Args, err)
	}
	if d := Describe(rule); d.Conditions[0] != "Sadakat seviyesi: Gümüş veya Altın" {
		t.Errorf("description = %q", d.Conditions[0])
	}
	schema, _ := json.Marshal(JSONSchema())
	if !strings.Contains(string(schema), `"loyalty_tier"`) || !strings.Contains(string(schema), `"gold"`) {
		t.Error("the model schema does not offer the loyalty tier")
	}
	for name, raw := range map[string]string{
		"a tier that does not exist": `{"type":"attribute","field":"loyalty_tier","op":"in","values":["platinum"]}`,
		"every tier":                 `{"type":"attribute","field":"loyalty_tier","op":"in","values":["bronze","silver","gold"]}`,
	} {
		if _, err := Parse([]byte(`{"match":"all","conditions":[` + raw + `]}`)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The short closed sets share one schema variant (DECISIONS 45): the API
// rejects one variant per attribute. The provinces keep their own.
func TestShortSetsShareOneVariant(t *testing.T) {
	raw, _ := json.Marshal(JSONSchema())
	text := string(raw)
	if n := strings.Count(text, `"const":"attribute"`); n != 3 {
		t.Errorf("%d attribute variants, want 3: city, the short sets, age", n)
	}
	if !strings.Contains(text, `"field":{"const":"city"`) {
		t.Error("the city list is no longer tied to the city field")
	}
	if !strings.Contains(text, `"field":{"enum":["language","gender","loyalty_tier"],"type":"string"}`) {
		t.Errorf("shared variant missing: %s", text)
	}
	// What the schema no longer says, Parse still does.
	mixed := `{"result":{"kind":"rule","rule":{"everyone":false,"match":"all","conditions":[{"type":"attribute","field":"gender","op":"in","values":["gold"]}]},"assumptions":[],"schedule":null}}`
	if _, errs, _ := parseOutcome([]byte(mixed), Clock{}); len(errs) != 1 || errs[0].Path != "result.rule.conditions[0].values[0]" {
		t.Errorf("errs = %v", errs)
	}
}
