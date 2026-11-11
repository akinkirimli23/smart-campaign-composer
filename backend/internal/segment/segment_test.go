package segment

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/seed"
)

// exampleJSON is the rule for the case's example brief.
const exampleJSON = `{
  "match": "all",
  "conditions": [
    {"type": "attribute", "field": "city", "op": "in", "values": ["İstanbul"]},
    {"type": "event", "event": "cart_add", "within_days": 30, "exists": true},
    {"type": "event", "event": "purchase", "within_days": 30, "exists": false}
  ]
}`

var asOf = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func TestParseExample(t *testing.T) {
	rule, err := Parse([]byte(exampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := Rule{Match: MatchAll, Conditions: []Condition{
		{Type: TypeAttribute, Field: "city", Op: OpIn, Values: []string{"İstanbul"}},
		{Type: TypeEvent, Event: "cart_add", WithinDays: 30, Exists: true},
		{Type: TypeEvent, Event: "purchase", WithinDays: 30, Exists: false},
	}}
	if !reflect.DeepEqual(rule, want) {
		t.Fatalf("got %+v\nwant %+v", rule, want)
	}
}

// A rule must survive the trip to the frontend and back unchanged.
func TestRuleRoundTrip(t *testing.T) {
	in := Rule{Match: MatchAny, Conditions: []Condition{
		{Type: TypeAttribute, Field: "city", Op: OpNotIn, Values: []string{"Ankara", "İzmir"}},
		{Type: TypeAttribute, Field: FieldAge, Op: OpBetween, Min: 25, Max: 34},
		{Type: TypeEvent, Event: "purchase", Category: "cosmetics", WithinDays: 7, Exists: true},
		{Type: TypeEvent, Event: "cart_add", WithinDays: 90, Exists: false},
	}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(%s): %v", data, err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip changed the rule:\n in %+v\nout %+v\njson %s", in, out, data)
	}
}

func TestParseRejects(t *testing.T) {
	cond := func(c string) string { return `{"match":"all","conditions":[` + c + `]}` }
	event := `{"type":"event","event":"cart_add","within_days":30,"exists":true}`

	tests := []struct {
		name     string
		json     string
		wantPath string
		wantMsg  string // substring of the message at wantPath
	}{
		{"malformed json", `{"match":`, "", "geçerli JSON değil"},
		{"not an object", `[]`, "", "JSON nesnesi olmalı"},
		{"null", `null`, "", "JSON nesnesi olmalı"},
		{"unknown top-level key", `{"match":"all","conditions":[` + event + `],"sql":"DROP TABLE users"}`, "sql", "bilinmeyen alan"},
		{"missing match", `{"conditions":[` + event + `]}`, "match", "zorunlu alan eksik"},
		{"bad match", `{"match":"or","conditions":[` + event + `]}`, "match", "izinli değerler: all, any"},
		{"no conditions", `{"match":"all","conditions":[]}`, "conditions", "en az bir koşul"},
		{"conditions not array", `{"match":"all","conditions":{}}`, "conditions", "dizi olmalı"},
		{"too many conditions", cond(strings.TrimSuffix(strings.Repeat(event+",", 11), ",")), "conditions", "en fazla 10"},

		{"unknown condition type", cond(`{"type":"sql","query":"1=1"}`), "conditions[0].type", "bilinmeyen bir koşul tipi"},
		{"missing type", cond(`{"field":"city"}`), "conditions[0].type", "zorunlu alan eksik"},

		// Closed sets: a near-miss is an error with a hint, never an empty audience.
		{"city without Turkish letters", cond(`{"type":"attribute","field":"city","op":"in","values":["Istanbul"]}`),
			"conditions[0].values[0]", `"İstanbul" mi demek istediniz?`},
		{"city not in data", cond(`{"type":"attribute","field":"city","op":"in","values":["Paris"]}`),
			"conditions[0].values[0]", "geçerli bir şehir değil"},
		{"unknown field", cond(`{"type":"attribute","field":"email","op":"in","values":["a@b.c"]}`),
			"conditions[0].field", "bilinmeyen bir alan"},
		{"op not allowed for field", cond(`{"type":"attribute","field":"city","op":"like","values":["İstanbul"]}`),
			"conditions[0].op", "izinli operatörler: in, not_in"},
		{"empty values", cond(`{"type":"attribute","field":"city","op":"in","values":[]}`), "conditions[0].values", "en az bir değer"},
		{"values not array", cond(`{"type":"attribute","field":"city","op":"in","values":"İstanbul"}`), "conditions[0].values", "metin dizisi"},
		{"duplicate value", cond(`{"type":"attribute","field":"city","op":"in","values":["Ankara","Ankara"]}`), "conditions[0].values[1]", "birden fazla"},
		{"in with every value", cond(`{"type":"attribute","field":"language","op":"in","values":["tr","en"]}`), "conditions[0].values", "hiçbir şeyi filtrelemiyor"},
		{"not_in with every value", cond(`{"type":"attribute","field":"gender","op":"not_in","values":["female","male"]}`), "conditions[0].values", "kimseyi seçmez"},
		{"key of another variant", cond(`{"type":"attribute","field":"city","op":"in","values":["Ankara"],"within_days":7}`),
			"conditions[0].within_days", "bilinmeyen alan"},
		{"age with values", cond(`{"type":"attribute","field":"age","op":"between","values":["25"]}`), "conditions[0].values", "bilinmeyen alan"},
		{"age min above max", cond(`{"type":"attribute","field":"age","op":"between","min":40,"max":30}`), "conditions[0].min", "büyük olamaz"},
		{"age out of range", cond(`{"type":"attribute","field":"age","op":"between","min":18,"max":300}`), "conditions[0].max", "0 ile 120"},
		{"age not integer", cond(`{"type":"attribute","field":"age","op":"between","min":18.5,"max":30}`), "conditions[0].min", "tam sayı"},

		{"unknown event", cond(`{"type":"event","event":"page_view","within_days":30,"exists":true}`), "conditions[0].event", "geçerli bir olay değil"},
		{"unknown category", cond(`{"type":"event","event":"purchase","category":"Kozmetik","within_days":30,"exists":true}`),
			"conditions[0].category", "geçerli bir kategori değil"},
		{"window too small", cond(`{"type":"event","event":"purchase","within_days":0,"exists":true}`), "conditions[0].within_days", "1 ile 365"},
		{"window too large", cond(`{"type":"event","event":"purchase","within_days":366,"exists":true}`), "conditions[0].within_days", "1 ile 365"},
		{"window as string", cond(`{"type":"event","event":"purchase","within_days":"30","exists":true}`), "conditions[0].within_days", "tam sayı"},
		{"exists missing", cond(`{"type":"event","event":"purchase","within_days":30}`), "conditions[0].exists", "zorunlu alan eksik"},
		{"exists null", cond(`{"type":"event","event":"purchase","within_days":30,"exists":null}`), "conditions[0].exists", "true ya da false"},

		// SQL injection attempts die in the whitelist, long before any SQL exists.
		{"injection in value", cond(`{"type":"attribute","field":"city","op":"in","values":["İstanbul'; DROP TABLE users;--"]}`),
			"conditions[0].values[0]", "geçerli bir şehir değil"},
		{"injection in field", cond(`{"type":"attribute","field":"city = 'x' OR 1=1 --","op":"in","values":["İstanbul"]}`),
			"conditions[0].field", "bilinmeyen bir alan"},
		{"injection in op", cond(`{"type":"attribute","field":"city","op":"= ANY(SELECT city FROM users) --","values":["İstanbul"]}`),
			"conditions[0].op", "kullanılamaz"},
		{"injection in event", cond(`{"type":"event","event":"purchase' OR '1'='1","within_days":30,"exists":true}`),
			"conditions[0].event", "geçerli bir olay değil"},
		{"injection in category", cond(`{"type":"event","event":"purchase","category":"x') OR 1=1 --","within_days":30,"exists":true}`),
			"conditions[0].category", "geçerli bir kategori değil"},

		// Individually valid, meaningless together.
		{"same attribute twice", cond(`{"type":"attribute","field":"city","op":"in","values":["Ankara"]},{"type":"attribute","field":"city","op":"in","values":["İzmir"]}`),
			"conditions[1].field", "zaten kullanılmış"},
		{"same event condition twice", cond(event + "," + event), "conditions[1]", "aynı koşul"},
		{"did and did not, same window", cond(event + `,{"type":"event","event":"cart_add","within_days":30,"exists":false}`), "conditions[1]", "çelişiyor"},
		{"did in 7 days, did not in 30", cond(`{"type":"event","event":"purchase","within_days":7,"exists":true},{"type":"event","event":"purchase","within_days":30,"exists":false}`),
			"conditions[1]", "çelişiyor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			verr, ok := err.(ValidationError)
			if !ok {
				t.Fatalf("Parse accepted an invalid rule (err=%v)", err)
			}
			for _, fe := range verr {
				if fe.Path == tt.wantPath && strings.Contains(fe.Message, tt.wantMsg) {
					return
				}
			}
			t.Fatalf("no error at %q containing %q; got:\n%v", tt.wantPath, tt.wantMsg, verr)
		})
	}
}

