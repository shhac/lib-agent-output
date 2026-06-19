package output

import (
	"encoding/json"
	"io"
)

// MetaKeyPagination is the @-prefixed key for the pagination trailer line —
// the one metadata key shared across the whole agent-* family. Tools define
// their own additional @-keys (e.g. "@counts", "@unresolved") as needed.
const MetaKeyPagination = "@pagination"

// Pagination is the value carried by the trailing {"@pagination": ...} line of
// a paginated NDJSON list.
//
// next_cursor is an opaque token the caller echoes back to fetch the next page:
// a cursor, a URL, an offset, or a page number all serialize into it, so a
// single generic shape covers every API in the family. A tool that must expose
// a richer, domain-specific pagination shape emits its own struct via
// WriteMetaLine(MetaKeyPagination, ...).
type Pagination struct {
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
	TotalItems int    `json:"total_items,omitempty"`
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
	return n.WriteMetaLine(MetaKeyPagination, p)
}
