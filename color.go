package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Color is opt-in visual styling layered onto the family's structured output.
// It is purely cosmetic: when enabled, ANSI escapes are inserted around the
// tokens of the canonical JSON/NDJSON bytes; when disabled, output is
// byte-identical to the uncolored encoder. That invariant is what keeps colored
// output safe for machine consumers — stripping the escapes yields the exact
// original bytes, and the disabled path never runs the colorizer at all.
//
// Three concerns compose, separated so each can change without touching the
// others:
//   - the decision — WHETHER to color a given stream: configured at runtime via
//     SetColorMode (the --color flag) and SetTerminalDetector (isatty injection).
//   - the Painter — HOW a token's Role is styled: a code-level seam. The
//     colorizer depends only on the Painter interface, so swapping the mechanism
//     (ANSI → a styling library, truecolor, HTML) is a one-type change with no
//     caller impact. There is deliberately no runtime SetPainter — there is one
//     painter today; add an injector when a second one actually exists.
//   - the theme — the Role→style map the ANSI painter consults.

// Role is the semantic class of a JSON token. It is contract-aware, not merely
// syntactic: values of the known envelope fields (error/fixable_by/hint/notice)
// get their own roles so they can be emphasised distinctly from ordinary data.
type Role int

const (
	RolePunct   Role = iota // structural punctuation: { } [ ] , :
	RoleKey                 // an object key
	RoleString              // an ordinary string value
	RoleNumber              // a numeric value
	RoleBool                // true / false
	RoleNull                // null
	RoleError               // the value of an "error" field
	RoleFixable             // the value of a "fixable_by" field
	RoleHint                // the value of a "hint" field
	RoleNotice              // the value of a "notice" field
)

// Painter turns a token and its Role into styled bytes. This is the swappable
// mechanism seam: the colorizer depends only on this interface and never on how
// the style is actually applied.
type Painter interface {
	Paint(role Role, s string) string
}

const (
	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[2m"
	ansiBoldRed = "\x1b[1;31m"
	ansiYellow  = "\x1b[33m"
	ansiCyan    = "\x1b[36m"
	ansiMagenta = "\x1b[35m"
)

// defaultTheme dims the scaffolding (punctuation, keys) so ordinary string
// values — left unstyled, i.e. the terminal's default foreground — stand out as
// "the data". Numbers/bools/null get muted accents, and the known envelope
// fields get semantic emphasis (error in bold red, fixable_by/hint in yellow,
// notice in cyan). An empty style means "leave the token unstyled".
var defaultTheme = map[Role]string{
	RolePunct:   ansiDim,
	RoleKey:     ansiDim,
	RoleString:  "",
	RoleNumber:  ansiCyan,
	RoleBool:    ansiMagenta,
	RoleNull:    ansiDim,
	RoleError:   ansiBoldRed,
	RoleFixable: ansiYellow,
	RoleHint:    ansiYellow,
	RoleNotice:  ansiCyan,
}

type ansiPainter struct{ theme map[Role]string }

func (p ansiPainter) Paint(role Role, s string) string {
	style, ok := p.theme[role]
	if !ok || style == "" {
		return s
	}
	return style + s + ansiReset
}

// defaultPainter is the ANSI painter used when color is enabled.
var defaultPainter Painter = ansiPainter{theme: defaultTheme}

// ColorMode selects when output is colorized.
type ColorMode int

const (
	ColorAuto   ColorMode = iota // color iff the target stream is a terminal (and NO_COLOR/TERM=dumb don't forbid it)
	ColorAlways                  // always color, even when piped
	ColorNever                   // never color
)

// ParseColorMode resolves "auto"/"always"/"never" (case-insensitive; empty →
// auto). Unknown values are FixableByAgent, mirroring ParseFormat.
func ParseColorMode(s string) (ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return ColorAuto, nil
	case "always":
		return ColorAlways, nil
	case "never":
		return ColorNever, nil
	default:
		return ColorAuto, New(fmt.Sprintf("unknown color mode %q, expected one of: auto, always, never", s), FixableByAgent)
	}
}

var (
	colorMu   sync.RWMutex
	colorMode = ColorAuto
	// terminalDetector reports whether w is a terminal. The default reports false
	// — so lib-agent-output stays dependency-free and defaults to no color (safe
	// for machine consumers). lib-agent-cli injects a real isatty-based detector
	// via SetTerminalDetector, exactly as RegisterEncoder injects YAML.
	terminalDetector = func(io.Writer) bool { return false }
)

