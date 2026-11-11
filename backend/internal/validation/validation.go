// Package validation holds the error shape shared by everything that checks
// LLM or user input: a list of problems, each with the JSON path it is at.
// The same list is shown to the marketer and fed back to the model.
package validation

import "strings"

// FieldError points at one problem. Path uses the JSON location
// (e.g. "conditions[0].values[0]") so both a human and the LLM can find it.
type FieldError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e FieldError) String() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

// Errors lists every problem found, not just the first, so one retry can fix
// them all.
type Errors []FieldError

func (e Errors) Error() string {
	lines := make([]string, len(e))
	for i, fe := range e {
		lines[i] = fe.String()
	}
	return strings.Join(lines, "\n")
}

// WithPrefix returns the errors with prefix prepended to every path.
func (e Errors) WithPrefix(prefix string) Errors {
	out := make(Errors, len(e))
	for i, fe := range e {
		out[i] = FieldError{Path: JoinPath(prefix, fe.Path), Message: fe.Message}
	}
	return out
}

// JoinPath appends a key or an index ("[0]") to a JSON path.
func JoinPath(prefix, path string) string {
	switch {
	case prefix == "":
		return path
	case path == "":
		return prefix
	case strings.HasPrefix(path, "["):
		return prefix + path
	default:
		return prefix + "." + path
	}
}