// Valid combinations that look similar to the rejected ones above.
func TestParseAcceptsMeaningfulCombinations(t *testing.T) {
	tests := map[string]string{
		// Lapsed customers: bought in the last 90 days but not in the last 30.
		"wider positive, narrower negative": `{"match":"all","conditions":[
			{"type":"event","event":"purchase","within_days":90,"exists":true},
			{"type":"event","event":"purchase","within_days":30,"exists":false}]}`,
		"negative limited to another category": `{"match":"all","conditions":[
			{"type":"event","event":"purchase","category":"books","within_days":30,"exists":true},
			{"type":"event","event":"purchase","category":"sports","within_days":30,"exists":false}]}`,
		"did or did not, under any": `{"match":"any","conditions":[
			{"type":"event","event":"cart_add","within_days":30,"exists":true},
			{"type":"event","event":"cart_add","within_days":30,"exists":false}]}`,
		"null category": `{"match":"all","conditions":[
			{"type":"event","event":"cart_add","category":null,"within_days":30,"exists":true}]}`,
	}
	for name, js := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(js)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// One reply should let the model fix everything, so all errors are reported.
func TestParseReportsAllErrors(t *testing.T) {
	_, err := Parse([]byte(`{"match":"all","conditions":[
		{"type":"attribute","field":"city","op":"in","values":["Istanbul","Paris"]},
		{"type":"event","event":"view","within_days":999,"exists":true}]}`))
	verr, _ := err.(ValidationError)
	if len(verr) != 4 {
		t.Fatalf("want 4 errors, got %d:\n%v", len(verr), err)
	}

	// A structurally broken condition must not hide problems in the others, and
	// must not shift their indexes.
	_, err = Parse([]byte(`{"match":"all","conditions":[
		{"type":"event","event":"purchase","within_days":30,"exists":true,"sql":"1=1"},
		{"type":"attribute","field":"city","op":"in","values":["Istanbul"]}]}`))
	verr, _ = err.(ValidationError)
	if len(verr) != 2 || verr[0].Path != "conditions[0].sql" || verr[1].Path != "conditions[1].values[0]" {
		t.Fatalf("want errors at conditions[0].sql and conditions[1].values[0], got:\n%v", err)
	}
}

func TestCompileExample(t *testing.T) {
	rule, err := Parse([]byte(exampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(rule, asOf)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := "(u.city = ANY($1)" +
		" AND EXISTS (SELECT 1 FROM events e WHERE e.user_id = u.id AND e.event_type = $2 AND e.occurred_at >= $3 AND e.occurred_at <= $4)" +
		" AND NOT EXISTS (SELECT 1 FROM events e WHERE e.user_id = u.id AND e.event_type = $5 AND e.occurred_at >= $6 AND e.occurred_at <= $7))"
	if q.Where != wantSQL {
		t.Fatalf("SQL:\n got %s\nwant %s", q.Where, wantSQL)
	}
	from := asOf.AddDate(0, 0, -30)
	wantArgs := []any{[]string{"İstanbul"}, "cart_add", from, asOf, "purchase", from, asOf}
	if !reflect.DeepEqual(q.Args, wantArgs) {
		t.Fatalf("args:\n got %v\nwant %v", q.Args, wantArgs)
	}
}

// No value from a rule may appear in the SQL text, and the clock must be a
// parameter. Checked over a rule that uses every condition variant.
func TestCompileKeepsValuesOutOfSQL(t *testing.T) {
	rule := Rule{Match: MatchAny, Conditions: []Condition{
		{Type: TypeAttribute, Field: "city", Op: OpNotIn, Values: []string{"Ankara", "İzmir"}},
		{Type: TypeAttribute, Field: "language", Op: OpIn, Values: []string{"en"}},
		{Type: TypeAttribute, Field: "gender", Op: OpIn, Values: []string{"female"}},
		{Type: TypeAttribute, Field: FieldAge, Op: OpBetween, Min: 25, Max: 34},
		{Type: TypeEvent, Event: "purchase", Category: "cosmetics", WithinDays: 7, Exists: true},
	}}
	q, err := Compile(rule, asOf)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Ankara", "İzmir", "'", "female", "cosmetics", "purchase", "2026", "1992", "now()", "25", "34"} {
		if strings.Contains(q.Where, leaked) {
			t.Errorf("SQL contains %q: %s", leaked, q.Where)
		}
	}
	if !strings.Contains(q.Where, " OR ") || strings.Contains(q.Where, " AND u.") {
		t.Errorf("match any must join conditions with OR: %s", q.Where)
	}
	if got, want := strings.Count(q.Where, "$"), len(q.Args); got != want {
		t.Errorf("%d placeholders for %d args", got, want)
	}
	// age 25-34 as of 2026 -> born 1992..2001
	if q.Args[3] != 1992 || q.Args[4] != 2001 {
		t.Errorf("birth year bounds = %v, %v; want 1992, 2001", q.Args[3], q.Args[4])
	}
}

// Compile validates on its own: a rule built in Go that skipped Parse still
// cannot smuggle an identifier into SQL.
func TestCompileRejectsUnvalidatedRule(t *testing.T) {
	bad := []Rule{
		{Match: MatchAll, Conditions: []Condition{{Type: TypeAttribute, Field: "email; DROP TABLE users", Op: OpIn, Values: []string{"x"}}}},
		{Match: MatchAll, Conditions: []Condition{{Type: TypeAttribute, Field: "city", Op: "LIKE", Values: []string{"İstanbul"}}}},
		{Match: "all) OR (1=1", Conditions: []Condition{{Type: TypeEvent, Event: "purchase", WithinDays: 30, Exists: true}}},
		{Match: MatchAll},
	}
	for i, r := range bad {
		if q, err := Compile(r, asOf); err == nil {
			t.Errorf("rule %d compiled to %q", i, q.Where)
		}
	}
}

func TestDescribeExample(t *testing.T) {
	rule, _ := Parse([]byte(exampleJSON))
	got := Describe(rule)
	want := Description{
		Summary: "Aşağıdaki koşulların hepsini sağlayan kullanıcılar",
		Conditions: []string{
			"Şehir: İstanbul",
			"Son 30 günde sepete ürün ekledi",
			"Son 30 günde hiç satın alma yapmadı",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// Every closed-set value must have a place in the schema and a label, so adding
// a category in seed cannot silently leave the LLM or the UI behind.
func TestClosedSetsAreCovered(t *testing.T) {
	schema, err := json.Marshal(JSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range append(seed.Cities(), seed.Categories...) {
		if !strings.Contains(string(schema), `"`+v+`"`) {
			t.Errorf("schema does not list %q", v)
		}
	}
	for _, c := range seed.Categories {
		if categoryLabels[c] == "" {
			t.Errorf("category %q has no Turkish label", c)
		}
	}
	if strings.Contains(string(schema), "$ref") {
		t.Error("schema must not be recursive: structured outputs reject it")
	}
}

// --- DECISIONS 25: event counts, signup date, purchase metrics ----------------

const extendedJSON = `{"match":"all","conditions":[
  {"type":"event","event":"cart_add","category":"electronics","within_days":30,"exists":true,"min_count":3},
  {"type":"signup","op":"within","days":90},
  {"type":"metric","metric":"total_spent","op":"gte","value":1000,"within_days":null},
  {"type":"metric","metric":"order_count","op":"lte","value":5,"within_days":60}]}`

func TestExtendedConditionsRoundTrip(t *testing.T) {
	rule, err := Parse([]byte(extendedJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []Condition{
		{Type: TypeEvent, Event: "cart_add", Category: "electronics", WithinDays: 30, Exists: true, MinCount: 3},
		{Type: TypeSignup, Op: OpWithin, Days: 90},
		{Type: TypeMetric, Metric: MetricTotalSpent, Op: OpGTE, Value: 1000},
		{Type: TypeMetric, Metric: MetricOrderCount, Op: OpLTE, Value: 5, WithinDays: 60},
	}
	if !reflect.DeepEqual(rule.Conditions, want) {
		t.Fatalf("got %+v\nwant %+v", rule.Conditions, want)
	}
	// What the backend sends to the browser parses back to the same rule.
	out, err := json.Marshal(rule)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(out)
	if err != nil || !reflect.DeepEqual(again, rule) {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
	// Optional fields left out mean the same as null.
	short, err := Parse([]byte(`{"match":"all","conditions":[
	  {"type":"event","event":"purchase","within_days":7,"exists":true},
	  {"type":"metric","metric":"total_spent","op":"gte","value":50}]}`))
	if err != nil || short.Conditions[0].MinCount != 0 || short.Conditions[1].WithinDays != 0 {
		t.Fatalf("got %+v, %v", short, err)
	}
}

func TestExtendedConditionsRejected(t *testing.T) {
	cond := func(c string) string { return `{"match":"all","conditions":[` + c + `]}` }
	tests := []struct {
		name, json, path, want string
	}{
		{"min_count zero", cond(`{"type":"event","event":"purchase","within_days":30,"exists":true,"min_count":0}`), "conditions[0].min_count", "1 ile 100"},
		{"min_count too large", cond(`{"type":"event","event":"purchase","within_days":30,"exists":true,"min_count":101}`), "conditions[0].min_count", "1 ile 100"},
		{"min_count with exists false", cond(`{"type":"event","event":"purchase","within_days":30,"exists":false,"min_count":3}`), "conditions[0].min_count", "exists: false ile kullanılamaz"},
		{"min_count as text", cond(`{"type":"event","event":"purchase","within_days":30,"exists":true,"min_count":"3"}`), "conditions[0].min_count", "tam sayı olmalı"},
		{"same event twice with different counts", cond(`{"type":"event","event":"purchase","within_days":30,"exists":true,"min_count":2},{"type":"event","event":"purchase","within_days":30,"exists":true,"min_count":5}`), "conditions[1]", "tek koşulda birleştirin"},

		{"signup unknown op", cond(`{"type":"signup","op":"before","days":7}`), "conditions[0].op", "within, older_than"},
		{"signup zero days", cond(`{"type":"signup","op":"within","days":0}`), "conditions[0].days", "1 ile 3650"},
		{"signup with a date", cond(`{"type":"signup","op":"within","days":7,"since":"2026-01-01"}`), "conditions[0].since", "bilinmeyen alan"},
		{"signup field of another variant", cond(`{"type":"signup","op":"within","days":7,"within_days":7}`), "conditions[0].within_days", "bilinmeyen alan"},
		{"signup same bound twice", cond(`{"type":"signup","op":"within","days":7},{"type":"signup","op":"within","days":30}`), "conditions[1]", "aynı sınır"},
		{"signup empty range", cond(`{"type":"signup","op":"within","days":7},{"type":"signup","op":"older_than","days":30}`), "conditions[1]", "çelişiyor"},

		{"unknown metric", cond(`{"type":"metric","metric":"lifetime_value","op":"gte","value":10}`), "conditions[0].metric", "total_spent, order_count"},
		{"metric as sql", cond(`{"type":"metric","metric":"sum(amount)); DROP TABLE users; --","op":"gte","value":10}`), "conditions[0].metric", "geçerli bir metrik değil"},
		{"metric unknown op", cond(`{"type":"metric","metric":"total_spent","op":">","value":10}`), "conditions[0].op", "gte"},
		{"metric negative", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":-1}`), "conditions[0].value", "0 ile 1000000"},
		{"metric too large", cond(`{"type":"metric","metric":"order_count","op":"gte","value":1001}`), "conditions[0].value", "0 ile 1000"},
		{"metric decimal", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":99.5}`), "conditions[0].value", "tam sayı olmalı"},
		{"metric that selects everyone", cond(`{"type":"metric","metric":"order_count","op":"gte","value":0}`), "conditions[0].value", "herkesi seçer"},
		{"metric window out of range", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":10,"within_days":400}`), "conditions[0].within_days", "1 ile 365"},
		{"metric window zero", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":10,"within_days":0}`), "conditions[0].within_days", "1 ile 365"},
		{"metric with category", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":10,"category":"books"}`), "conditions[0].category", "bilinmeyen alan"},
		{"metric same bound twice", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":10},{"type":"metric","metric":"total_spent","op":"gte","value":20}`), "conditions[1]", "aynı sınır"},
		{"metric empty range", cond(`{"type":"metric","metric":"total_spent","op":"gte","value":1000},{"type":"metric","metric":"total_spent","op":"lte","value":500}`), "conditions[1]", "çelişiyor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.json))
			verr, ok := err.(ValidationError)
			if !ok {
				t.Fatalf("want a validation error, got %v", err)
			}
			for _, e := range verr {
				if e.Path == tt.path && strings.Contains(e.Message, tt.want) {
					return
				}
			}
			t.Fatalf("no error at %q containing %q:\n%v", tt.path, tt.want, err)
		})
	}
}

// Bounds that form a range, and the same metric over different periods, are
// meaningful and accepted.
func TestExtendedConditionsAccepted(t *testing.T) {
	for name, conditions := range map[string]string{
		"signup range":            `{"type":"signup","op":"within","days":90},{"type":"signup","op":"older_than","days":30}`,
		"spend range":             `{"type":"metric","metric":"total_spent","op":"gte","value":500},{"type":"metric","metric":"total_spent","op":"lte","value":1000}`,
		"same metric two periods": `{"type":"metric","metric":"total_spent","op":"gte","value":500},{"type":"metric","metric":"total_spent","op":"lte","value":100,"within_days":30}`,
		"never ordered":           `{"type":"metric","metric":"order_count","op":"lte","value":0}`,
		"count and absence":       `{"type":"event","event":"cart_add","within_days":30,"exists":true,"min_count":3},{"type":"event","event":"purchase","within_days":30,"exists":false}`,
	} {
		if _, err := Parse([]byte(`{"match":"all","conditions":[` + conditions + `]}`)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCompileExtendedConditions(t *testing.T) {
	rule, err := Parse([]byte(extendedJSON))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Compile(rule, asOf)
	if err != nil {
		t.Fatal(err)
	}
	wantSQL := "((SELECT count(*) FROM events e WHERE e.user_id = u.id AND e.event_type = $1 AND e.occurred_at >= $2 AND e.occurred_at <= $3 AND e.category = $4) >= $5::bigint" +
		" AND (u.signup_at >= $6 AND u.signup_at <= $7)" +
		" AND (SELECT COALESCE(sum(e.amount), 0) FROM events e WHERE e.user_id = u.id AND e.event_type = $8 AND e.occurred_at <= $9) >= $10::bigint" +
		" AND (SELECT count(*) FROM events e WHERE e.user_id = u.id AND e.event_type = $11 AND e.occurred_at <= $12 AND e.occurred_at >= $13) <= $14::bigint)"
	if q.Where != wantSQL {
		t.Fatalf("SQL:\n got %s\nwant %s", q.Where, wantSQL)
	}
	wantArgs := []any{
		"cart_add", asOf.AddDate(0, 0, -30), asOf, "electronics", 3,
		asOf.AddDate(0, 0, -90), asOf,
		"purchase", asOf, 1000,
		"purchase", asOf, asOf.AddDate(0, 0, -60), 5,
	}
	if !reflect.DeepEqual(q.Args, wantArgs) {
		t.Fatalf("args:\n got %v\nwant %v", q.Args, wantArgs)
	}
	// Thresholds, names and the clock are parameters, never SQL text.
	for _, leaked := range []string{"1000", "electronics", "purchase", "cart_add", "total_spent", "2026", "now()", "'"} {
		if strings.Contains(q.Where, leaked) {
			t.Errorf("SQL contains %q: %s", leaked, q.Where)
		}
	}

	older, err := Compile(Rule{Match: MatchAll, Conditions: []Condition{{Type: TypeSignup, Op: OpOlderThan, Days: 365}}}, asOf)
	if err != nil || older.Where != "(u.signup_at < $1)" || !reflect.DeepEqual(older.Args, []any{asOf.AddDate(0, 0, -365)}) {
		t.Fatalf("older_than: %q %v %v", older.Where, older.Args, err)
	}
}

// A rule built in Go with out-of-range values does not reach SQL either.
func TestCompileRejectsInvalidExtendedConditions(t *testing.T) {
	for _, c := range []Condition{
		{Type: TypeEvent, Event: "purchase", WithinDays: 30, Exists: true, MinCount: -1},
		{Type: TypeSignup, Op: "since", Days: 7},
		{Type: TypeMetric, Metric: "amount", Op: OpGTE, Value: 10},
		{Type: TypeMetric, Metric: MetricTotalSpent, Op: OpGTE, Value: 10, WithinDays: -5},
	} {
		if _, err := Compile(Rule{Match: MatchAll, Conditions: []Condition{c}}, asOf); err == nil {
			t.Errorf("compiled an invalid condition: %+v", c)
		}
	}
}

func TestDescribeExtendedConditions(t *testing.T) {
	rule, err := Parse([]byte(extendedJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Son 30 günde elektronik kategorisinde en az 3 kez sepete ürün ekledi",
		"Son 90 günde üye oldu",
		"Toplam harcama: en az 1000 TL (tüm zamanlar)",
		"Sipariş sayısı: en fazla 5 (son 60 günde)",
	}
	if got := Describe(rule).Conditions; !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	older := Describe(Rule{Match: MatchAll, Conditions: []Condition{{Type: TypeSignup, Op: OpOlderThan, Days: 365}}})
	if older.Conditions[0] != "365 günden daha önce üye oldu" {
		t.Errorf("got %q", older.Conditions[0])
	}
}

// The schema offers the model every variant the parser accepts.
func TestSchemaListsEveryConditionType(t *testing.T) {
	raw, err := json.Marshal(JSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range append(append([]string{}, conditionTypes...), "min_count", MetricTotalSpent, MetricOrderCount, OpWithin, OpOlderThan, OpGTE, OpLTE) {
		if !strings.Contains(string(raw), `"`+want+`"`) {
			t.Errorf("schema does not mention %q", want)
		}
	}
}
