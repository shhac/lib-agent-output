package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Format is an output format selector shared across the family's CLIs.
type Format string

const (
	FormatJSON   Format = "json"  // pretty-printed; the single-resource default
	FormatYAML   Format = "yaml"  // needs a registered encoder (see RegisterEncoder)
	FormatNDJSON Format = "jsonl" // one record per line; the list default
)

// ParseFormat resolves a user-supplied format string. "ndjson" is accepted as
// an alias for "jsonl" and "yml" for "yaml". An empty string is an error; use
// ResolveFormat to apply a default.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	case "jsonl", "ndjson":
		return FormatNDJSON, nil
	default:
		return "", fmt.Errorf("unknown format %q, expected one of: json, yaml, jsonl", s)
	}
}

// ResolveFormat returns the parsed flag value, or def when flag is empty.
func ResolveFormat(flag string, def Format) (Format, error) {
	if strings.TrimSpace(flag) == "" {
		return def, nil
	}
	return ParseFormat(flag)
}

// Encoder serializes a value to bytes for a Format whose encoding needs a
// third-party dependency (e.g. YAML).
type Encoder func(v any) ([]byte, error)

var (
	encodersMu sync.RWMutex
	encoders   = map[Format]Encoder{}
)

// RegisterEncoder installs an encoder for a non-stdlib Format. This is how
// lib-agent-output stays dependency-free while still supporting `--format yaml`:
// a CLI registers gopkg.in/yaml.v3 once at init, and Print/WriteList dispatch to
// it. JSON and NDJSON are always handled natively and need no registration.
func RegisterEncoder(f Format, e Encoder) {
	encodersMu.Lock()
	defer encodersMu.Unlock()
	encoders[f] = e
}

func lookupEncoder(f Format) (Encoder, bool) {
	encodersMu.RLock()
	defer encodersMu.RUnlock()
	e, ok := encoders[f]
	return e, ok
}

// PrintJSON writes data as pretty-printed JSON (2-space indent) to w. It is the
// single-resource printer; see Print for format-driven output.
// newEncoder returns a JSON encoder with HTML escaping disabled — the family
// convention, so URLs and query strings survive intact. It is the single point
// of control for the "escaping off" invariant across the package.
func newEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc
}

// PrintJSON writes data as pretty-printed JSON (2-space indent) to w. It is the
// single-resource printer; see Print for format-driven output. Pass a Pruner
// (e.g. PruneEmpty) to strip empty fields, or nil for none.
func PrintJSON(w io.Writer, data any, prune Pruner) error {
	return Print(w, data, FormatJSON, prune)
}

// Print writes data in the given format. JSON (pretty) and NDJSON (one line)
// are native; YAML and any other format are delegated to a RegisterEncoder
// encoder, returning an error if none is registered. The prune policy is the
// caller's choice — pass a Pruner (PruneNils, PruneEmpty, or your own) to shape
// the content, or nil to write it as-is. HTML escaping is always disabled so
// URLs survive intact.
func Print(w io.Writer, data any, format Format, prune Pruner) error {
	data = applyPrune(data, prune)
	switch format {
	case FormatNDJSON:
		return newEncoder(w).Encode(data)
	case FormatJSON:
		enc := newEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	default:
		enc, ok := lookupEncoder(format)
		if !ok {
			return fmt.Errorf("no encoder registered for format %q; call output.RegisterEncoder", format)
		}
		b, err := enc(data)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
}

// applyPrune normalizes v to its JSON-decoded form and applies the pruner, so a
// Pruner's tree rules reach typed structs as well as map[string]any. A nil
// pruner returns v untouched (and skips the round-trip, preserving exact
// encoding).
func applyPrune(v any, prune Pruner) any {
	if prune == nil {
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return prune(v)
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return prune(v)
	}
	return prune(decoded)
}
