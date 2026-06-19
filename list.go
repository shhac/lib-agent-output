package output

import (
	"io"
	"sort"
)

// WriteList renders a list of records plus optional @-prefixed metadata, in the
// family's two list shapes:
//
//   - NDJSON: one record per line (optionally pruned), then one line per
//     metadata entry, keys sorted for deterministic output.
//   - JSON / YAML: a single envelope {"data": [records], <meta keys...>}.
//
// The caller supplies the metadata map and its key names (e.g.
// MetaKeyPagination), so WriteList imposes no policy on whether meta keys are
// @-prefixed or where pagination lives — it just renders what it's given. The
// prune Pruner (nil for none) shapes each record; the envelope is never pruned,
// so an empty "data" list survives.
func WriteList(w io.Writer, format Format, items []any, meta map[string]any, prune Pruner) error {
	items = pruneItems(items, prune)

	if format == FormatNDJSON {
		nw := NewNDJSONWriter(w)
		for _, it := range items {
			if err := nw.WriteItem(it); err != nil {
				return err
			}
		}
		for _, k := range sortedKeys(meta) {
			if err := nw.WriteMetaLine(k, meta[k]); err != nil {
				return err
			}
		}
		return nil
	}

	envelope := map[string]any{"data": items}
	for k, v := range meta {
		envelope[k] = v
	}
	// The envelope itself is not pruned: an empty "data" list must survive.
	return Print(w, envelope, format, nil)
}

func pruneItems(items []any, prune Pruner) []any {
	if prune == nil {
		return items
	}
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = applyPrune(it, prune)
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
