package output

import (
	"bytes"
	"io"
	"os"
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

// TestColor_Enabled — the public predicate CLIs use for their own renderers.
func TestColor_Enabled(t *testing.T) {
	var tty, pipe bytes.Buffer

	withColor(t, ColorAuto, &tty)
	if !Enabled(&tty) {
		t.Error("auto + terminal: want enabled")
	}
	if Enabled(&pipe) {
		t.Error("auto + non-terminal: want disabled")
	}

	withColor(t, ColorAlways)
	if !Enabled(&pipe) {
		t.Error("always: want enabled even for a pipe")
	}

	withColor(t, ColorNever, &tty)
	if Enabled(&tty) {
		t.Error("never: want disabled even for a terminal")
	}
}

// TestColor_NDJSONWriter — the streaming writer colorizes each line and strips
// back to the plain stream.
func TestColor_NDJSONWriter(t *testing.T) {
	write := func() string {
		var b bytes.Buffer
		w := NewNDJSONWriter(&b)
		_ = w.WriteItem(map[string]any{"id": "A", "n": 1})
		_ = w.WriteMetaLine(MetaKeyPagination, Pagination{HasMore: true})
		return b.String()
	}
	withColor(t, ColorNever)
	plain := write()
	withColor(t, ColorAlways)
	colored := write()

	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("expected ANSI in colored NDJSON")
	}
	if stripANSI(colored) != plain {
		t.Errorf("stripped NDJSON != plain\n got=%q\nwant=%q", stripANSI(colored), plain)
	}
}

// TestColor_EnvPrecedence — NO_COLOR and TERM=dumb force auto off, but explicit
// --color always overrides NO_COLOR.
func TestColor_EnvPrecedence(t *testing.T) {
	var tty bytes.Buffer

	t.Run("NO_COLOR disables auto", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		withColor(t, ColorAuto, &tty)
		if Enabled(&tty) {
			t.Error("NO_COLOR should disable auto")
		}
	})

	t.Run("always overrides NO_COLOR", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		withColor(t, ColorAlways, &tty)
		if !Enabled(&tty) {
			t.Error("--color always should override NO_COLOR")
		}
	})

	t.Run("TERM=dumb disables auto", func(t *testing.T) {
		// Isolate from an inherited NO_COLOR so we exercise the TERM branch.
		if v, ok := os.LookupEnv("NO_COLOR"); ok {
			os.Unsetenv("NO_COLOR")
			t.Cleanup(func() { os.Setenv("NO_COLOR", v) })
		}
		t.Setenv("TERM", "dumb")
		withColor(t, ColorAuto, &tty)
		if Enabled(&tty) {
			t.Error("TERM=dumb should disable auto")
		}
	})
}

