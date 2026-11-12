package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/akinkirimli23/smart-campaign-composer/backend/internal/validation"
)

// Limits from the case. They apply after personalisation, in runes.
const (
	MaxSMS          = 160
	MaxEmailSubject = 80
	// MaxEmailBody is not a case requirement; it bounds what a model (or a
	// client) can make us store and send.
	MaxEmailBody = 5000
)

// Email is the email variant of a campaign text. Body is plain text.
type Email struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Content is one set of campaign texts. Every field is a template.
//
// A campaign holds one set per language (Localized, DECISIONS 30). Validate
// works on a single set on purpose: every language goes through it.
type Content struct {
	SMS   string `json:"sms"`
	Email Email  `json:"email"`
}

// Lengths are the worst-case lengths of the limited fields: every variable
// replaced by its longest value.
type Lengths struct {
	SMS          int `json:"sms"`
	EmailSubject int `json:"email_subject"`
}

// Parse decodes a model reply (or a client payload) strictly and validates it.
// Unknown keys, missing keys and non-string values are errors, not defaults.
func Parse(data []byte, max VarLengths) (Content, Lengths, error) {
	var errs validation.Errors
	add := func(path, format string, args ...any) {
		errs = append(errs, validation.FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
	}

	root, ok := object(data)
	if !ok {
		if json.Valid(data) {
			add("", "JSON nesnesi olmalı")
		} else {
			add("", "geçerli JSON değil")
		}
		return Content{}, Lengths{}, errs
	}
	var c Content
	checkKeys(add, "", root, "sms", "email")
	c.SMS = text(add, "sms", root["sms"])
	if raw, present := root["email"]; present {
		if email, ok := object(raw); ok {
			checkKeys(add, "email", email, "subject", "body")
			c.Email.Subject = text(add, "email.subject", email["subject"])
			c.Email.Body = text(add, "email.body", email["body"])
		} else {
			add("email", "JSON nesnesi olmalı")
		}
	}
	if len(errs) > 0 {
		// A structurally broken reply is not measured: the lengths would be
		// those of fields that are not there.
		return Content{}, Lengths{}, errs
	}
	lengths, err := c.Validate(max)
	if err != nil {
		return Content{}, Lengths{}, err
	}
	return c, lengths, nil
}

// Validate checks templates and limits. It is the one check for model output
// and for text a marketer edited by hand. All problems are reported at once.
func (c Content) Validate(max VarLengths) (Lengths, error) {
	var errs validation.Errors
	var lengths Lengths
	lengths.SMS = checkField(&errs, "sms", c.SMS, MaxSMS, max)
	lengths.EmailSubject = checkField(&errs, "email.subject", c.Email.Subject, MaxEmailSubject, max)
	checkField(&errs, "email.body", c.Email.Body, MaxEmailBody, max)
	if strings.ContainsAny(c.Email.Subject, "\r\n") {
		// A line break in a subject is a header-injection vector once a real
		// provider is plugged in.
		errs = append(errs, validation.FieldError{Path: "email.subject", Message: "satır sonu içeremez"})
	}
	if len(errs) > 0 {
		return Lengths{}, errs
	}
	return lengths, nil
}

func checkField(errs *validation.Errors, path, template string, limit int, max VarLengths) int {
	add := func(format string, args ...any) {
		*errs = append(*errs, validation.FieldError{Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if !utf8.ValidString(template) {
		add("geçerli UTF-8 değil")
		return 0
	}
	if strings.TrimSpace(template) == "" {
		add("boş olamaz")
		return 0
	}
	parts, problems := parseTemplate(template)
	for _, p := range problems {
		add("%s", p)
	}
	if len(problems) > 0 {
		return 0
	}
	length, used := worstCase(parts, max)
	for _, name := range used {
		if _, ok := max[name]; !ok {
			add("{{%s}} için en uzun değer bilinmiyor, uzunluk ölçülemedi", name)
			return 0
		}
	}
	if length > limit {
		detail := ""
		if len(used) > 0 {
			counted := make([]string, len(used))
			for i, name := range used {
				counted[i] = fmt.Sprintf("{{%s}} %d karakter sayılır", name, max[name])
			}
			detail = "; değişkenler en uzun değerleriyle ölçülür (" + strings.Join(counted, ", ") + ")"
		}
		add("%d karakter, limit %d; en az %d karakter kısaltın%s", length, limit, length-limit, detail)
	}
	return length
}

// --- strict JSON helpers ------------------------------------------------------

func object(data []byte) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return nil, false
	}
	return obj, true
}

func checkKeys(add func(string, string, ...any), path string, obj map[string]json.RawMessage, required ...string) {
	known := map[string]bool{}
	for _, k := range required {
		known[k] = true
		if _, ok := obj[k]; !ok {
			add(validation.JoinPath(path, k), "zorunlu alan eksik")
		}
	}
	var unknown []string
	for k := range obj {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown) // deterministic error order
	for _, k := range unknown {
		add(validation.JoinPath(path, k), "bilinmeyen alan; izinli alanlar: %s", strings.Join(required, ", "))
	}
}

// text returns a string value; a missing key was already reported by checkKeys.
func text(add func(string, string, ...any), path string, raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &s) != nil {
		add(path, "metin olmalı")
		return ""
	}
	return s
}
