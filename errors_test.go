package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestFixableByStatus encodes the mapping the agent-* family converged on
// independently (verified across vercel, cloudflare, dd, incident, stripe,
// posthog). If this table changes, the shared classifier has drifted from the
// family — that's a deliberate decision, not a casual edit.
func TestFixableByStatus(t *testing.T) {
	cases := map[int]FixableBy{
		400: FixableByAgent,
		401: FixableByHuman,
		402: FixableByHuman,
		403: FixableByHuman,
		404: FixableByAgent,
		409: FixableByAgent,
		422: FixableByAgent,
		429: FixableByRetry,
		500: FixableByRetry,
		502: FixableByRetry,
		503: FixableByRetry,
	}
	for status, want := range cases {
		if got := FixableByStatus(status); got != want {
			t.Errorf("FixableByStatus(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestWithHintsJoinsNonEmpty(t *testing.T) {
	e := New("boom", FixableByAgent).WithHints("first", "", "   ", "second")
	if e.Hint != "first; second" {
		t.Errorf("Hint = %q, want %q", e.Hint, "first; second")
	}
}

func TestWithCauseUnwraps(t *testing.T) {
	cause := bytes.ErrTooLarge
	e := New("boom", FixableByRetry).WithCause(cause)
	if e.Unwrap() != cause {
		t.Error("WithCause should set the unwrappable cause")
	}
}

func TestWithRetryAfterSerializes(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, New("rate limited", FixableByRetry).WithRetryAfter(30*time.Second))
	m := decodeLine(t, buf.Bytes())
	if m["retry_after_seconds"] != float64(30) {
		t.Errorf("retry_after_seconds = %v, want 30", m["retry_after_seconds"])
	}
	if m["fixable_by"] != "retry" {
		t.Errorf("fixable_by = %v, want retry", m["fixable_by"])
	}
}

func TestWriteErrorKeyOrder(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, New("boom", FixableByHuman).WithHint("do the thing"))
	s := buf.String()
	// Human-readable order: classification before the variable-length hint.
	if strings.Index(s, `"fixable_by"`) > strings.Index(s, `"hint"`) {
		t.Errorf("fixable_by should precede hint: %s", s)
	}
	if strings.Index(s, `"error"`) > strings.Index(s, `"fixable_by"`) {
		t.Errorf("error should precede fixable_by: %s", s)
	}
}

func TestWithRetryAfterRoundsAndOmits(t *testing.T) {
	// Sub-second rounds down to 0 and is omitted (no default imposed).
	var buf bytes.Buffer
	WriteError(&buf, New("oops", FixableByAgent).WithRetryAfter(400*time.Millisecond))
	if _, ok := decodeLine(t, buf.Bytes())["retry_after_seconds"]; ok {
		t.Error("retry_after_seconds should be omitted when it rounds to 0")
	}
}
