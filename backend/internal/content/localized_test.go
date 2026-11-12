package content

import (
	"reflect"
	"strings"
	"testing"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/validation"
)

var english = Content{
	SMS:   "Hi {{first_name}}, 30% off is waiting for you!",
	Email: Email{Subject: "{{first_name}}, your cart is waiting", Body: "Hi {{first_name}},\n\nHappy shopping!"},
}

func localizedJSON(def string, variants map[string]Content) []byte {
	return mustJSON(map[string]any{"default": def, "variants": variants})
}

// A bare set of texts is a single-language campaign in the default language:
// clients written before DECISIONS 30 keep working.
func TestParseLocalizedShorthand(t *testing.T) {
	got, lengths, err := ParseLocalized(mustJSON(valid()), testMax)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Single("tr", valid())) {
		t.Errorf("got %+v", got)
	}
	if want, _ := valid().Validate(testMax); lengths["tr"] != want {
		t.Errorf("lengths = %+v, want %+v", lengths, want)
	}
}

func TestParseLocalized(t *testing.T) {
	got, lengths, err := ParseLocalized(localizedJSON("tr", map[string]Content{"tr": valid(), "en": english}), testMax)
	if err != nil {
		t.Fatal(err)
	}
	if got.Default != "tr" || got.Variants["tr"] != valid() || got.Variants["en"] != english {
		t.Errorf("got %+v", got)
	}
	if !reflect.DeepEqual(got.Codes(), []string{"tr", "en"}) {
		t.Errorf("codes = %v", got.Codes())
	}
	// 3 + 10 (first_name at its longest) + 29
	if lengths["en"].SMS != 42 || len(lengths) != 2 {
		t.Errorf("lengths = %+v", lengths)
	}

	// English may be the default too; it is listed first then.
	got, _, err = ParseLocalized(localizedJSON("en", map[string]Content{"tr": valid(), "en": english}), testMax)
	if err != nil || !reflect.DeepEqual(got.Codes(), []string{"en", "tr"}) {
		t.Errorf("codes = %v, err = %v", got.Codes(), err)
	}
}

// Every language gets the checks a single text gets, and the error says
// which language it is about.
func TestParseLocalizedRejects(t *testing.T) {
	long := english
	long.SMS = strings.Repeat("x", 151) + "{{first_name}}"
	unknownVar := english
	unknownVar.Email.Subject = "Hi {{nickname}}"

	tests := []struct {
		name, body, path, message string
	}{
		{"a variant over the limit", string(localizedJSON("tr", map[string]Content{"tr": valid(), "en": long})), "variants.en.sms", "161 karakter, limit 160"},
		{"a variant with an unknown variable", string(localizedJSON("tr", map[string]Content{"tr": valid(), "en": unknownVar})), "variants.en.email.subject", "bilinmeyen bir değişken"},
		{"a language outside the set", string(localizedJSON("tr", map[string]Content{"tr": valid(), "de": english})), "variants.de", "bilinmeyen bir dil"},
		{"no text for the default language", string(localizedJSON("en", map[string]Content{"tr": valid()})), "default", "için metin yok"},
		{"no default", `{"variants":{"tr":` + string(mustJSON(valid())) + `}}`, "default", "zorunlu alan eksik"},
		{"no variants", `{"default":"tr","variants":{}}`, "variants", "en az bir dil"},
		{"variants is not an object", `{"default":"tr","variants":[]}`, "variants", "JSON nesnesi"},
		{"an extra field", `{"default":"tr","variants":{"tr":` + string(mustJSON(valid())) + `},"push":"x"}`, "push", "bilinmeyen alan"},
		{"a variant with a missing field", `{"default":"tr","variants":{"tr":{"sms":"a"}}}`, "variants.tr.email", "zorunlu alan eksik"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseLocalized([]byte(tt.body), testMax)
			errs, ok := err.(validation.Errors)
			if !ok {
				t.Fatalf("err = %v", err)
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

// A recipient gets the text of their own language and nothing else; when
// the campaign has none for them, the default language.
func TestLocalizedFor(t *testing.T) {
	both := Localized{Default: "tr", Variants: map[string]Content{"tr": valid(), "en": english}}
	if both.For("en") != english || both.For("tr") != valid() {
		t.Error("a recipient did not get their own language")
	}
	only := Single("tr", valid())
	if only.For("en") != valid() || only.For("") != valid() {
		t.Error("a recipient without a variant did not get the default language")
	}
}

func TestLocalizedStorage(t *testing.T) {
	in := Localized{Default: "tr", Variants: map[string]Content{"tr": valid(), "en": english}}
	raw, err := MarshalLocalized(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalLocalized(raw)
	if err != nil || !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %+v, err = %v", out, err)
	}
	// The pre-migration shape has no default language: it must not be read
	// as a campaign without texts.
	if _, err := UnmarshalLocalized(mustJSON(valid())); err == nil {
		t.Error("content without a default language was accepted")
	}
}
