package output

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestColorize_DefaultBranchAndMultibyte — the scanner passes through bytes it
// doesn't recognize (the strip-to-original safety net), and multibyte UTF-8
// string content survives strip-to-original end-to-end.
func TestColorize_DefaultBranchAndMultibyte(t *testing.T) {
	src := []byte("\x01{}\x02") // stray control bytes the stdlib encoder never emits
	if got := stripANSI(string(colorizeJSON(src, defaultPainter))); got != string(src) {
		t.Errorf("stray bytes must pass through; strip=%q want=%q", got, src)
	}

	var plain, colored bytes.Buffer
	withColor(t, ColorNever)
	_ = Print(&plain, map[string]any{"s": "世界🎉"}, FormatJSON, nil)
	withColor(t, ColorAlways)
	_ = Print(&colored, map[string]any{"s": "世界🎉"}, FormatJSON, nil)
	if stripANSI(colored.String()) != plain.String() {
		t.Errorf("multibyte strip mismatch:\n got=%q\nwant=%q", stripANSI(colored.String()), plain.String())
	}
}

// TestPaintString_MalformedGuards — the degenerate-token guards in paintString
// and unquote pass through without panicking (the never-hard-fail property).
func TestPaintString_MalformedGuards(t *testing.T) {
	var b bytes.Buffer
	paintString(&b, []byte(`"`), RoleString, defaultPainter) // len 1, < 2
	if got := stripANSI(b.String()); got != `"` {
		t.Errorf("1-byte token should pass through; got %q", got)
	}
	b.Reset()
	paintString(&b, nil, RoleString, defaultPainter) // len 0
	if b.String() != "" {
		t.Errorf("empty token should emit nothing; got %q", b.String())
	}
	if unquote([]byte(`"`)) != `"` || unquote([]byte(`x`)) != `x` {
		t.Error("unquote of a degenerate token should return it as-is")
	}
}

// TestEncode_ErrorBranches — a value that can't be JSON-encoded surfaces an error
// rather than panicking, on both the colored path and the registered-encoder path.
func TestEncode_ErrorBranches(t *testing.T) {
	withColor(t, ColorAlways)
	if err := Print(io.Discard, map[string]any{"c": make(chan int)}, FormatJSON, nil); err == nil {
		t.Error("expected an encode error for a chan value on the colored path")
	}

	RegisterEncoder(Format("boom"), func(any) ([]byte, error) { return nil, errors.New("nope") })
	if err := Print(io.Discard, map[string]any{"a": 1}, Format("boom"), nil); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("expected propagated encoder error; got %v", err)
	}
}

// TestApplyPrune_MarshalFailureFallback — when the JSON round-trip fails, the
// pruner still runs on the raw value rather than silently skipping pruning.
func TestApplyPrune_MarshalFailureFallback(t *testing.T) {
	called := false
	applyPrune(map[string]any{"c": make(chan int)}, func(v any) any { called = true; return v })
	if !called {
		t.Error("pruner should still run on the raw value when marshal fails")
	}
}

// TestIsEmpty_Collections — the empty-collection boundary, asserted directly.
func TestIsEmpty_Collections(t *testing.T) {
	if !isEmpty([]any{}) || !isEmpty(map[string]any{}) {
		t.Error("empty slice and empty map should be empty")
	}
	if isEmpty([]any{1}) {
		t.Error("non-empty slice should not be empty")
	}
}

// TestError_StringAndNewf — the error-interface method and the printf constructor
// (the public error contract used by errors.As / %w consumers).
func TestError_StringAndNewf(t *testing.T) {
	if New("boom", FixableByAgent).Error() != "boom" {
		t.Error("Error() should return the message")
	}
	if e := Newf(FixableByRetry, "n=%d", 5); e.Message != "n=5" || e.FixableBy != FixableByRetry {
		t.Errorf("Newf produced %+v", e)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write fail") }

// TestWriteList_WriteErrorPropagates — WriteList surfaces a downstream writer
// error (and the prune branch of pruneItems runs).
func TestWriteList_WriteErrorPropagates(t *testing.T) {
	withColor(t, ColorNever)
	err := WriteList(errWriter{}, FormatNDJSON, []any{map[string]any{"a": 1}}, nil, PruneEmpty)
	if err == nil {
		t.Error("WriteList should propagate a writer error")
	}
}

// TestWriteList_MetaWriteErrorPropagates — with no items, the meta-line loop is
// reached first, so this covers the meta write-error return arm specifically.
func TestWriteList_MetaWriteErrorPropagates(t *testing.T) {
	withColor(t, ColorNever)
	meta := map[string]any{MetaKeyPagination: Pagination{HasMore: true}}
	if err := WriteList(errWriter{}, FormatNDJSON, nil, meta, nil); err == nil {
		t.Error("WriteList should propagate a meta-line writer error")
	}
}
