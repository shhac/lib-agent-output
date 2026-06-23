package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

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
