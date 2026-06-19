package output

import "strings"

// Prune returns a copy of v with "empty" values removed, matching the family's
// compact-projection convention: nil, empty or whitespace-only strings, empty
// maps, and empty slices are dropped from maps; nil elements are dropped from
// slices. A top-level empty slice is preserved — an empty list is meaningful
// output, whereas an empty field within an object is just noise.
//
// Prune operates on JSON-decoded values (map[string]any, []any, scalars); pass
// arbitrary structs through json round-tripping first, or use Print/PrintJSON,
// which do that for you.
//
// Prune is opt-in by design. A producer that must preserve nulls — fixed
// tabular columns, where a missing key and a null mean different things —
// simply does not call it.
func Prune(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			pe := Prune(e)
			if isEmpty(pe) {
				continue
			}
			out[k] = pe
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for _, e := range val {
			pe := Prune(e)
			if pe == nil {
				continue
			}
			out = append(out, pe)
		}
		return out
	default:
		return v
	}
}

func isEmpty(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case map[string]any:
		return len(val) == 0
	case []any:
		return len(val) == 0
	default:
		return false
	}
}
