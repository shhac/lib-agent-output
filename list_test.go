package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteListNDJSONStreamsRecordsThenMeta(t *testing.T) {
	var buf bytes.Buffer
	items := []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}}
	meta := map[string]any{MetaKeyPagination: Pagination{HasMore: true, NextCursor: "c"}}

	if err := WriteList(&buf, FormatNDJSON, items, meta, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), buf.String())
	}
	last := decodeLine(t, []byte(lines[2]))
	if _, ok := last[MetaKeyPagination]; !ok {
		t.Errorf("meta line should be @pagination: %q", lines[2])
	}
}

func TestWriteListJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	items := []any{map[string]any{"id": "a"}}
	meta := map[string]any{MetaKeyPagination: map[string]any{"has_more": false}}

	if err := WriteList(&buf, FormatJSON, items, meta, false); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	data, ok := env["data"].([]any)
	if !ok || len(data) != 1 {
		t.Errorf("envelope.data = %v", env["data"])
	}
	if _, ok := env[MetaKeyPagination]; !ok {
		t.Errorf("envelope should carry @pagination: %v", env)
	}
}

func TestWriteListJSONKeepsEmptyDataList(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteList(&buf, FormatJSON, []any{}, nil, true); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if _, ok := env["data"]; !ok {
		t.Errorf("empty data list must survive pruning: %v", env)
	}
}
