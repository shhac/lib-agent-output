package output

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FileRoot is a named directory a file tool may expose to an agent. Path is the
// absolute host directory; Name is the stable label the agent sees. The host
// path is never shown to the agent — every file is addressed as Name + a
// root-relative path — so a root can move on disk without changing the agent
// surface.
type FileRoot struct {
	Name string
	Path string
}

// FileRefType is the value of a FileRef's @type discriminator. It lets a
// consumer recognise a FileRef object embedded anywhere inside an arbitrary
// record without colliding with the single-key @-prefixed metadata lines (a
// FileRef is a multi-field object, a metadata line is one @-key).
const FileRefType = "file"

// FileRef is one fetchable local file, expressed relative to a named root. It
// is the single shape for "a file the agent can read" across the family —
// whether listed by a file tool or embedded in another tool's record — so the
// file bit looks identical no matter what surrounds it.
type FileRef struct {
	Type     string `json:"@type"`
	Root     string `json:"root"`
	Path     string `json:"path"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimetype,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// NewFileRef builds a FileRef for rel — a path relative to the named root.
// Backslashes are normalised to forward slashes and the path is cleaned; the
// caller may set MimeType/Size afterwards (see FileRefAt for the on-disk form).
func NewFileRef(rootName, rel string) FileRef {
	rel = strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(rel)), "/")
	return FileRef{Type: FileRefType, Root: rootName, Path: rel, Name: path.Base(rel)}
}

// FileRefAt builds a FileRef for the file at abs, which must already have been
// validated to live inside the root by SafeResolve. rel is the root-relative
// slash path to record. Size comes from the file; MimeType is inferred from the
// extension only (no read) — use SniffMimeType when you have the bytes.
func FileRefAt(rootName, rel, abs string) (FileRef, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return FileRef{}, Wrap(err, FixableByAgent)
	}
	ref := NewFileRef(rootName, rel)
	ref.Size = info.Size()
	ref.MimeType = mime.TypeByExtension(path.Ext(rel))
	return ref, nil
}

// IsFileRef reports whether a decoded JSON object is a FileRef (carries the
// @type=file discriminator with a root and path). Consumers use it to spot file
// references embedded in otherwise-opaque records.
func IsFileRef(obj map[string]any) bool {
	if t, _ := obj["@type"].(string); t != FileRefType {
		return false
	}
	_, hasRoot := obj["root"].(string)
	_, hasPath := obj["path"].(string)
	return hasRoot && hasPath
}

// SafeResolve joins rel under root and guarantees the result stays inside it —
// the single containment chokepoint every file verb must route through. It
// rejects absolute inputs and any parent-directory (..) escape, then resolves
// symlinks and confirms the final target is still within the root, so a symlink
// pointing outside is rejected too. The returned path is absolute and safe to
// open. Error messages never leak the host path: the agent only sees the root
// name and its own relative input.
func SafeResolve(root FileRoot, rel string) (string, error) {
	if strings.TrimSpace(root.Path) == "" {
		return "", New("file root is not configured", FixableByHuman)
	}
	clean, err := cleanRootRelative(root.Name, rel)
	if err != nil {
		return "", err
	}

	rootAbs, err := filepath.Abs(root.Path)
	if err != nil {
		return "", Wrap(err, FixableByHuman)
	}
	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", Newf(FixableByHuman, "file root %q is unavailable", root.Name).WithCause(err)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(rootAbs, filepath.FromSlash(clean)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", Newf(FixableByAgent, "no such file in %q: %s", root.Name, clean)
		}
		return "", Newf(FixableByAgent, "cannot access %q in %q", clean, root.Name).WithCause(err)
	}

	if !isWithinRoot(resolved, rootResolved) {
		return "", Newf(FixableByAgent, "path escapes the %q root: %s", root.Name, rel).
			WithHint("the path resolves (via a symlink) to a location outside the root")
	}
	return resolved, nil
}

// cleanRootRelative validates that rel is a safe root-relative path and returns
// its cleaned, forward-slash form. It is pure — no filesystem access — covering
// exactly the checks that don't need disk: absolute paths and parent-directory
// (..) escapes are rejected here; symlink-based escapes are caught later by
// isWithinRoot once the path is resolved.
func cleanRootRelative(rootName, rel string) (string, error) {
	slash := filepath.ToSlash(rel)
	if path.IsAbs(slash) {
		return "", Newf(FixableByAgent, "path must be relative to the %q root: %s", rootName, rel).
			WithHint("drop the leading / and address the file relative to the root")
	}
	clean := path.Clean(slash)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", Newf(FixableByAgent, "path escapes the %q root: %s", rootName, rel).
			WithHint("the path may not use .. to leave the root")
	}
	return clean, nil
}

// isWithinRoot reports whether an already symlink-resolved path lies inside the
// resolved root. It states the containment invariant positively, in one place,
// so the security check at its call site reads plainly rather than as a
// double-negative.
func isWithinRoot(resolved, rootResolved string) bool {
	return resolved == rootResolved ||
		strings.HasPrefix(resolved, rootResolved+string(filepath.Separator))
}

// SniffMimeType returns a best-effort MIME type for a file, preferring its
// extension and falling back to content sniffing of the bytes. It is the shared
// classifier a file tool uses to decide which content block to emit.
func SniffMimeType(name string, data []byte) string {
	if ext := path.Ext(name); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}
