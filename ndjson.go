package output

import (
	"encoding/json"
	"io"
)

// Pagination is the value carried by the trailing {"@pagination": ...} line of
// a paginated NDJSON list.
type Pagination struct {
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// NDJSONWriter emits newline-delimited JSON: one bare record per line, with
// metadata carried on @-prefixed lines. HTML escaping is disabled so URLs and
// query strings survive intact.
type NDJSONWriter struct {
	enc *json.Encoder
}

// NewNDJSONWriter returns an NDJSONWriter writing to w (typically os.Stdout).
func NewNDJSONWriter(w io.Writer) *NDJSONWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &NDJSONWriter{enc: enc}
}

// WriteItem writes a single record as one JSON line.
func (n *NDJSONWriter) WriteItem(item any) error {
	return n.enc.Encode(item)
}

// WriteMetaLine writes a single {key: value} metadata line. By convention key
// is @-prefixed (e.g. "@pagination", "@unresolved") so consumers can tell
// metadata apart from data records.
func (n *NDJSONWriter) WriteMetaLine(key string, value any) error {
	return n.enc.Encode(map[string]any{key: value})
}

// WritePagination writes the trailing {"@pagination": ...} line.
func (n *NDJSONWriter) WritePagination(p Pagination) error {
	return n.WriteMetaLine("@pagination", p)
}
