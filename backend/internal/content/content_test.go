package content

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/validation"
)

var testMax = VarLengths{"first_name": 10, "last_name": 12, "city": 9}

func valid() Content {
	return Content{
		SMS:   "Merhaba {{first_name}}, sepetindeki ürünler seni bekliyor!",
		Email: Email{Subject: "{{first_name}}, %30 indirim seni bekliyor", Body: "Merhaba {{first_name}},\n\nİyi alışverişler!"},
	}
}

func marshal(t *testing.T, c Content) []byte {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// errorAt returns the message of the single error at path, failing otherwise.
func errorAt(t *testing.T, err error, path string) string {
	t.Helper()
	errs, ok := err.(validation.Errors)
	if !ok {
		t.Fatalf("want validation errors, got %v", err)
	}
	var found []string
	for _, e := range errs {
		if e.Path == path {
			found = append(found, e.Message)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly one error at %q, got:\n%v", path, err)
	}
	return found[0]
}

func TestParseValid(t *testing.T) {
	c, lengths, err := Parse(marshal(t, valid()), testMax)
	if err != nil {
		t.Fatal(err)
	}
	if c != valid() {
		t.Errorf("content changed: %+v", c)
	}
	// "Merhaba " + 10 + ", sepetindeki ürünler seni bekliyor!"
	wantSMS := utf8.RuneCountInString("Merhaba , sepetindeki ürünler seni bekliyor!") + 10
	if lengths.SMS != wantSMS {
		t.Errorf("sms length = %d, want %d", lengths.SMS, wantSMS)
	}
}

// The limit applies after personalisation, with the longest value, in runes.
func TestLimitsAreMeasuredAfterPersonalisation(t *testing.T) {
	const prefix = "Merhaba {{first_name}}, "
	fixed := utf8.RuneCountInString("Merhaba , ")

	atLimit := valid()
	atLimit.SMS = prefix + strings.Repeat("ş", MaxSMS-fixed-testMax["first_name"])
	if len(atLimit.SMS) <= MaxSMS {
		t.Fatal("test needs a text that is over the limit in bytes but not in runes")
	}
	lengths, err := atLimit.Validate(testMax)
	if err != nil {
		t.Fatalf("exactly %d characters must pass: %v", MaxSMS, err)
	}
	if lengths.SMS != MaxSMS {
		t.Errorf("length = %d, want %d", lengths.SMS, MaxSMS)
	}

	// The same template is too long once the longest name is one character longer.
	longer := VarLengths{"first_name": 11, "last_name": 12, "city": 9}
	_, err = atLimit.Validate(longer)
	msg := errorAt(t, err, "sms")
	for _, want := range []string{"161 karakter", "limit 160", "en az 1 karakter", "{{first_name}} 11 karakter"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}

	// The measured length is what a real recipient with the longest name gets.
	rendered, err := Render(atLimit.SMS, map[string]string{"first_name": strings.Repeat("ğ", 10)})
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(rendered); n != lengths.SMS {
		t.Errorf("rendered length %d != measured length %d", n, lengths.SMS)
	}

	subject := valid()
	subject.Email.Subject = strings.Repeat("a", MaxEmailSubject-testMax["city"]+1) + "{{city}}"
	_, err = subject.Validate(testMax)
	if msg := errorAt(t, err, "email.subject"); !strings.Contains(msg, "81 karakter, limit 80") {
		t.Errorf("message = %q", msg)
	}
}

// A variable used twice is counted twice.
func TestRepeatedVariableCountsEachTime(t *testing.T) {
	c := valid()
	c.SMS = "{{first_name}} {{first_name}}"
	lengths, err := c.Validate(testMax)
	if err != nil {
		t.Fatal(err)
	}
	if lengths.SMS != 21 {
		t.Errorf("length = %d, want 21", lengths.SMS)
	}
}

func TestTemplateProblems(t *testing.T) {
	tests := []struct {
		name, sms, want string
	}{
		{"unknown variable", "Merhaba {{name}}", "{{name}} bilinmeyen bir değişken"},
		{"variable from another system", "Merhaba {{user.first_name}}", "geçerli bir değişken adı değil"},
		{"upper case", "Merhaba {{FIRST_NAME}}", "geçerli bir değişken adı değil"},
		{"empty", "Merhaba {{}}", "geçerli bir değişken adı değil"},
		{"unclosed", "Merhaba {{first_name", "kapanmamış"},
		{"nested", "Merhaba {{ {{first_name}} }}", "kapanmamış"},
		{"stray close", "Merhaba first_name}}", "karşılığı olmayan"},
		{"blank", "   ", "boş olamaz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			c.SMS = tt.sms
			_, err := c.Validate(testMax)
			if msg := errorAt(t, err, "sms"); !strings.Contains(msg, tt.want) {
				t.Errorf("message %q does not contain %q", msg, tt.want)
			}
		})
	}

	// Spaces inside the braces and single braces are fine.
	c := valid()
	c.SMS = "Merhaba {{ first_name }}, {kod} }"
	if _, err := c.Validate(testMax); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// The error names the allowed variables so the model can fix it in one go.
	c.SMS = "{{name}}"
	_, err := c.Validate(testMax)
	if msg := errorAt(t, err, "sms"); !strings.Contains(msg, "{{first_name}}, {{last_name}}, {{city}}") {
		t.Errorf("message = %q", msg)
	}
}

func TestSubjectMustBeOneLine(t *testing.T) {
	c := valid()
	c.Email.Subject = "İndirim\nBcc: someone@example.com"
	_, err := c.Validate(testMax)
	if msg := errorAt(t, err, "email.subject"); !strings.Contains(msg, "satır sonu") {
		t.Errorf("message = %q", msg)
	}
}

// Every problem is reported in one pass so a single retry can fix them all.
func TestAllProblemsReportedAtOnce(t *testing.T) {
	c := Content{SMS: strings.Repeat("x", 200), Email: Email{Subject: "{{nope}}", Body: ""}}
	_, err := c.Validate(testMax)
	errs, _ := err.(validation.Errors)
	if len(errs) != 3 {
		t.Fatalf("want 3 errors, got:\n%v", err)
	}
}

func TestParseIsStrict(t *testing.T) {
	tests := []struct {
		name, json, path, want string
	}{
		{"not json", `{"sms": "a", `, "", "geçerli JSON değil"},
		{"text around json", "Here you go:\n" + string(mustJSON(valid())), "", "geçerli JSON değil"},
		{"array", `[]`, "", "JSON nesnesi olmalı"},
		{"missing sms", `{"email":{"subject":"a","body":"b"}}`, "sms", "zorunlu alan eksik"},
		{"missing body", `{"sms":"a","email":{"subject":"a"}}`, "email.body", "zorunlu alan eksik"},
		{"null sms", `{"sms":null,"email":{"subject":"a","body":"b"}}`, "sms", "metin olmalı"},
		{"number", `{"sms":5,"email":{"subject":"a","body":"b"}}`, "sms", "metin olmalı"},
		{"email as string", `{"sms":"a","email":"b"}`, "email", "JSON nesnesi olmalı"},
		{"unknown key", `{"sms":"a","email":{"subject":"a","body":"b"},"push":"c"}`, "push", "bilinmeyen alan"},
		{"unknown nested key", `{"sms":"a","email":{"subject":"a","body":"b","html":"<b>"}}`, "email.html", "bilinmeyen alan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Parse([]byte(tt.json), testMax)
			if msg := errorAt(t, err, tt.path); !strings.Contains(msg, tt.want) {
				t.Errorf("message %q does not contain %q", msg, tt.want)
			}
		})
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestRender(t *testing.T) {
	got, err := Render("Merhaba {{first_name}} {{ last_name }}, {{city}}!", map[string]string{
		"first_name": "Ayşe", "last_name": "Yılmaz", "city": "İzmir",
	})
	if err != nil || got != "Merhaba Ayşe Yılmaz, İzmir!" {
		t.Errorf("got %q, %v", got, err)
	}
	// Values are inserted verbatim: braces in a value are not re-expanded.
	got, err = Render("{{first_name}}", map[string]string{"first_name": "{{city}}"})
	if err != nil || got != "{{city}}" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := Render("{{first_name}}", nil); err == nil {
		t.Error("missing value must be an error, not an empty string")
	}
	if _, err := Render("{{nope}}", nil); err == nil {
		t.Error("invalid template must be an error")
	}
}

// The schema given to the model and the parser must agree on the shape.
func TestSchemaMatchesParser(t *testing.T) {
	schema := Schema()
	props := schema["properties"].(map[string]any)
	if len(props) != 2 || props["sms"] == nil || props["email"] == nil {
		t.Fatalf("top-level properties = %v", props)
	}
	email := props["email"].(map[string]any)
	if email["additionalProperties"] != false || schema["additionalProperties"] != false {
		t.Error("structured outputs require additionalProperties: false on every object")
	}
	if got := len(email["properties"].(map[string]any)); got != 2 {
		t.Errorf("email has %d properties, want 2", got)
	}
}
