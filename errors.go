package output

import (
	stderrors "errors"
	"fmt"
	"io"
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

// Error is the structured error written to stderr. Its JSON form is the
// contract: {"error": ..., "fixable_by": ..., "hint": ...?}.
type Error struct {
	Message   string    `json:"error"`
	Hint      string    `json:"hint,omitempty"`
	FixableBy FixableBy `json:"fixable_by"`
	Cause     error     `json:"-"`
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

// Wrap classifies an existing error, preserving it as the cause.
func Wrap(err error, fixableBy FixableBy) *Error {
	return &Error{Message: err.Error(), FixableBy: fixableBy, Cause: err}
}

// WithHint attaches an actionable hint (ideally naming the exact next command)
// and returns the same Error for chaining.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
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
	_ = newEncoder(w).Encode(e)
}
