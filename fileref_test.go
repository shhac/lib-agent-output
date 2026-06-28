package output

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewFileRefNormalises(t *testing.T) {
	cases := map[string]struct{ path, name string }{
		"downloads/F1.png":     {"downloads/F1.png", "F1.png"},
		"./downloads/../a.txt": {"a.txt", "a.txt"},
		"nested/dir/b.pdf":     {"nested/dir/b.pdf", "b.pdf"},
		"/leading/slash.gif":   {"leading/slash.gif", "slash.gif"},
	}
	for in, want := range cases {
		ref := NewFileRef("cache", in)
		if ref.Type != FileRefType || ref.Root != "cache" {
			t.Errorf("NewFileRef(%q): type=%q root=%q", in, ref.Type, ref.Root)
		}
		if ref.Path != want.path || ref.Name != want.name {
			t.Errorf("NewFileRef(%q) = path %q name %q, want %q / %q", in, ref.Path, ref.Name, want.path, want.name)
		}
	}
}

func TestIsFileRef(t *testing.T) {
	yes := map[string]any{"@type": "file", "root": "cache", "path": "a.png"}
	if !IsFileRef(yes) {
		t.Error("IsFileRef = false for a valid file ref")
	}
	for name, obj := range map[string]map[string]any{
		"wrong type":  {"@type": "thread", "root": "cache", "path": "a"},
		"no root":     {"@type": "file", "path": "a"},
		"no path":     {"@type": "file", "root": "cache"},
		"plain record": {"id": "F1", "path": "a"},
	} {
		if IsFileRef(obj) {
			t.Errorf("IsFileRef = true for %s", name)
		}
	}
}

func TestFileRefFor(t *testing.T) {
	roots := []FileRoot{
		{Name: "cache", Path: "/home/u/.cache/app"},
		{Name: "downloads", Path: "/home/u/.cache/app/downloads"}, // nested → deepest wins
	}

	ref, ok := FileRefFor(roots, "/home/u/.cache/app/downloads/F1.png")
	if !ok {
		t.Fatal("FileRefFor missed a path under a root")
	}
	if ref.Root != "downloads" || ref.Path != "F1.png" {
		t.Errorf("deepest-root mapping = %+v, want root downloads path F1.png", ref)
	}
	if ref.MimeType == "" {
		t.Error("expected a mimetype from the .png extension")
	}

	if r2, _ := FileRefFor(roots, "/home/u/.cache/app/users.json"); r2.Root != "cache" || r2.Path != "users.json" {
		t.Errorf("top-root mapping = %+v, want root cache path users.json", r2)
	}

	for _, p := range []string{"/etc/passwd", "relative/path.txt", "/home/u/.cache/app"} {
		if _, ok := FileRefFor(roots, p); ok {
			t.Errorf("FileRefFor(%q) matched, want no match", p)
		}
	}
}

func TestSafeResolveAllowsInRoot(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "downloads", "F1.png"), "img")
	root := FileRoot{Name: "cache", Path: dir}

	got, err := SafeResolve(root, "downloads/F1.png")
	if err != nil {
		t.Fatalf("SafeResolve: %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, "downloads", "F1.png"))
	if got != want {
		t.Errorf("SafeResolve = %q, want %q", got, want)
	}

	// The root itself ("." ) resolves to the root.
	if _, err := SafeResolve(root, "."); err != nil {
		t.Errorf("SafeResolve(root, \".\"): %v", err)
	}
}

func TestSafeResolveRejectsEscapes(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "ok.txt"), "x")
	root := FileRoot{Name: "cache", Path: dir}

	for _, rel := range []string{"../escape.txt", "a/../../escape.txt", "/etc/passwd", "../../../../etc/passwd"} {
		if _, err := SafeResolve(root, rel); err == nil {
			t.Errorf("SafeResolve(%q) succeeded, want rejection", rel)
		}
	}
}

func TestSafeResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is restricted on Windows CI")
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	mustWrite(t, secret, "top secret")

	dir := t.TempDir()
	link := filepath.Join(dir, "leak")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	root := FileRoot{Name: "cache", Path: dir}

	if _, err := SafeResolve(root, "leak/secret.txt"); err == nil {
		t.Error("SafeResolve followed a symlink out of the root, want rejection")
	}
}

func TestSafeResolveRejectsEmptyRootPath(t *testing.T) {
	for _, p := range []string{"", "   "} {
		if _, err := SafeResolve(FileRoot{Name: "cache", Path: p}, "a.txt"); err == nil {
			t.Errorf("SafeResolve(root path %q) succeeded, want rejection", p)
		}
	}
}

func TestSafeResolveFollowsInternalSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is restricted on Windows CI")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "target.txt"), "hi")
	if err := os.Symlink(filepath.Join(dir, "target.txt"), filepath.Join(dir, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := SafeResolve(FileRoot{Name: "cache", Path: dir}, "link")
	if err != nil {
		t.Fatalf("SafeResolve(internal symlink): %v", err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, "target.txt"))
	if got != want {
		t.Errorf("SafeResolve(link) = %q, want %q", got, want)
	}
}

func TestSafeResolveWithSymlinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is restricted on Windows CI")
	}
	real := t.TempDir()
	mustWrite(t, filepath.Join(real, "ok.txt"), "x")
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.txt"), "secret")
	if err := os.Symlink(outside, filepath.Join(real, "leak")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Reach the root itself through a symlink, so EvalSymlinks(rootAbs) matters.
	rootLink := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(real, rootLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	root := FileRoot{Name: "cache", Path: rootLink}

	if _, err := SafeResolve(root, "ok.txt"); err != nil {
		t.Errorf("SafeResolve(ok.txt) through symlinked root failed: %v", err)
	}
	if _, err := SafeResolve(root, "leak/secret.txt"); err == nil {
		t.Error("SafeResolve escaped via symlink under a symlinked root, want rejection")
	}
}

func TestSafeResolveNotFound(t *testing.T) {
	root := FileRoot{Name: "cache", Path: t.TempDir()}
	_, err := SafeResolve(root, "missing.png")
	if err == nil {
		t.Fatal("SafeResolve(missing) succeeded, want not-found error")
	}
	var e *Error
	if !As(err, &e) || e.FixableBy != FixableByAgent {
		t.Errorf("not-found error = %v, want fixable_by agent", err)
	}
}

func TestSafeResolveRootUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is restricted on Windows CI")
	}
	// A root that is a broken symlink can't be resolved — a human (mis)configured it.
	rootLink := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(filepath.Join(t.TempDir(), "does-not-exist"), rootLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := SafeResolve(FileRoot{Name: "cache", Path: rootLink}, "a.txt")
	if err == nil {
		t.Fatal("SafeResolve with unresolvable root succeeded, want error")
	}
	var e *Error
	if !As(err, &e) || e.FixableBy != FixableByHuman {
		t.Errorf("root-unavailable error = %v, want fixable_by human", err)
	}
}

func TestSafeResolveCannotAccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is restricted on Windows CI")
	}
	dir := t.TempDir()
	// A circular symlink resolves to ELOOP, not ErrNotExist — exercises the
	// "cannot access" branch distinct from the not-found one.
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := SafeResolve(FileRoot{Name: "cache", Path: dir}, "a")
	if err == nil {
		t.Fatal("SafeResolve on a circular symlink succeeded, want error")
	}
	var e *Error
	if !As(err, &e) || e.FixableBy != FixableByAgent {
		t.Errorf("cannot-access error = %v, want fixable_by agent", err)
	}
}

func TestSniffMimeType(t *testing.T) {
	if mt := SniffMimeType("a.png", nil); mt == "" {
		t.Error("png by extension returned empty")
	}
	// No extension: fall back to content sniffing (PNG magic bytes).
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if mt := SniffMimeType("noext", png); mt != "image/png" {
		t.Errorf("content sniff = %q, want image/png", mt)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
