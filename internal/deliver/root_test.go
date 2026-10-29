package deliver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resolved(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestInitRootCreatesIdempotentMarker(t *testing.T) {
	dir := t.TempDir()
	root, created, err := InitRoot(dir)
	if err != nil || !created || root != resolved(t, dir) {
		t.Fatalf("first init = %q %v %v", root, created, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, RootMarker))
	if err != nil || strings.Contains(string(data), dir) {
		t.Fatalf("marker must not contain a path: %q %v", data, err)
	}
	if _, created, err = InitRoot(dir); err != nil || created {
		t.Fatalf("second init created=%v err=%v", created, err)
	}
}

func TestInitRootRefusesForeignMarkerAndMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, RootMarker), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InitRoot(dir); err == nil {
		t.Fatal("foreign marker was accepted")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, RootMarker)); string(got) != "not json" {
		t.Fatal("foreign marker was overwritten")
	}
	if _, _, err := InitRoot(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
}

func TestFindRootSearchesParentsAndNearestWins(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "a", "b")
	deep := filepath.Join(inner, "c", "d")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := FindRoot(deep); err == nil {
		t.Fatal("root found without a marker")
	}
	if _, _, err := InitRoot(outer); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(deep); err != nil || got != resolved(t, outer) {
		t.Fatalf("outer root = %q %v", got, err)
	}
	if _, _, err := InitRoot(inner); err != nil {
		t.Fatal(err)
	}
	if got, err := FindRoot(deep); err != nil || got != resolved(t, inner) {
		t.Fatalf("nearest root = %q %v", got, err)
	}
}

func TestFindRootIgnoresDirectoryAndSymlinkMarkers(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, RootMarker), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := FindRoot(dir); err == nil {
		t.Fatal("directory named .hrelay was accepted")
	}
	other := t.TempDir()
	if _, _, err := InitRoot(other); err != nil {
		t.Fatal(err)
	}
	link := t.TempDir()
	if err := os.Symlink(filepath.Join(other, RootMarker), filepath.Join(link, RootMarker)); err != nil {
		t.Fatal(err)
	}
	if _, err := FindRoot(link); err == nil {
		t.Fatal("symlinked marker was accepted")
	}
}

func TestLoadConfigRootPrecedenceAndNoCwdFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("DELIVER_STATE_DIR", "")
	marked, plain, explicit := t.TempDir(), t.TempDir(), t.TempDir()
	if _, _, err := InitRoot(marked); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(marked, "agents", "dev")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if c, err := LoadConfig(""); err != nil || c.Root != resolved(t, marked) {
		t.Fatalf("marker discovery = %q %v", c.Root, err)
	}
	if c, err := LoadConfig(explicit); err != nil || c.Root != resolved(t, explicit) {
		t.Fatalf("explicit root = %q %v", c.Root, err)
	}
	t.Chdir(plain)
	if _, err := LoadConfig(""); err == nil || !strings.Contains(err.Error(), "hrelay init") {
		t.Fatalf("plain cwd must fail with init hint, got %v", err)
	}
}
