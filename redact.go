package output

import "strings"

// MetaKeyRedacted is the top-level key under which Redact attaches its notes, so
// an agent can see WHICH fields were hidden without seeing their values.
const MetaKeyRedacted = "@redacted"

// RedactedPlaceholder replaces a masked value.
const RedactedPlaceholder = "[REDACTED]"

// RedactionNote records one masked field for the @redacted list.
type RedactionNote struct {
	Path       string `json:"path"`
	Reason     string `json:"reason,omitempty"`
	ExposeHint string `json:"expose_hint,omitempty"`
}

// RedactRule is the producer's policy: should the value at path/key be masked?
//
//   - path is the dot-path to the field (array elements add a "[]" segment).
//   - parent is the enclosing object (nil at the root or inside an array), so a
//     rule can be context-aware, e.g. mask "name" only when
//     parent["object"] == "customer".
//
// The rule closes over whatever it needs — a key list, value prefixes, or secret
// values to compare against — so no extra context argument is required. Masking
// is whole-value: returning true replaces the entire field value.
type RedactRule func(path, key string, value any, parent map[string]any) bool

// RedactKeys returns a RedactRule that masks any field whose key (case-insensitive)
// is in keys — the most common policy (a fixed list of secret field names). For
// value-prefix, context-aware, or secret-echo policies, write your own RedactRule.
func RedactKeys(keys ...string) RedactRule {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[strings.ToLower(k)] = true
	}
	return func(_, key string, _ any, _ map[string]any) bool {
		return set[strings.ToLower(key)]
	}
}

// Redact walks v (a JSON-decoded value) and replaces every field where rule
// reports true — and whose path/key is not in expose — with RedactedPlaceholder,
// returning the masked copy. When anything is masked and the root is an object,
// a MetaKeyRedacted note list is attached at the top level.
//
// expose is the --expose allowlist (comma-joined entries accepted); an entry
// reveals a redacted field by exact path, exact key, a "<prefix>." path prefix,
// or "all"/"*".
//
// Redact owns the mechanism (the walk, the placeholder, the notes, --expose
// matching); WHICH fields are secret is the rule's policy. A nil rule is a no-op.
// It masks whole values only — masking a secret that appears as a *substring* of
// a larger value (e.g. echoed into a log line) is a different, raw-bytes
// operation and stays with the producer.
func Redact(v any, rule RedactRule, expose []string) any {
	if rule == nil {
		return v
	}
	exposed := parseExpose(expose)
	masked, notes := redactValue(v, "", rule, exposed)
	if len(notes) > 0 {
		if m, ok := masked.(map[string]any); ok {
			m[MetaKeyRedacted] = notes
		}
	}
	return masked
}

// Redactor adapts redaction into a Pruner-shaped transform, so it composes with
// pruning in the same encode funnel (Print/PrintJSON/WriteList) instead of being
// a manual pre-Print step. It applies Redact(v, rule, expose); a nil rule is a
// pass-through. Compose it BEFORE a pruner with Chain so masked placeholders
// aren't then pruned away as empty:
//
//	output.Print(w, data, format, output.Chain(output.Redactor(rule, expose), output.PruneEmpty))
//
// Because the funnel normalizes structs via a single JSON round-trip before
// applying the (composed) transform, redaction now runs on that same decoded
// tree — so a consuming CLI no longer needs its own decode-redact-print shim.
func Redactor(rule RedactRule, expose []string) Pruner {
	return func(v any) any {
		return Redact(v, rule, expose)
	}
}

func redactValue(v any, path string, rule RedactRule, exposed map[string]bool) (any, []RedactionNote) {
	switch val := v.(type) {
	case map[string]any:
		return redactMap(val, path, rule, exposed)
	case []any:
		return redactSlice(val, path, rule, exposed)
	default:
		return v, nil
	}
}

func redactMap(m map[string]any, path string, rule RedactRule, exposed map[string]bool) (map[string]any, []RedactionNote) {
	out := make(map[string]any, len(m))
	var notes []RedactionNote
	for key, value := range m {
		fieldPath := joinRedactPath(path, key)
		if rule(fieldPath, key, value, m) && !isExposed(key, fieldPath, exposed) {
			out[key] = maskValue(value)
			notes = append(notes, RedactionNote{
				Path:       fieldPath,
				Reason:     "sensitive_field",
				ExposeHint: "--expose " + fieldPath,
			})
			continue
		}
		child, childNotes := redactValue(value, fieldPath, rule, exposed)
		out[key] = child
		notes = append(notes, childNotes...)
	}
	return out, notes
}

func redactSlice(s []any, path string, rule RedactRule, exposed map[string]bool) ([]any, []RedactionNote) {
	out := make([]any, len(s))
	var notes []RedactionNote
	itemPath := path + "[]"
	for i, item := range s {
		child, childNotes := redactValue(item, itemPath, rule, exposed)
		out[i] = child
		notes = append(notes, childNotes...)
	}
	return out, notes
}

// maskValue replaces a value with the placeholder, preserving nil (a null is not
// a secret, and masking it would hide that the field is absent).
func maskValue(v any) any {
	if v == nil {
		return nil
	}
	return RedactedPlaceholder
}

func joinRedactPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func parseExpose(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if n := normalizeExpose(part); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

func normalizeExpose(s string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(s)), ".")
}

func isExposed(key, path string, exposed map[string]bool) bool {
	if len(exposed) == 0 {
		return false
	}
	p := normalizeExpose(path)
	k := normalizeExpose(key)
	if exposed["all"] || exposed["*"] || exposed[p] || exposed[k] {
		return true
	}
	for allowed := range exposed {
		if allowed != "" && strings.HasPrefix(p, allowed+".") {
			return true
		}
	}
	return false
}
