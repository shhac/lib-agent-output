package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	cases := map[string]Format{
		"json":    FormatJSON,
		"JSON":    FormatJSON,
		"yaml":    FormatYAML,
		"yml":     FormatYAML,
		"jsonl":   FormatNDJSON,
		"ndjson":  FormatNDJSON,
		" jsonl ": FormatNDJSON,
	}
	for in, want := range cases {
		got, err := ParseFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseFormat("toml"); err == nil {
		t.Error("ParseFormat(toml) should error")
	}
}

func TestResolveFormat(t *testing.T) {
	if got, _ := ResolveFormat("", FormatNDJSON); got != FormatNDJSON {
		t.Errorf("empty flag should use default, got %q", got)
	}
	if got, _ := ResolveFormat("json", FormatNDJSON); got != FormatJSON {
		t.Errorf("flag should win, got %q", got)
	}
	if _, err := ResolveFormat("bogus", FormatJSON); err == nil {
		t.Error("bad flag should error")
	}
}

func TestPrintJSONPrunesAndKeepsHTML(t *testing.T) {
	var buf bytes.Buffer
	in := map[string]any{"url": "https://x/?a=1&b=2", "empty": "", "nested": map[string]any{}}
	if err := PrintJSON(&buf, in, PruneEmpty); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "empty") || strings.Contains(out, "nested") {
		t.Errorf("prune should drop empty fields: %s", out)
	}
	if !strings.Contains(out, "a=1&b=2") {
		t.Errorf("HTML escaping should be off: %s", out)
	}
}

func TestPrintYAMLNeedsRegisteredEncoder(t *testing.T) {
	var buf bytes.Buffer
	// No encoder registered for an unknown format → error.
	if err := Print(&buf, map[string]any{"a": 1}, Format("toml"), nil); err == nil {
		t.Error("Print with no registered encoder should error")
	}

	// A registered encoder is used.
	RegisterEncoder(Format("fake"), func(v any) ([]byte, error) {
		return []byte("ENCODED"), nil
	})
	buf.Reset()
	if err := Print(&buf, map[string]any{"a": 1}, Format("fake"), nil); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "ENCODED" {
		t.Errorf("registered encoder output = %q", buf.String())
	}
}
