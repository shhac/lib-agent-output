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
func PrintJSON(w io.Writer, data any, prune bool) error {
	return Print(w, data, FormatJSON, prune)
}

// Print writes data in the given format. JSON (pretty) and NDJSON (one line)
// are native; YAML and any other format are delegated to a RegisterEncoder
// encoder, returning an error if none is registered. When prune is true, empty
// fields are stripped first (see Prune). HTML escaping is always disabled so
// URLs survive intact.
// newEncoder returns a JSON encoder with HTML escaping disabled — the family
// convention, so URLs and query strings survive intact. It is the single point
// of control for the "escaping off" invariant across the package.
func newEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc
}

func Print(w io.Writer, data any, format Format, prune bool) error {
	if prune {
		data = pruneValue(data)
	}
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

// pruneValue normalizes an arbitrary value to its JSON-decoded form and prunes
// it, so Prune's tree rules apply to typed structs as well as map[string]any.
func pruneValue(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return v
	}
	return Prune(decoded)
}
