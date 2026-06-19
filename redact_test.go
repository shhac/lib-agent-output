package output

import (
	"strings"
	"testing"
)

func TestRedactKeysMasksAndNotes(t *testing.T) {
	in := map[string]any{
		"id":            "cus_1",
		"client_secret": "sk_live_abc",
		"nested":        map[string]any{"api_key": "key_xyz", "name": "ok"},
	}
	out := Redact(in, RedactKeys("client_secret", "api_key"), nil).(map[string]any)

	if out["client_secret"] != RedactedPlaceholder {
		t.Errorf("client_secret = %v, want masked", out["client_secret"])
	}
	if out["id"] != "cus_1" {
		t.Errorf("non-secret field changed: %v", out["id"])
	}
	if nested := out["nested"].(map[string]any); nested["api_key"] != RedactedPlaceholder || nested["name"] != "ok" {
		t.Errorf("nested = %v", nested)
	}

	notes, ok := out[MetaKeyRedacted].([]RedactionNote)
	if !ok {
		t.Fatalf("@redacted notes missing or wrong type: %T", out[MetaKeyRedacted])
	}
	paths := map[string]bool{}
	for _, n := range notes {
		paths[n.Path] = true
	}
	if !paths["client_secret"] || !paths["nested.api_key"] {
		t.Errorf("note paths = %v, want client_secret + nested.api_key", paths)
	}
	// notes attach at the TOP level only, never nested.
	if _, leaked := out["nested"].(map[string]any)[MetaKeyRedacted]; leaked {
		t.Error("@redacted should not appear on nested objects")
	}
}

func TestRedactNoNotesWhenNothingMasked(t *testing.T) {
	out := Redact(map[string]any{"id": "x"}, RedactKeys("secret"), nil).(map[string]any)
	if _, ok := out[MetaKeyRedacted]; ok {
		t.Error("@redacted should be absent when nothing was masked")
	}
}

func TestRedactPreservesNil(t *testing.T) {
	out := Redact(map[string]any{"secret": nil}, RedactKeys("secret"), nil).(map[string]any)
	if out["secret"] != nil {
		t.Errorf("nil secret should stay nil, got %v", out["secret"])
	}
	if _, ok := out[MetaKeyRedacted]; !ok {
		t.Error("a masked-but-nil field still records a note")
	}
}

func TestRedactExpose(t *testing.T) {
	rule := RedactKeys("token")
	in := func() map[string]any {
		return map[string]any{"token": "t", "meta": map[string]any{"token": "t2"}}
	}

	// expose by exact key reveals all matching keys
	out := Redact(in(), rule, []string{"token"}).(map[string]any)
	if out["token"] != "t" || out["meta"].(map[string]any)["token"] != "t2" {
		t.Errorf("--expose token should reveal both: %v", out)
	}
	// expose by path reveals just one
	out = Redact(in(), rule, []string{"meta.token"}).(map[string]any)
	if out["token"] != RedactedPlaceholder || out["meta"].(map[string]any)["token"] != "t2" {
		t.Errorf("--expose meta.token should reveal only the nested one: %v", out)
	}
	// expose by prefix
	out = Redact(in(), rule, []string{"meta"}).(map[string]any)
	if out["meta"].(map[string]any)["token"] != "t2" {
		t.Errorf("--expose meta (prefix) should reveal meta.token: %v", out)
	}
	// expose all
	out = Redact(in(), rule, []string{"all"}).(map[string]any)
	if out["token"] != "t" {
		t.Errorf("--expose all should reveal everything: %v", out)
	}
}

func TestRedactValuePrefixRule(t *testing.T) {
	// posthog-style: mask any string value with a known secret prefix.
	rule := func(_, _ string, value any, _ map[string]any) bool {
		s, ok := value.(string)
		return ok && strings.HasPrefix(s, "phc_")
	}
	out := Redact(map[string]any{"k": "phc_secret", "j": "plain"}, rule, nil).(map[string]any)
	if out["k"] != RedactedPlaceholder || out["j"] != "plain" {
		t.Errorf("value-prefix redaction = %v", out)
	}
}

func TestRedactContextAwareRule(t *testing.T) {
	// stripe-style: mask "name" only inside a customer object.
	rule := func(_, key string, _ any, parent map[string]any) bool {
		return key == "name" && parent["object"] == "customer"
	}
	in := map[string]any{
		"cust":    map[string]any{"object": "customer", "name": "Ada"},
		"product": map[string]any{"object": "product", "name": "Widget"},
	}
	out := Redact(in, rule, nil).(map[string]any)
	if out["cust"].(map[string]any)["name"] != RedactedPlaceholder {
		t.Error("customer name should be masked")
	}
	if out["product"].(map[string]any)["name"] != "Widget" {
		t.Error("product name should not be masked")
	}
}

func TestRedactWholeObject(t *testing.T) {
	// A matched key whose value is an object is masked wholesale.
	out := Redact(map[string]any{"credentials": map[string]any{"user": "u", "pass": "p"}},
		RedactKeys("credentials"), nil).(map[string]any)
	if out["credentials"] != RedactedPlaceholder {
		t.Errorf("whole-object mask = %v", out["credentials"])
	}
}

func TestRedactArrays(t *testing.T) {
	in := map[string]any{"items": []any{
		map[string]any{"id": "1", "secret": "s1"},
		map[string]any{"id": "2", "secret": "s2"},
	}}
	out := Redact(in, RedactKeys("secret"), nil).(map[string]any)
	items := out["items"].([]any)
	for i, it := range items {
		if it.(map[string]any)["secret"] != RedactedPlaceholder {
			t.Errorf("item %d secret not masked", i)
		}
	}
	notes := out[MetaKeyRedacted].([]RedactionNote)
	if len(notes) != 2 || !strings.Contains(notes[0].Path, "items[]") {
		t.Errorf("array note paths = %v", notes)
	}
}

func TestRedactNilRuleIsNoop(t *testing.T) {
	in := map[string]any{"secret": "s"}
	out := Redact(in, nil, nil).(map[string]any)
	if out["secret"] != "s" {
		t.Error("nil rule should not mask anything")
	}
}
