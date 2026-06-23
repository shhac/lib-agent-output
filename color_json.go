package output

import "bytes"

// colorizeJSON walks already-encoded JSON bytes and wraps each token in its
// Painter style, passing whitespace (including indentation and the trailing
// newline) through untouched. Because it only inserts escapes around tokens,
// stripping the escapes reproduces src exactly. It tracks the most recent object
// key so a value of a known envelope field gets a semantic Role.
//
// src is always the output of encoding/json (canonical, well-formed), so this is
// a lightweight re-tokenizer, not a general JSON lexer — the byte classifiers can
// be forgiving and the default arm simply passes unrecognized bytes through.
func colorizeJSON(src []byte, p Painter) []byte {
	var out bytes.Buffer
	out.Grow(len(src) + len(src)/2)
	lastKey := ""
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		case isJSONSpace(c):
			out.WriteByte(c)
			i++
		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			out.WriteString(p.Paint(RolePunct, string(c)))
			i++
		case c == '"':
			j := i + 1
			for j < n {
				if src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == '"' {
					j++
					break
				}
				j++
			}
			if j > n { // a trailing backslash can push j one past EOF
				j = n
			}
			tok := src[i:j]
			k := j
			for k < n && isJSONSpace(src[k]) {
				k++
			}
			if k < n && src[k] == ':' { // a key
				paintString(&out, tok, RoleKey, p)
				lastKey = unquote(tok)
			} else { // a string value
				paintString(&out, tok, valueRole(lastKey, RoleString), p)
				lastKey = ""
			}
			i = j
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < n && isNumByte(src[j]) {
				j++
			}
			out.WriteString(p.Paint(valueRole(lastKey, RoleNumber), string(src[i:j])))
			lastKey = ""
			i = j
		case c == 't' || c == 'f' || c == 'n': // true / false / null
			j := i
			for j < n && src[j] >= 'a' && src[j] <= 'z' {
				j++
			}
			role := RoleNull
			if c != 'n' {
				role = valueRole(lastKey, RoleBool)
			}
			out.WriteString(p.Paint(role, string(src[i:j])))
			lastKey = ""
			i = j
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
}

// paintString renders a quoted JSON string token (including its surrounding
// quotes) so the delimiters are scaffolding, not data: the opening/closing
// quotes and each escape's backslash get the dim punctuation style, while the
// inner content carries contentRole. Concatenating the raw (un-styled) bytes
// reproduces tok exactly, preserving the strip-to-original invariant.
func paintString(out *bytes.Buffer, tok []byte, contentRole Role, p Painter) {
	if len(tok) == 0 || tok[0] != '"' { // not a quoted token; emit as-is
		out.WriteString(p.Paint(contentRole, string(tok)))
		return
	}
	out.WriteString(p.Paint(RolePunct, `"`)) // opening quote
	// A well-formed token ends with the closing quote; a forgiving scan to EOF
	// (unterminated string) does not — then its last byte is content, not a
	// delimiter, and must not be dropped.
	body := tok[1:]
	closed := len(body) > 0 && body[len(body)-1] == '"'
	if closed {
		body = body[:len(body)-1]
	}
	i, n := 0, len(body)
	for i < n {
		if body[i] == '\\' && i+1 < n {
			out.WriteString(p.Paint(RolePunct, `\`))                     // dim the escape backslash
			out.WriteString(p.Paint(contentRole, string(body[i+1:i+2]))) // the escaped char stays content (byte-exact, not rune-cast)
			i += 2
			continue
		}
		// Consume body[i] (a content byte, or a trailing lone '\') then run to
		// the next escape. Starting at i+1 guarantees forward progress.
		j := i + 1
		for j < n && body[j] != '\\' {
			j++
		}
		out.WriteString(p.Paint(contentRole, string(body[i:j])))
		i = j
	}
	if closed {
		out.WriteString(p.Paint(RolePunct, `"`)) // closing quote
	}
}

func unquote(b []byte) string {
	if len(b) >= 2 {
		return string(b[1 : len(b)-1])
	}
	return string(b)
}

func isNumByte(c byte) bool {
	return (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E'
}

// isJSONSpace reports the ASCII whitespace encoding/json emits between tokens.
// Shared by the token loop and the key-vs-value lookahead so the two stay in
// sync — a mismatch would misclassify keys.
func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\t' || c == '\r'
}