// SetColorMode sets the process-wide color mode (typically from a --color flag).
func SetColorMode(m ColorMode) {
	colorMu.Lock()
	colorMode = m
	colorMu.Unlock()
}

// SetTerminalDetector injects the terminal check used by ColorAuto. Pass a
// function that reports whether a writer targets a TTY (e.g. an isatty check on
// *os.File). Resolution is per-writer, so stdout and stderr decide independently.
func SetTerminalDetector(fn func(io.Writer) bool) {
	if fn == nil {
		fn = func(io.Writer) bool { return false }
	}
	colorMu.Lock()
	terminalDetector = fn
	colorMu.Unlock()
}

// painterFor returns the Painter to use when writing to w, or nil when color is
// off. In auto mode a present NO_COLOR or TERM=dumb forces it off; otherwise the
// injected detector decides for that specific stream.
func painterFor(w io.Writer) Painter {
	colorMu.RLock()
	mode := colorMode
	detect := terminalDetector
	colorMu.RUnlock()

	switch mode {
	case ColorNever:
		return nil
	case ColorAlways:
		return defaultPainter
	default: // ColorAuto
		if _, ok := os.LookupEnv("NO_COLOR"); ok {
			return nil
		}
		if strings.EqualFold(os.Getenv("TERM"), "dumb") {
			return nil
		}
		if detect(w) {
			return defaultPainter
		}
		return nil
	}
}

// Enabled reports whether color should be applied when writing to w, under the
// current mode and the per-stream rules (NO_COLOR / TERM=dumb / the injected
// terminal detector). It is the single predicate a CLI's own non-JSON renderer
// (e.g. a human transcript or a pretty card) should consult, instead of
// re-implementing the decision — so every surface in the family colors
// consistently. JSON/NDJSON output is handled automatically by the package.
func Enabled(w io.Writer) bool {
	return painterFor(w) != nil
}

// encodeJSON is the single internal funnel for the native JSON/NDJSON formats.
// When color is off it writes the canonical bytes directly (byte-identical to
// the bare encoder). When on, it renders those exact bytes to a buffer and
// inserts ANSI around their tokens, so the colored form differs from the plain
// form by escapes only.
func encodeJSON(w io.Writer, v any, pretty bool) error {
	p := painterFor(w)
	if p == nil {
		enc := newEncoder(w)
		if pretty {
			enc.SetIndent("", "  ")
		}
		return enc.Encode(v)
	}
	var buf bytes.Buffer
	enc := newEncoder(&buf)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(colorizeJSON(buf.Bytes(), p))
	return err
}

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
		case c == ' ' || c == '\n' || c == '\t' || c == '\r':
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
			tok := src[i:j]
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\n' || src[k] == '\t' || src[k] == '\r') {
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
	if len(tok) < 2 { // malformed; emit as-is under the content role
		out.WriteString(p.Paint(contentRole, string(tok)))
		return
	}
	out.WriteString(p.Paint(RolePunct, `"`)) // opening quote
	body := tok[1 : len(tok)-1]
	i, n := 0, len(body)
	for i < n {
		if body[i] == '\\' && i+1 < n {
			out.WriteString(p.Paint(RolePunct, `\`))                 // dim the escape backslash
			out.WriteString(p.Paint(contentRole, string(body[i+1]))) // the escaped char stays content
			i += 2
			continue
		}
		j := i
		for j < n && body[j] != '\\' {
			j++
		}
		out.WriteString(p.Paint(contentRole, string(body[i:j])))
		i = j
	}
	out.WriteString(p.Paint(RolePunct, `"`)) // closing quote
}

// valueRole maps a known envelope field name to its semantic Role, falling back
// to def for ordinary data.
func valueRole(key string, def Role) Role {
	switch key {
	case "error":
		return RoleError
	case "fixable_by":
		return RoleFixable
	case "hint":
		return RoleHint
	case "notice":
		return RoleNotice
	}
	return def
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
			out.WriteString(p.Paint(role, value[1:len(value)-1]))
			out.WriteString(p.Paint(RolePunct, value[len(value)-1:]))
			return
		}
		out.WriteString(p.Paint(role, value)) // unterminated; one span
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
		out.WriteString(p.Paint(role, value))
	}
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
