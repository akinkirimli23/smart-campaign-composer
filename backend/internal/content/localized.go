package content

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/validation"
)

// Language is a language campaign texts can be written in.
type Language struct {
	Code  string // as stored in users.language
	Name  string // English name, used in prompts
	Label string // Turkish name, shown to the marketer
}

// Languages is the closed set of content languages. It must cover every
// value users.language can take; a recipient whose language has no text of
// its own receives the campaign's default language.
var Languages = []Language{
	{Code: "tr", Name: "Turkish", Label: "Türkçe"},
	{Code: "en", Name: "English", Label: "İngilizce"},
}

// DefaultLanguage is the language of a campaign that names none.
const DefaultLanguage = "tr"

// LanguageByCode looks a language up by its code.
func LanguageByCode(code string) (Language, bool) {
	for _, l := range Languages {
		if l.Code == code {
			return l, true
		}
	}
	return Language{}, false
}

func languageCodes() string {
	codes := make([]string, len(Languages))
	for i, l := range Languages {
		codes[i] = l.Code
	}
	return strings.Join(codes, ", ")
}

// Localized is the content of a campaign: one set of texts per language
// (DECISIONS 30). Default names the set every recipient falls back to when
// their own language has none, so nobody is left without a message.
type Localized struct {
	Default  string             `json:"default"`
	Variants map[string]Content `json:"variants"`
}

// Single is a campaign written in one language only.
func Single(language string, c Content) Localized {
	return Localized{Default: language, Variants: map[string]Content{language: c}}
}

// For returns the texts a recipient with the given language receives.
func (l Localized) For(language string) Content {
	if c, ok := l.Variants[language]; ok {
		return c
	}
	return l.Variants[l.Default]
}

// Codes lists the languages that have texts: the default first, the rest
// in alphabetical order.
func (l Localized) Codes() []string {
	codes := make([]string, 0, len(l.Variants))
	for code := range l.Variants {
		if code != l.Default {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	if _, ok := l.Variants[l.Default]; ok {
		codes = append([]string{l.Default}, codes...)
	}
	return codes
}

// ParseLocalized decodes and validates campaign content from a client.
//
// It accepts {"default": "tr", "variants": {"tr": {...}, "en": {...}}} and,
// as shorthand for a single-language campaign in DefaultLanguage, a bare
// {"sms": ..., "email": ...}. Every variant goes through the same checks as a
// model reply; errors carry the variant in their path ("variants.en.sms").
func ParseLocalized(data []byte, max VarLengths) (Localized, map[string]Lengths, error) {
	root, isObject := object(data)
	if _, localized := root["variants"]; !isObject || !localized {
		c, lengths, err := Parse(data, max)
		if err != nil {
			return Localized{}, nil, err
		}
		return Single(DefaultLanguage, c), map[string]Lengths{DefaultLanguage: lengths}, nil
	}

	var errs validation.Errors
	add := func(path, format string, args ...any) {
		errs = append(errs, validation.FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	checkKeys(add, "", root, "default", "variants")
	out := Localized{Default: text(add, "default", root["default"]), Variants: map[string]Content{}}
	lengths := map[string]Lengths{}

	variants, ok := object(root["variants"])
	switch {
	case !ok:
		add("variants", "JSON nesnesi olmalı")
	case len(variants) == 0:
		add("variants", "en az bir dil gerekli")
	}
	codes := make([]string, 0, len(variants))
	for code := range variants {
		codes = append(codes, code)
	}
	sort.Strings(codes) // deterministic error order
	for _, code := range codes {
		path := "variants." + code
		if _, known := LanguageByCode(code); !known {
			add(path, "%q bilinmeyen bir dil; izinli diller: %s", code, languageCodes())
			continue
		}
		c, l, err := Parse(variants[code], max)
		if err != nil {
			errs = append(errs, err.(validation.Errors).WithPrefix(path)...)
			continue
		}
		out.Variants[code], lengths[code] = c, l
	}
	if _, present := root["default"]; present && len(errs) == 0 {
		if _, ok := variants[out.Default]; !ok {
			add("default", "%q için metin yok; varsayılan dil variants içinde olmalı", out.Default)
		}
	}
	if len(errs) > 0 {
		return Localized{}, nil, errs
	}
	return out, lengths, nil
}

// MarshalLocalized is the stored form of campaign content.
func MarshalLocalized(l Localized) ([]byte, error) { return json.Marshal(l) }

// UnmarshalLocalized reads stored campaign content. It was validated before
// it was stored, so this only decodes.
func UnmarshalLocalized(data []byte) (Localized, error) {
	var l Localized
	if err := json.Unmarshal(data, &l); err != nil {
		return Localized{}, err
	}
	if _, ok := l.Variants[l.Default]; !ok {
		return Localized{}, fmt.Errorf("content: no texts for the default language %q", l.Default)
	}
	return l, nil
}
