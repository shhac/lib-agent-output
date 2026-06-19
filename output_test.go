package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func decodeLine(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not valid JSON: %q: %v", b, err)
	}
	return m
}

func TestWriteErrorShape(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, New("widget \"x\" not found", FixableByAgent).WithHint("list with 'item list'"))

	m := decodeLine(t, buf.Bytes())
	if m["error"] != `widget "x" not found` {
		t.Errorf("error = %q", m["error"])
	}
	if m["fixable_by"] != "agent" {
		t.Errorf("fixable_by = %q, want agent", m["fixable_by"])
	}
	if m["hint"] != "list with 'item list'" {
		t.Errorf("hint = %q", m["hint"])
	}
}

func TestWriteErrorClassifiesPlainError(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, bytes.ErrTooLarge)

	m := decodeLine(t, buf.Bytes())
	if m["fixable_by"] != "agent" {
		t.Errorf("plain error should default to agent, got %q", m["fixable_by"])
	}
	if _, ok := m["hint"]; ok {
		t.Errorf("hint should be omitted when empty")
	}
}

func TestWriteErrorDoesNotEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, New("bad query a&b<c>d", FixableByAgent))
	if !strings.Contains(buf.String(), "a&b<c>d") {
		t.Errorf("HTML should not be escaped: %s", buf.String())
	}
}

func TestNDJSONWriterOneRecordPerLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewNDJSONWriter(&buf)
	_ = w.WriteItem(map[string]any{"id": "w-1"})
	_ = w.WriteItem(map[string]any{"id": "w-2"})
	_ = w.WritePagination(Pagination{HasMore: true, NextCursor: "c2"})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), buf.String())
	}
	if got := decodeLine(t, []byte(lines[0]))["id"]; got != "w-1" {
		t.Errorf("line 0 id = %q", got)
	}
	page := decodeLine(t, []byte(lines[2]))
	meta, ok := page["@pagination"].(map[string]any)
	if !ok {
		t.Fatalf("third line should be @pagination: %q", lines[2])
	}
	if meta["has_more"] != true || meta["next_cursor"] != "c2" {
		t.Errorf("pagination payload = %v", meta)
	}
}

func TestWriteNotice(t *testing.T) {
	var buf bytes.Buffer
	WriteNotice(&buf, "rate limited, slowing down", "retry in 30s")
	m := decodeLine(t, buf.Bytes())
	if m["notice"] != "rate limited, slowing down" {
		t.Errorf("notice = %q", m["notice"])
	}
	if m["hint"] != "retry in 30s" {
		t.Errorf("hint = %q", m["hint"])
	}
}
