package output

import (
	"bytes"
	"strings"
)

// colorizeYAML dims the structural scaffolding of already-encoded YAML (canonical
// block style, as produced by a registered yaml.v3 encoder): indentation markers,
// keys, the ":" and "-" punctuation, and quote delimiters all take the dim style,
// so ordinary string values keep the terminal default and stand out as the data.
// Numbers/bools get muted accents, null is dim, and values of the known envelope
// fields get their semantic role. Like colorizeJSON it only inserts escapes
// around existing bytes — stripping them reproduces src exactly — and anything it
// doesn't confidently recognize is passed through unstyled.
func colorizeYAML(src []byte, p Painter) []byte {
	var out bytes.Buffer
	out.Grow(len(src) + len(src)/2)

	// blockIndent >= 0 while inside a literal/folded block scalar whose key sat at
	// that column; its more-indented content lines are emitted as plain data.
	blockIndent := -1
	for len(src) > 0 {
		body, nl := nextLine(&src)
		indent := leadingSpaces(body)
		rest := body[indent:]

		if blockIndent >= 0 {
			if strings.TrimSpace(rest) == "" || indent > blockIndent {
				out.WriteString(body) // literal block-scalar content; leave unstyled
				out.WriteString(nl)
				continue
			}
			blockIndent = -1 // dedent: block ended; parse this line normally
		}

		out.WriteString(body[:indent]) // indentation, verbatim

		if rest == "---" || rest == "..." { // document markers
			out.WriteString(p.Paint(RolePunct, rest))
			out.WriteString(nl)
			continue
		}

		// Sequence markers: "- " (possibly several, for nested inline sequences).
		for strings.HasPrefix(rest, "- ") {
			out.WriteString(p.Paint(RolePunct, "-"))
			out.WriteByte(' ')
			rest = rest[2:]
			indent += 2
		}
		if rest == "-" { // empty sequence element
			out.WriteString(p.Paint(RolePunct, "-"))
			out.WriteString(nl)
			continue
		}

		key, sep, value, ok := splitKeyValue(rest)
		if !ok { // a bare scalar (sequence value or stray content)
			writeYAMLScalar(&out, "", rest, p)
			out.WriteString(nl)
			continue
		}

		if key != "" {
			out.WriteString(p.Paint(RoleKey, key))
		}
		out.WriteString(p.Paint(RolePunct, ":"))
		out.WriteString(sep) // spacing after ':', verbatim
		if isBlockIndicator(value) {
			out.WriteString(p.Paint(RolePunct, value))
			blockIndent = indent
		} else {
			writeYAMLScalar(&out, unquoteKey(key), value, p)
		}
		out.WriteString(nl)
	}
	return out.Bytes()
}

// nextLine splits one line off *src, returning the line without its terminator
// and the terminator itself ("\n" or "" at EOF), advancing *src past both.
func nextLine(src *[]byte) (body, end string) {
	s := *src
	if i := bytes.IndexByte(s, '\n'); i >= 0 {
		*src = s[i+1:]
		return string(s[:i]), "\n"
	}
	*src = nil
	return string(s), ""
}

func leadingSpaces(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}

// splitKeyValue splits "key: value" / "key:" into the key token, the spacing
// after the colon, and the value — all verbatim. ok is false when rest has no
// mapping "key:" separator (i.e. it is a bare scalar). A quoted key is respected
// so a ":" inside it is not mistaken for the separator.
func splitKeyValue(rest string) (key, sep, value string, ok bool) {
	ci := keyColon(rest)
	if ci < 0 {
		return "", "", "", false
	}
	after := rest[ci+1:]
	s := 0
	for s < len(after) && after[s] == ' ' {
		s++
	}
	return rest[:ci], after[:s], after[s:], true
}

// keyColon returns the index of the ":" separating a mapping key from its value
// (a ":" at end-of-line or followed by a space), or -1. A leading quoted key is
// skipped so a ":" inside it isn't taken as the separator.
func keyColon(rest string) int {
	i := 0
	if i < len(rest) && (rest[i] == '"' || rest[i] == '\'') {
		q := rest[i]
		for i++; i < len(rest) && rest[i] != q; i++ {
			if q == '"' && rest[i] == '\\' {
				i++
			}
		}
		if i < len(rest) {
			i++ // closing quote
		}
	}
	for ; i < len(rest); i++ {
		if rest[i] == ':' && (i+1 == len(rest) || rest[i+1] == ' ') {
			return i
		}
	}
	return -1
}

func unquoteKey(key string) string {
	if len(key) >= 2 && (key[0] == '"' || key[0] == '\'') && key[len(key)-1] == key[0] {
		return key[1 : len(key)-1]
	}
	return key
}

