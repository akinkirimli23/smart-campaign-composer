package content

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// collect feeds doc to a fieldStream in pieces of n bytes and returns the
// text per field plus every delta.
func collect(doc string, n int) (map[string]string, []string) {
	got := map[string]string{}
	var deltas []string
	fs := newFieldStream(func(field, text string) {
		got[field] += text
		deltas = append(deltas, text)
	}, FieldSMS, FieldEmailSubject, FieldEmailBody)
	for len(doc) > 0 {
		k := min(n, len(doc))
		fs.Write(doc[:k])
		doc = doc[k:]
	}
	return got, deltas
}

// Whatever the chunking, the streamed text equals what encoding/json decodes,
// and no delta ever splits a character.
func TestFieldStreamMatchesJSONDecoder(t *testing.T) {
	docs := map[string]string{
		"plain":           `{"sms":"Merhaba {{first_name}}, %30 indirim!","email":{"subject":"Şimdi başla","body":"Satır 1\nSatır 2"}}`,
		"escapes":         `{"sms":"a \"quoted\" word, a back\\slash, a tab\t and a slash\/","email":{"subject":"s","body":"b"}}`,
		"unicode escapes": `{"sms":"çay İstanbul 🎉 bitti","email":{"subject":"ş","body":"😀"}}`,
		"raw emoji":       `{"sms":"🎉 İndirim 🎉","email":{"subject":"ğüşiöç","body":"€"}}`,
		"whitespace":      "{\n  \"sms\" : \"a\" ,\n  \"email\" : {\n    \"subject\" : \"b\" ,\n    \"body\" : \"c\"\n  }\n}",
		"other keys":      `{"note":"sms","n":[1,{"sms":"not this"},"x"],"sms":"real","email":{"body":"b","subject":"s","extra":{"subject":"no"}}}`,
		"key order":       `{"email":{"body":"b first","subject":"s"},"sms":"last"}`,
		"braces in text":  `{"sms":"{ \"a\": [1] } , : ","email":{"subject":"}{","body":"]["}}`,
		"lone surrogate":  `{"sms":"a\ud83cb","email":{"subject":"s","body":"b"}}`,
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			var want struct {
				SMS   string `json:"sms"`
				Email struct{ Subject, Body string }
			}
			if err := json.Unmarshal([]byte(doc), &want); err != nil {
				t.Fatalf("test document is not valid JSON: %v", err)
			}
			for n := 1; n <= len(doc); n++ {
				got, deltas := collect(doc, n)
				if got[FieldSMS] != want.SMS || got[FieldEmailSubject] != want.Email.Subject || got[FieldEmailBody] != want.Email.Body {
					t.Fatalf("chunk size %d: got %q, want %+v", n, got, want)
				}
				if len(got) != 3 {
					t.Fatalf("chunk size %d: unexpected fields %v", n, got)
				}
				for _, d := range deltas {
					if !utf8.ValidString(d) {
						t.Fatalf("chunk size %d: delta %q splits a character", n, d)
					}
				}
			}
		})
	}
}

// Text is reported as it arrives, not when the string closes.
func TestFieldStreamIsIncremental(t *testing.T) {
	var deltas []string
	fs := newFieldStream(func(_, text string) { deltas = append(deltas, text) }, FieldSMS)
	fs.Write(`{"sms":"Mer`)
	fs.Write(`haba {{fir`)
	fs.Write(`st_name}}"`)
	if got := strings.Join(deltas, "|"); got != "Mer|haba {{fir|st_name}}" {
		t.Errorf("deltas = %q", got)
	}
}

// Broken input must not panic or loop; what it emits is not validated here.
func TestFieldStreamSurvivesGarbage(t *testing.T) {
	for _, doc := range []string{
		``, `}}}]]]`, `"sms":"x"`, `{"sms":"unterminated`, `{"sms":"\u12`, `{"sms":"\`, `{{{{"sms":"deep"`,
		"Here is the JSON:\n```json\n{\"sms\":\"x\"}\n```", `{"sms":"a"}{"sms":"b"}`,
	} {
		for n := 1; n <= len(doc)+1; n++ {
			collect(doc, n)
		}
	}
}
