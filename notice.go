package output

import "io"

// WriteNotice writes a non-fatal, machine-parseable diagnostic as a single
// JSON line to w (typically os.Stderr). It is the informational counterpart to
// WriteError, so stderr stays structured JSON rather than ad-hoc prose.
func WriteNotice(w io.Writer, notice, hint string) {
	payload := map[string]any{"notice": notice}
	if hint != "" {
		payload["hint"] = hint
	}
	_ = newEncoder(w).Encode(payload)
}
