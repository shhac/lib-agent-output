package output

import (
	"bytes"
	"testing"
)

// The load-bearing safety property of the colorizers is byte-identity: stripping
// the ANSI escapes from the colored output must reproduce the input exactly, so
// a machine consumer that strips color reads the original bytes. The colorizers
// are hand-rolled re-tokenizers where an unconsidered input shape could silently
// drop or duplicate a byte; these fuzz tests assert the invariant over arbitrary
// input, complementing the fixed examples in color_test.go.
//
// Inputs containing a raw ESC (0x1b) are skipped: stripANSI would also strip an
// escape that was part of the input, breaking the comparison. Canonical JSON/YAML
// (the real inputs, from encoders) never contains a raw ESC — JSON escapes it as
//  — so this excludes only inputs the colorizers never actually see.

func FuzzColorizeJSON(f *testing.F) {
	for _, s := range []string{
		`{}`, `[]`, `"x"`, `42`, `true`, `null`,
		`{"a":1,"b":true,"c":null,"d":-3.5e10}`,
		`{"error":"boom","fixable_by":"agent","hint":"do x","notice":"n"}`,
		`[1,2.5,-3,"s\n\"q\\\t",{"k":[null,false]}]`,
		"{\n  \"nested\": {\n    \"k\": \"v\"\n  }\n}\n",
		`{"unicode":"é πφ"}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if bytes.IndexByte(b, 0x1b) >= 0 {
			return
		}
		if got := stripANSI(string(colorizeJSON(b, defaultPainter))); got != string(b) {
			t.Errorf("strip-to-original violated:\n in = %q\nout = %q", b, got)
		}
	})
}

func FuzzColorizeYAML(f *testing.F) {
	for _, s := range []string{
		"a: 1\n", "name: widget\ncount: 3\n",
		"error: boom\nfixable_by: agent\n",
		"items:\n  - one\n  - two\n",
		"nested:\n  key: value\n  num: 42\n",
		"quoted: \"a: b\"\n'k': v\n",
		"block: |\n  line one\n  line two\nnext: x\n",
		"---\ndoc: marker\n...\n",
		"empty: {}\nlist: []\nnull_val: ~\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if bytes.IndexByte(b, 0x1b) >= 0 {
			return
		}
		if got := stripANSI(string(colorizeYAML(b, defaultPainter))); got != string(b) {
			t.Errorf("strip-to-original violated:\n in = %q\nout = %q", b, got)
		}
	})
}