// isBlockIndicator reports whether v is a literal/folded block scalar header
// (e.g. "|", ">", "|-", ">+", "|2") rather than a value on the same line.
func isBlockIndicator(v string) bool {
	if v == "" || (v[0] != '|' && v[0] != '>') {
		return false
	}
	for i := 1; i < len(v); i++ {
		if c := v[i]; c != '-' && c != '+' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// writeYAMLScalar paints a single YAML scalar value: quote delimiters dim with
// the content under the key's envelope/string role, null dim, numbers and bools
// accented, empty flow collections dim, and any other bareword as a plain string
// value (left at the terminal default). Every byte of value is emitted exactly.
func writeYAMLScalar(out *bytes.Buffer, keyName, value string, p Painter) {
	if value == "" {
		return
	}
	if value[0] == '"' || value[0] == '\'' {
		role := valueRole(keyName, RoleString)
		if len(value) >= 2 && value[len(value)-1] == value[0] {
			out.WriteString(p.Paint(RolePunct, value[:1]))
			body := value[1 : len(value)-1]
			if value[0] == '"' {
				// Only double-quoted scalars process backslash escapes; single-quoted
				// YAML takes them literally (its sole escape is '' → '), so its body
				// stays one span (still URL-scanned).
				paintYAMLDQBody(out, body, role, p)
			} else {
				paintContent(out, body, role, p)
			}
			out.WriteString(p.Paint(RolePunct, value[len(value)-1:]))
			return
		}
		paintContent(out, value, role, p) // unterminated; one span
		return
	}
	switch value {
	case "null", "~", "Null", "NULL":
		out.WriteString(p.Paint(RoleNull, value))
	case "true", "false", "True", "False", "yes", "no", "Yes", "No", "on", "off", "On", "Off":
		out.WriteString(p.Paint(valueRole(keyName, RoleBool), value))
	case "[]", "{}":
		out.WriteString(p.Paint(RolePunct, value))
	default:
		role := valueRole(keyName, RoleString)
		if isYAMLNumber(value) {
			role = valueRole(keyName, RoleNumber)
		}
		paintContent(out, value, role, p)
	}
}

// paintYAMLDQBody paints the inner content of a double-quoted YAML scalar the
// same way JSON paintString does: control/unicode escape sequences in RoleEscape
// (so a \n stands out as an escape), a literal escape's backslash (\" \\ \/) dim,
// and ordinary content under role. Concatenating the raw bytes reproduces body
// exactly, preserving the strip-to-original invariant.
func paintYAMLDQBody(out *bytes.Buffer, body string, role Role, p Painter) {
	i, n := 0, len(body)
	for i < n {
		if body[i] == '\\' && i+1 < n {
			if end, ok := yamlEscapeEnd(body, i); ok {
				out.WriteString(p.Paint(RoleEscape, body[i:end]))
				i = end
				continue
			}
			out.WriteString(p.Paint(RolePunct, `\`))      // dim the literal escape backslash
			out.WriteString(p.Paint(role, body[i+1:i+2])) // the escaped char stays content
			i += 2
			continue
		}
		j := i + 1
		for j < n && body[j] != '\\' {
			j++
		}
		paintContent(out, body[i:j], role, p)
		i = j
	}
}

// yamlEscapeEnd reports whether body[i:] begins with a YAML double-quoted
// control/unicode escape (the caller has checked body[i]=='\\' and i+1<n),
// returning the index just past it. YAML's double-quoted grammar is richer than
// JSON's: hex escapes \xXX, \uXXXX, \UXXXXXXXX span 2/4/8 digits, and the named
// escapes (\0 \a \b \t \n \v \f \r \e \N \_ \L \P and \<space>) span two. The
// literal escapes \" \\ \/ return ok=false so they keep the dim-backslash
// rendering; a truncated hex escape (unterminated scan) also returns false.
func yamlEscapeEnd(body string, i int) (int, bool) {
	switch body[i+1] {
	case '"', '\\', '/':
		return 0, false
	case 'x':
		return hexEscapeEnd(body, i, 2)
	case 'u':
		return hexEscapeEnd(body, i, 4)
	case 'U':
		return hexEscapeEnd(body, i, 8)
	case '0', 'a', 'b', 't', 'n', 'v', 'f', 'r', 'e', 'N', '_', 'L', 'P', ' ':
		return i + 2, true
	}
	return 0, false
}

// hexEscapeEnd returns the index past a \x/\u/\U escape of the given digit count,
// or ok=false when the token would run past the end of body.
func hexEscapeEnd(body string, i, digits int) (int, bool) {
	end := i + 2 + digits
	if end > len(body) {
		return 0, false
	}
	return end, true
}

func isYAMLNumber(s string) bool {
	i, digits := 0, false
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-':
		default:
			return false
		}
	}
	return digits
}
