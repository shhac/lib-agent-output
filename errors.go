package output

import (
	stderrors "errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// FixableBy classifies who can resolve an error, so a calling agent knows
// whether to retry, fix its own input, or defer to a human.
type FixableBy string

const (
	// FixableByAgent means the caller supplied bad input (bad args, flags, or
	// target) and can correct it and retry — typically 4xx-class validation.
	FixableByAgent FixableBy = "agent"
	// FixableByHuman means a person must act: auth, permissions, payment, or an
	// explicit confirmation the agent must not self-grant.
	FixableByHuman FixableBy = "human"
	// FixableByRetry means the failure is transient (429/5xx/network) and the
	// same call may succeed if retried with backoff.
	FixableByRetry FixableBy = "retry"
)

// FixableByStatus classifies an HTTP status code, following the convention the
// agent-* family converged on independently: 401/402/403 need a human (auth,
// permission, payment); 429 and 5xx are transient and retryable; everything
// else (404, other 4xx, bad input) is the agent's to fix. A CLI that needs a
// different call for a specific code can branch before or after this default
// (e.g. promoting a vendor "authentication_error" body to FixableByHuman).
//
// Network, timeout, and context errors are not status-coded — classify those
// directly, e.g. output.Wrap(err, output.FixableByRetry).
func FixableByStatus(status int) FixableBy {
	switch {
	case status == 429 || status >= 500:
		return FixableByRetry
	case status == 401 || status == 402 || status == 403:
		return FixableByHuman
	default:
		return FixableByAgent
	}
}

// Error is the structured error written to stderr. Its JSON form is the
// contract: {"error", "fixable_by", "hint"?, "retry_after_seconds"?}. Field
// order is deliberate — the variable-length hint comes after fixable_by so a
// human scanning stderr sees the classification before the (longer) advice.
type Error struct {
	Message   string    `json:"error"`
	FixableBy FixableBy `json:"fixable_by"`
	Hint      string    `json:"hint,omitempty"`
	// RetryAfterSeconds, when > 0, tells the agent how long to wait before
	// retrying a FixableByRetry error. It is always the caller's value (e.g.
	// parsed from a Retry-After header); this package imposes no default.
	RetryAfterSeconds int   `json:"retry_after_seconds,omitempty"`
	Cause             error `json:"-"`
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

// New returns an Error with the given message and classification.
func New(message string, fixableBy FixableBy) *Error {
	return &Error{Message: message, FixableBy: fixableBy}
}

// Newf is New with printf-style formatting.
func Newf(fixableBy FixableBy, format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), FixableBy: fixableBy}
}

// Wrap classifies an existing error, preserving it as the cause. A nil error
// wraps to a nil *Error, so callers can wrap-and-return without a nil guard.
func Wrap(err error, fixableBy FixableBy) *Error {
	if err == nil {
		return nil
	}
	return &Error{Message: err.Error(), FixableBy: fixableBy, Cause: err}
}

// WithHint attaches an actionable hint (ideally naming the exact next command)
// and returns the same Error for chaining.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// WithHints joins several non-empty hints into the single hint field (with
// "; ") and returns the same Error for chaining.
func (e *Error) WithHints(hints ...string) *Error {
	var kept []string
	for _, h := range hints {
		if strings.TrimSpace(h) != "" {
			kept = append(kept, h)
		}
	}
	e.Hint = strings.Join(kept, "; ")
	return e
}

// WithCause attaches the underlying cause (for errors.Unwrap / errors.As) and
// returns the same Error for chaining.
func (e *Error) WithCause(err error) *Error {
	e.Cause = err
	return e
}

// WithRetryAfter records how long to wait before retrying, rounded to whole
// seconds — set it from the API's Retry-After header (or your own policy). The
// value is surfaced as retry_after_seconds; this package never supplies a
// default, so the producer's business logic is always in control.
func (e *Error) WithRetryAfter(d time.Duration) *Error {
	if s := int(d.Round(time.Second) / time.Second); s > 0 {
		e.RetryAfterSeconds = s
	}
	return e
}

// As is a convenience wrapper around errors.As for *Error.
func As(err error, target **Error) bool {
	return stderrors.As(err, target)
}

// WriteError writes err as a single structured JSON line to w (typically
// os.Stderr). A plain error that is not already an *Error is treated as
// fixable_by: agent, since an unclassified failure is most often bad input.
func WriteError(w io.Writer, err error) {
	var e *Error
	if !As(err, &e) {
		e = Wrap(err, FixableByAgent)
	}
	_ = encodeJSON(w, e, false)
}
