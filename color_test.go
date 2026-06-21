package output

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// withColor sets mode + a detector that treats the given writers as terminals,
// restoring both afterwards.
func withColor(t *testing.T, mode ColorMode, terminals ...io.Writer) {
	t.Helper()
	colorMu.Lock()
	prevMode, prevDetect := colorMode, terminalDetector
	colorMu.Unlock()
	t.Cleanup(func() {
		colorMu.Lock()
		colorMode, terminalDetector = prevMode, prevDetect
		colorMu.Unlock()
	})
	SetColorMode(mode)
	SetTerminalDetector(func(w io.Writer) bool {
		for _, tw := range terminals {
			if tw == w {
				return true
			}
		}
		return false
	})
}

// TestColor_OffIsByteIdentical — the disabled path must never alter output.
func TestColor_OffIsByteIdentical(t *testing.T) {
	data := map[string]any{"name": "Alice", "age": 30, "tags": []any{"a", "b"}, "active": true, "bio": nil}

	for _, mode := range []ColorMode{ColorNever, ColorAuto} {
		withColor(t, mode) // no writer registered as a terminal → off
		for _, f := range []Format{FormatJSON, FormatNDJSON} {
			var plain bytes.Buffer
			// Reference: the bare encoder used everywhere else.
			enc := newEncoder(&plain)
			if f == FormatJSON {
				enc.SetIndent("", "  ")
			}
			if err := enc.Encode(map[string]any{"name": "Alice", "age": 30, "tags": []any{"a", "b"}, "active": true, "bio": nil}); err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			if err := Print(&got, data, f, nil); err != nil {
				t.Fatal(err)
			}
			if got.String() != plain.String() {
				t.Errorf("mode=%v format=%s: output differs from bare encoder\n got=%q\nwant=%q", mode, f, got.String(), plain.String())
			}
			if strings.Contains(got.String(), "\x1b[") {
				t.Errorf("mode=%v format=%s: emitted ANSI while off: %q", mode, f, got.String())
			}
		}
	}
}

// TestColor_OnStripsToOriginal — colored output must equal the plain output once
// the escapes are removed (color is purely additive).
func TestColor_OnStripsToOriginal(t *testing.T) {
	data := map[string]any{"name": "Alice", "url": "https://x.test/a?b=c&d=e", "n": 42, "ok": true, "x": nil}

	var plain bytes.Buffer
	withColor(t, ColorNever)
	if err := Print(&plain, data, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}

	var colored bytes.Buffer
	withColor(t, ColorAlways)
	if err := Print(&colored, data, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatal("expected ANSI escapes when ColorAlways")
	}
	if stripANSI(colored.String()) != plain.String() {
		t.Errorf("stripped colored output != plain\n got=%q\nwant=%q", stripANSI(colored.String()), plain.String())
	}
}

// TestColor_PerStreamAuto — auto resolves each writer independently: a writer
// the detector calls a terminal gets color; another does not.
func TestColor_PerStreamAuto(t *testing.T) {
	var tty, pipe bytes.Buffer
	withColor(t, ColorAuto, &tty)

	if err := Print(&tty, map[string]any{"a": 1}, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	if err := Print(&pipe, map[string]any{"a": 1}, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tty.String(), "\x1b[") {
		t.Errorf("tty writer should be colored: %q", tty.String())
	}
	if strings.Contains(pipe.String(), "\x1b[") {
		t.Errorf("pipe writer must stay clean: %q", pipe.String())
	}
}

// TestColor_NoColorEnvForcesOff — auto honors NO_COLOR even on a terminal.
func TestColor_NoColorEnvForcesOff(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var tty bytes.Buffer
	withColor(t, ColorAuto, &tty)
	if err := Print(&tty, map[string]any{"a": 1}, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tty.String(), "\x1b[") {
		t.Errorf("NO_COLOR must suppress color: %q", tty.String())
	}
}

// TestColor_ErrorEnvelopeSemantics — the error value is painted with the error
// role (bold red), proving contract-aware emphasis on stderr.
func TestColor_ErrorEnvelopeSemantics(t *testing.T) {
	var buf bytes.Buffer
	withColor(t, ColorAlways)
	WriteError(&buf, New("boom", FixableByAgent))
	out := buf.String()
	// The message content is bold-red; the surrounding quotes are dim punctuation.
	if !strings.Contains(out, ansiBoldRed+"boom"+ansiReset) {
		t.Errorf("error value content should be bold-red painted; got %q", out)
	}
	if !strings.Contains(out, ansiDim+`"`+ansiReset+ansiBoldRed+"boom") {
		t.Errorf("opening quote of the error value should be dim; got %q", out)
	}
	if stripANSI(out) != `{"error":"boom","fixable_by":"agent"}`+"\n" {
		t.Errorf("stripped error envelope changed: %q", stripANSI(out))
	}
}

// TestColor_StringDelimitersAndEscapes — the quotes and an escape's backslash are
// dim punctuation; the content keeps its value style; stripping yields the
// original.
func TestColor_StringDelimitersAndEscapes(t *testing.T) {
	val := `a"b\c` // JSON-encodes to "a\"b\\c"
	withColor(t, ColorAlways)
	var buf bytes.Buffer
	if err := Print(&buf, map[string]any{"k": val}, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, ansiDim+`\`+ansiReset) {
		t.Errorf("escape backslash should be dim punctuation; got %q", out)
	}

	var plain bytes.Buffer
	withColor(t, ColorNever)
	if err := Print(&plain, map[string]any{"k": val}, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	if stripANSI(out) != plain.String() {
		t.Errorf("stripped colored != plain\n got=%q\nwant=%q", stripANSI(out), plain.String())
	}
}

func TestParseColorMode(t *testing.T) {
	for in, want := range map[string]ColorMode{"": ColorAuto, "auto": ColorAuto, "ALWAYS": ColorAlways, "never": ColorNever} {
		got, err := ParseColorMode(in)
		if err != nil || got != want {
			t.Errorf("ParseColorMode(%q)=%v,%v; want %v,nil", in, got, err, want)
		}
	}
	if _, err := ParseColorMode("rainbow"); err == nil {
		t.Error("expected error for unknown mode")
	}
}
