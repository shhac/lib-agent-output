package output

import (
	"reflect"
	"testing"
)

func TestPruneEmptyDropsEmptiesFromMaps(t *testing.T) {
	in := map[string]any{
		"id":     "x",
		"nil":    nil,
		"blank":  "   ",
		"emptyM": map[string]any{},
		"emptyS": []any{},
		"keep":   "v",
		"nested": map[string]any{"a": "1", "b": nil},
	}
	got := PruneEmpty(in).(map[string]any)

	for _, dropped := range []string{"nil", "blank", "emptyM", "emptyS"} {
		if _, ok := got[dropped]; ok {
			t.Errorf("key %q should have been pruned", dropped)
		}
	}
	if got["id"] != "x" || got["keep"] != "v" {
		t.Errorf("non-empty keys lost: %v", got)
	}
	nested := got["nested"].(map[string]any)
	if _, ok := nested["b"]; ok {
		t.Error("nested nil should be pruned")
	}
	if nested["a"] != "1" {
		t.Error("nested non-empty lost")
	}
}

func TestPruneEmptyPreservesTopLevelEmptySlice(t *testing.T) {
	got := PruneEmpty([]any{})
	if !reflect.DeepEqual(got, []any{}) {
		t.Errorf("top-level empty slice should be preserved, got %#v", got)
	}
}

func TestPruneEmptyDropsNilSliceElements(t *testing.T) {
	got := PruneEmpty([]any{"a", nil, "b"}).([]any)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("nil slice elements should be dropped, got %#v", got)
	}
}

func TestPruneNilsDropsNilsKeepsEmpties(t *testing.T) {
	in := map[string]any{
		"id":     "x",
		"nilval": nil,
		"blank":  "",
		"emptyM": map[string]any{},
		"emptyS": []any{},
	}
	got := PruneNils(in).(map[string]any)
	if _, ok := got["nilval"]; ok {
		t.Error("PruneNils should drop nil")
	}
	for _, kept := range []string{"id", "blank", "emptyM", "emptyS"} {
		if _, ok := got[kept]; !ok {
			t.Errorf("PruneNils should keep %q (only nils are dropped)", kept)
		}
	}
}

func TestPruneNilsKeepsAllSliceElements(t *testing.T) {
	got := PruneNils([]any{"a", nil, "b"}).([]any)
	if len(got) != 3 {
		t.Errorf("PruneNils keeps all slice elements (incl nil), got %#v", got)
	}
}
