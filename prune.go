package output

import "strings"

// Pruner is a content-shaping policy: it returns a copy of a JSON-decoded value
// with some "empty" nodes removed. WHICH nodes count as empty is a policy
// decision the producer owns — an empty string or null can be meaningful in one
// API and noise in another. Pass one of the provided pruners (PruneNils,
// PruneEmpty), your own, or nil for no pruning.
//
// Pruners operate on JSON-decoded values (map[string]any, []any, scalars);
// Print/PrintJSON/WriteList normalize structs via a JSON round-trip before
// applying the pruner, so you can pass typed values to them directly.
type Pruner func(any) any

// Chain composes pruners into one, applying them left to right (the first listed
// runs first). It lets a caller pass several content-shaping policies through the
// single prune parameter of Print/PrintJSON/WriteList — most importantly redact
// then prune: Chain(Redactor(rule, expose), PruneEmpty). nil entries are skipped;
// Chain() with no transforms is an identity pruner.
func Chain(transforms ...Pruner) Pruner {
	return func(v any) any {
		for _, t := range transforms {
			if t != nil {
				v = t(v)
			}
		}
		return v
	}
}

// PruneNils drops nil values from maps, leaving empty strings, empty maps, and
// empty slices — and nil slice elements — intact. Use it when an empty or null
// value is meaningful and distinct from an absent one (e.g. fixed columns, or
// an API where present-but-empty differs from omitted).
func PruneNils(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			if e == nil {
				continue
			}
			out[k] = PruneNils(e)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = PruneNils(e)
		}
		return out
	default:
		return v
	}
}

// PruneEmpty drops nils, empty or whitespace-only strings, empty maps, and empty
// slices from maps; and nil elements from slices. A top-level empty slice is
// preserved — an empty list is meaningful output, whereas an empty field within
// an object is just noise. This is the most aggressive, most token-efficient
// compact projection.
func PruneEmpty(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			pe := PruneEmpty(e)
			if isEmpty(pe) {
				continue
			}
			out[k] = pe
		}
		return out
	case []any:
		out := make([]any, 0, len(val))
		for _, e := range val {
			pe := PruneEmpty(e)
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
