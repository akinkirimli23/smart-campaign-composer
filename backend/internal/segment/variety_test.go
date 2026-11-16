package segment

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
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
