// Package content defines the campaign texts an LLM may propose (SMS and
// email), validates them and generates them with a bounded retry loop.
//
// A text is a template: literal characters plus {{variables}} from a closed
// set. Limits are checked against the worst case, i.e. with every variable
// replaced by its longest possible value, and counted in runes.
package content

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Variable is a personalisation variable a template may use.
type Variable struct {
	Name        string // as written in a template: {{first_name}}
	Column      string // users column it is filled from; a constant, never input
	Description string // shown to the model
}

// Variables is the closed set of template variables and the single source for
// the prompt, the validator, the length query and (stage 4) the renderer.
var Variables = []Variable{
	{Name: "first_name", Column: "first_name", Description: "recipient's first name"},
	{Name: "last_name", Column: "last_name", Description: "recipient's last name"},
	{Name: "city", Column: "city", Description: "recipient's city"},
}

// VarLengths is the longest value (in runes) each variable can take.
type VarLengths map[string]int

func variableNames() []string {
	names := make([]string, len(Variables))
	for i, v := range Variables {
		names[i] = "{{" + v.Name + "}}"
	}
	return names
}

func isVariable(name string) bool {
	for _, v := range Variables {
		if v.Name == name {
			return true
		}
	}
	return false
}

// part is a literal run of text or one variable reference.
type part struct {
	literal  string
	variable string
}

var varNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// parseTemplate splits s into parts. Anything that looks like a variable but
// is not one from the closed set is a problem: sent as-is, the recipient
// would read a literal "{{name}}".
func parseTemplate(s string) (parts []part, problems []string) {
	rest := s
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			break
		}
		if open > 0 {
			parts = append(parts, part{literal: rest[:open]})
		}
		rest = rest[open+2:]
		end := strings.Index(rest, "}}")
		if end < 0 || strings.Contains(rest[:end], "{{") {
			problems = append(problems, "kapanmamış \"{{\" var; değişkenler {{ad}} biçiminde yazılır")
			return parts, problems
		}
		name := strings.TrimSpace(rest[:end])
		rest = rest[end+2:]
		switch {
		case !varNameRe.MatchString(name):
			problems = append(problems, fmt.Sprintf("{{%s}} geçerli bir değişken adı değil; izinli değişkenler: %s", name, strings.Join(variableNames(), ", ")))
		case !isVariable(name):
			problems = append(problems, fmt.Sprintf("{{%s}} bilinmeyen bir değişken; izinli değişkenler: %s", name, strings.Join(variableNames(), ", ")))
		default:
			parts = append(parts, part{variable: name})
		}
	}
	if rest != "" {
		parts = append(parts, part{literal: rest})
	}
	for _, p := range parts {
		if strings.Contains(p.literal, "}}") {
			problems = append(problems, "karşılığı olmayan \"}}\" var")
			break
		}
	}
	return parts, problems
}

// worstCase returns the rune length of the template with every variable at
// its longest value, and the variables it uses (in order, without repeats).
func worstCase(parts []part, max VarLengths) (length int, used []string) {
	seen := map[string]bool{}
	for _, p := range parts {
		if p.variable == "" {
			length += utf8.RuneCountInString(p.literal)
			continue
		}
		length += max[p.variable]
		if !seen[p.variable] {
			seen[p.variable] = true
			used = append(used, p.variable)
		}
	}
	return length, used
}

// Render fills a template for one recipient. values must have an entry for
// every variable the template uses; the template must already be valid.
func Render(template string, values map[string]string) (string, error) {
	parts, problems := parseTemplate(template)
	if len(problems) > 0 {
		return "", fmt.Errorf("content: invalid template: %s", strings.Join(problems, "; "))
	}
	var b strings.Builder
	for _, p := range parts {
		if p.variable == "" {
			b.WriteString(p.literal)
			continue
		}
		v, ok := values[p.variable]
		if !ok {
			return "", fmt.Errorf("content: no value for {{%s}}", p.variable)
		}
		b.WriteString(v)
	}
	return b.String(), nil
}
