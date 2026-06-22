package output

import (
	"bytes"
	"strings"
	"testing"
)

// TestRedactorChain_InFunnel — Redactor composes with a pruner through Print's
// single prune parameter: redaction masks the secret, then PruneEmpty drops the
// empties, all on one decoded tree. The masked placeholder survives pruning and
// the @redacted notes are attached.
func TestRedactorChain_InFunnel(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Token string `json:"token"`
		Empty string `json:"empty"`
	}
	withColor(t, ColorNever) // plain output for assertions
	var buf bytes.Buffer
	err := Print(&buf, payload{Name: "alice", Token: "sk_secret", Empty: ""}, FormatJSON,
		Chain(Redactor(RedactKeys("token"), nil), PruneEmpty))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "sk_secret") {
		t.Errorf("token should be masked; got %s", out)
	}
	if !strings.Contains(out, RedactedPlaceholder) {
		t.Errorf("expected %q placeholder; got %s", RedactedPlaceholder, out)
	}
	if strings.Contains(out, `"empty"`) {
		t.Errorf("PruneEmpty should drop the empty field; got %s", out)
	}
	if !strings.Contains(out, `"alice"`) {
		t.Errorf("non-secret field should survive; got %s", out)
	}
	if !strings.Contains(out, MetaKeyRedacted) {
		t.Errorf("expected %s notes; got %s", MetaKeyRedacted, out)
	}
}

// TestChain_NilAndIdentity — Chain applies left-to-right, skips nil entries, and
// an empty Chain is the identity transform.
func TestChain_NilAndIdentity(t *testing.T) {
	v := map[string]any{"a": 1}
	if got := Chain()(v).(map[string]any); got["a"] != 1 {
		t.Errorf("empty Chain should be identity; got %v", got)
	}
	out := Chain(nil, PruneNils, nil)(map[string]any{"x": nil, "y": 2}).(map[string]any)
	if _, has := out["x"]; has {
		t.Error("PruneNils in the chain should drop nil x")
	}
	if out["y"] != 2 {
		t.Error("y should survive")
	}
}

// TestRedactor_NilRulePassThrough — a nil rule is a no-op transform.
func TestRedactor_NilRulePassThrough(t *testing.T) {
	v := map[string]any{"a": 1}
	if got := Redactor(nil, nil)(v).(map[string]any); got["a"] != 1 {
		t.Errorf("nil rule should pass through unchanged; got %v", got)
	}
}
