package content

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// fieldStream turns a JSON document that arrives in arbitrary pieces into
// per-field text deltas, so the UI can fill the SMS and email boxes while the
// model is still writing (DECISIONS 18).
//
// It is a display aid only: it never decides whether a reply is valid. It
// follows object keys, decodes string escapes and reports the text of the
// string values whose path is in fields; everything else is skipped. On
// malformed input it emits whatever it can make sense of and Parse rejects
// the reply afterwards.
type fieldStream struct {
	fields map[string]bool
	emit   func(field, text string)

	stack []frame

	inString bool
	isKey    bool   // the string being read is an object key
	field    string // path of the string value being read; "" when not reported
	escaped  bool
	hexLeft  int  // hex digits still expected of a \uXXXX escape
	hexVal   rune // value of the escape so far
	high     rune // pending UTF-16 high surrogate

	key []byte // decoded bytes of the key being read
	out []byte // decoded bytes of the value, not yet emitted
}

type frame struct {
	object    bool
	key       string // current key (objects only)
	expectKey bool
}

func newFieldStream(emit func(field, text string), fields ...string) *fieldStream {
	fs := &fieldStream{fields: map[string]bool{}, emit: emit}
	for _, f := range fields {
		fs.fields[f] = true
	}
	return fs
}

// Write consumes the next piece of the document.
func (fs *fieldStream) Write(chunk string) {
	for i := 0; i < len(chunk); i++ {
		fs.step(chunk[i])
	}
	fs.flush(false)
}

func (fs *fieldStream) step(b byte) {
	if !fs.inString {
		fs.structural(b)
		return
	}
	switch {
	case fs.hexLeft > 0:
		fs.hexVal = fs.hexVal<<4 | hexDigit(b)
		if fs.hexLeft--; fs.hexLeft == 0 {
			fs.unit(fs.hexVal)
		}
	case fs.escaped:
		fs.escaped = false
		if b != 'u' {
			fs.dropSurrogate()
		}
		switch b {
		case 'n':
			fs.append('\n')
		case 't':
			fs.append('\t')
		case 'r':
			fs.append('\r')
		case 'b':
			fs.append('\b')
		case 'f':
			fs.append('\f')
		case 'u':
			fs.hexLeft, fs.hexVal = 4, 0
		default: // \" \\ \/ and anything unexpected: the character itself
			fs.append(b)
		}
	case b == '\\':
		fs.escaped = true
	case b == '"':
		fs.dropSurrogate()
		if fs.isKey {
			fs.stack[len(fs.stack)-1].key = string(fs.key)
		} else {
			fs.flush(true)
		}
		fs.inString, fs.field = false, ""
	default:
		fs.dropSurrogate()
		fs.append(b)
	}
}

func (fs *fieldStream) structural(b byte) {
	top := func() *frame {
		if len(fs.stack) == 0 {
			return nil
		}
		return &fs.stack[len(fs.stack)-1]
	}
	switch b {
	case '{':
		fs.stack = append(fs.stack, frame{object: true, expectKey: true})
	case '[':
		fs.stack = append(fs.stack, frame{})
	case '}', ']':
		if len(fs.stack) > 0 {
			fs.stack = fs.stack[:len(fs.stack)-1]
		}
	case ':':
		if t := top(); t != nil && t.object {
			t.expectKey = false
		}
	case ',':
		if t := top(); t != nil && t.object {
			t.expectKey = true
		}
	case '"':
		fs.inString = true
		t := top()
		fs.isKey = t != nil && t.object && t.expectKey
		if fs.isKey {
			fs.key = fs.key[:0]
		} else if path := fs.path(); fs.fields[path] {
			fs.field = path
		}
	}
}

// path is the location of the value being read, e.g. "email.subject".
func (fs *fieldStream) path() string {
	parts := make([]string, len(fs.stack))
	for i, f := range fs.stack {
		if f.object {
			parts[i] = f.key
		} else {
			parts[i] = "[]"
		}
	}
	return strings.Join(parts, ".")
}

func (fs *fieldStream) append(b byte) {
	switch {
	case fs.isKey:
		fs.key = append(fs.key, b)
	case fs.field != "":
		fs.out = append(fs.out, b)
	}
}

func (fs *fieldStream) appendRune(r rune) {
	var buf [utf8.UTFMax]byte
	for _, b := range buf[:utf8.EncodeRune(buf[:], r)] {
		fs.append(b)
	}
}

// unit handles one \uXXXX code unit; characters outside the BMP arrive as a
// surrogate pair of two escapes.
func (fs *fieldStream) unit(r rune) {
	if fs.high != 0 {
		high := fs.high
		fs.high = 0
		if combined := utf16.DecodeRune(high, r); combined != utf8.RuneError {
			fs.appendRune(combined)
			return
		}
		fs.appendRune(utf8.RuneError)
	}
	switch {
	case r >= 0xD800 && r < 0xDC00:
		fs.high = r
	case utf16.IsSurrogate(r):
		fs.appendRune(utf8.RuneError)
	default:
		fs.appendRune(r)
	}
}

// dropSurrogate resolves a high surrogate that was not followed by a low one.
func (fs *fieldStream) dropSurrogate() {
	if fs.high != 0 {
		fs.high = 0
		fs.appendRune(utf8.RuneError)
	}
}

// flush emits the decoded text so far. Unless the string has ended it holds
// back a trailing incomplete UTF-8 sequence, so a delta never splits a
// character.
func (fs *fieldStream) flush(all bool) {
	if fs.field == "" || len(fs.out) == 0 {
		fs.out = fs.out[:0]
		return
	}
	n := len(fs.out)
	if !all {
		for i := n - 1; i >= 0 && i >= n-utf8.UTFMax; i-- {
			if utf8.RuneStart(fs.out[i]) {
				if !utf8.FullRune(fs.out[i:]) {
					n = i
				}
				break
			}
		}
	}
	if n > 0 {
		fs.emit(fs.field, string(fs.out[:n]))
	}
	fs.out = append(fs.out[:0], fs.out[n:]...)
}

func hexDigit(b byte) rune {
	switch {
	case b >= '0' && b <= '9':
		return rune(b - '0')
	case b >= 'a' && b <= 'f':
		return rune(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return rune(b-'A') + 10
	}
	return 0
}