// TestColor_Roles — scalar and semantic tokens get their themed styles.
func TestColor_Roles(t *testing.T) {
	withColor(t, ColorAlways)

	var buf bytes.Buffer
	if err := Print(&buf, map[string]any{"n": 42, "ok": true, "x": nil}, FormatJSON, nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{ansiCyan + "42" + ansiReset, ansiMagenta + "true" + ansiReset, ansiDim + "null" + ansiReset} {
		if !strings.Contains(out, want) {
			t.Errorf("scalar role missing %q in %q", want, out)
		}
	}

	// notice (cyan) + hint (yellow) on the stderr notice envelope.
	var nb bytes.Buffer
	WriteNotice(&nb, "heads up", "do the thing")
	nout := nb.String()
	if !strings.Contains(nout, ansiCyan+"heads up"+ansiReset) {
		t.Errorf("notice value should be cyan; got %q", nout)
	}
	if !strings.Contains(nout, ansiYellow+"do the thing"+ansiReset) {
		t.Errorf("hint value should be yellow; got %q", nout)
	}
}

// TestColor_YAMLColorize — YAML structure (keys, ":", "-", quote delimiters) is
// dimmed; scalars take their value roles; the envelope fields keep their
// semantics; and stripping the escapes reproduces the source exactly.
func TestColor_YAMLColorize(t *testing.T) {
	doc := "error: boom\n" +
		"fixable_by: agent\n" +
		"count: 4\n" +
		"ratio: 1.5\n" +
		"enabled: true\n" +
		"missing: null\n" +
		"label: 'a: b'\n" +
		"items:\n" +
		"    - id: w-1\n" +
		"      tags:\n" +
		"        - x\n" +
		"        - y\n"

	out := string(colorizeYAML([]byte(doc), defaultPainter))

	if stripANSI(out) != doc {
		t.Fatalf("stripped YAML != source\n got=%q\nwant=%q", stripANSI(out), doc)
	}
	for name, want := range map[string]string{
		"key dim":        ansiDim + "count" + ansiReset,
		"colon dim":      ansiDim + ":" + ansiReset,
		"dash dim":       ansiDim + "-" + ansiReset,
		"quote dim":      ansiDim + "'" + ansiReset,
		"error boldRed":  ansiBoldRed + "boom" + ansiReset,
		"fixable yellow": ansiYellow + "agent" + ansiReset,
		"int cyan":       ansiCyan + "4" + ansiReset,
		"float cyan":     ansiCyan + "1.5" + ansiReset,
		"bool magenta":   ansiMagenta + "true" + ansiReset,
		"null dim":       ansiDim + "null" + ansiReset,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%s: missing %q in:\n%s", name, want, out)
		}
	}
	// An ordinary string value is left at the terminal default (no escapes).
	if strings.Contains(out, ansiReset+"w-1") || strings.Contains(out, "[mw-1") {
		t.Errorf("plain string value w-1 should be unstyled:\n%s", out)
	}
}

// TestColor_YAMLViaPrint — Print colorizes FormatYAML output when color is on,
// is byte-identical when off, and is strip-reversible either way.
func TestColor_YAMLViaPrint(t *testing.T) {
	// Register a YAML encoder for this test only, restoring the (unregistered)
	// state afterwards so it can't leak into tests that rely on it being absent.
	encodersMu.Lock()
	prev, had := encoders[FormatYAML]
	encodersMu.Unlock()
	t.Cleanup(func() {
		encodersMu.Lock()
		if had {
			encoders[FormatYAML] = prev
		} else {
			delete(encoders, FormatYAML)
		}
		encodersMu.Unlock()
	})

	yamlBytes := []byte("name: Alice\ncount: 3\nactive: true\n")
	RegisterEncoder(FormatYAML, func(any) ([]byte, error) { return yamlBytes, nil })

	var plain bytes.Buffer
	withColor(t, ColorNever)
	if err := Print(&plain, map[string]any{"x": 1}, FormatYAML, nil); err != nil {
		t.Fatal(err)
	}
	if plain.String() != string(yamlBytes) {
		t.Errorf("color off must be byte-identical: %q", plain.String())
	}

	var colored bytes.Buffer
	withColor(t, ColorAlways)
	if err := Print(&colored, map[string]any{"x": 1}, FormatYAML, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatal("expected ANSI escapes for YAML under ColorAlways")
	}
	if stripANSI(colored.String()) != string(yamlBytes) {
		t.Errorf("stripped YAML != plain\n got=%q\nwant=%q", stripANSI(colored.String()), string(yamlBytes))
	}
}

// TestColor_YAMLStripInvariantEdgeCases — the strip-to-original guarantee must
// hold for the gnarly shapes a re-tokenizer can mangle: block scalars (whose
// content must not be reparsed as mappings), empty flow collections, deep
// nesting, document markers, and a value containing an inner ": ".
func TestColor_YAMLStripInvariantEdgeCases(t *testing.T) {
	docs := []string{
		"description: |-\n    multi line\n    key: not-a-key here\nnext: 1\n",
		"folded: >\n    wrapped text\n    more text\n",
		"empty_map: {}\nempty_list: []\n",
		"---\nname: doc\n...\n",
		"url: http://example.test/a?b=c\nlabel: 'a: b'\n",
		"deep:\n    a:\n        b:\n            - 1\n            - 2\n",
		"",
		"plain scalar with no newline",
	}
	for _, doc := range docs {
		got := stripANSI(string(colorizeYAML([]byte(doc), defaultPainter)))
		if got != doc {
			t.Errorf("strip != source\n got=%q\nwant=%q", got, doc)
		}
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
