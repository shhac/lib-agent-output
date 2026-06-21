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
// Three pieces compose, each swappable independently:
//   - the decision (ColorMode + the injected terminal detector) → which Painter
//   - the Painter → HOW a token's Role is styled (the mechanism seam)
//   - the theme → the Role→style map a Painter consults
//
// To swap the styling mechanism (e.g. ANSI → a styling library, truecolor, or
// HTML), implement a new Painter; the colorizer and every caller are unchanged.

// Role is the semantic class of a JSON token. It is contract-aware, not merely
// syntactic: values of the known envelope fields (error/fixable_by/hint/notice)
// get their own roles so they can be emphasised distinctly from ordinary data.
type Role int

const (
	RolePunct  Role = iota // structural punctuation: { } [ ] , :
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
			tok := string(src[i:j])
			k := j
			for k < n && (src[k] == ' ' || src[k] == '\n' || src[k] == '\t' || src[k] == '\r') {
				k++
			}
			if k < n && src[k] == ':' { // a key
				out.WriteString(p.Paint(RoleKey, tok))
				lastKey = unquote(src[i:j])
			} else { // a string value
				out.WriteString(p.Paint(valueRole(lastKey, RoleString), tok))
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
		case c == 't' || c == 'f':
			j := i
			for j < n && src[j] >= 'a' && src[j] <= 'z' {
				j++
			}
			out.WriteString(p.Paint(valueRole(lastKey, RoleBool), string(src[i:j])))
			lastKey = ""
			i = j
		case c == 'n':
			j := i
			for j < n && src[j] >= 'a' && src[j] <= 'z' {
				j++
			}
			out.WriteString(p.Paint(RoleNull, string(src[i:j])))
			lastKey = ""
			i = j
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
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
