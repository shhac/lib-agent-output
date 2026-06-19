package output

import (
	"encoding/json"
	"io"
)

// WriteNotice writes a non-fatal, machine-parseable diagnostic as a single
// JSON line to w (typically os.Stderr). It is the informational counterpart to
// WriteError, so stderr stays structured JSON rather than ad-hoc prose.
func WriteNotice(w io.Writer, notice, hint string) {
	payload := map[string]any{"notice": notice}
	if hint != "" {
		payload["hint"] = hint
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}
