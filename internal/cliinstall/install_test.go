package cliinstall

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("binary one"), 0700); err != nil {
		t.Fatal(err)
	}
	return source, filepath.Join(root, "bin")
}

func TestInstallNativeCopyAndOwnedUpdate(t *testing.T) {
	source, dir := fixture(t)
	t.Setenv("PATH", dir)
	result, err := Install(context.Background(), source, dir)
	if err != nil || !result.OnPath || result.Path != filepath.Join(dir, "hrelay") {
		t.Fatalf("%+v %v", result, err)
	}
	info, err := os.Lstat(result.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0755 {
		t.Fatalf("native regular executable expected: %v %v", info, err)
	}
	marker, err := os.Stat(filepath.Join(dir, ".hrelay-install.json"))
	if err != nil || marker.Mode().Perm() != 0600 {
		t.Fatal("ownership record must be private", err)
	}
	if err := os.WriteFile(source, []byte("binary two"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), source, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(result.Path)
	if err != nil || string(body) != "binary two" {
		t.Fatal("copy must survive managed checkout removal", err)
	}
	// 從穩定副本重跑安裝也不依賴原始專案。
	if _, err := Install(context.Background(), result.Path, dir); err != nil {
		t.Fatal(err)
	}
	staged, _ := filepath.Glob(filepath.Join(dir, ".hrelay-stage-*"))
	if len(staged) != 0 {
		t.Fatal("staging files leaked", staged)
	}
}

func TestForeignModifiedAndSymlinkEntriesAreNotOverwritten(t *testing.T) {
	for _, kind := range []string{"foreign", "modified", "symlink", "corrupt marker", "marker symlink"} {
		t.Run(kind, func(t *testing.T) {
			source, dir := fixture(t)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			target, marker := filepath.Join(dir, "hrelay"), filepath.Join(dir, ".hrelay-install.json")
			if kind != "foreign" && kind != "symlink" {
				if _, err := Install(context.Background(), source, dir); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "foreign", "modified":
				if err := os.WriteFile(target, []byte("keep me"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(source, target); err != nil {
					t.Fatal(err)
				}
			case "corrupt marker":
				if err := os.WriteFile(marker, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "marker symlink":
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source, marker); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(target)
			if _, err := Install(context.Background(), source, dir); err == nil {
				t.Fatal("must refuse unsafe replacement")
			}
			after, _ := os.ReadFile(target)
			if string(before) != string(after) {
				t.Fatal("existing executable changed")
			}
		})
	}
}

func TestPathReportingValidationAndCancelledLock(t *testing.T) {
	source, dir := fixture(t)
	t.Setenv("PATH", "/nonexistent/bin")
	result, err := Install(context.Background(), source, dir)
	if err != nil || result.OnPath {
		t.Fatal("must not claim an unavailable PATH", result, err)
	}
	for _, path := range []string{"relative", dir + "\ninvalid"} {
		if _, err := Install(context.Background(), source, path); err == nil {
			t.Fatal("unsafe bin directory accepted")
		}
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".hrelay-install.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Install(ctx, source, dir); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatal("cancelled lock must stop before replacement", err)
	}
}

func TestPathShadowingDoesNotClaimInstalledCommandIsSelected(t *testing.T) {
	source, dir := fixture(t)
	earlier := t.TempDir()
	if err := os.WriteFile(filepath.Join(earlier, "hrelay"), []byte("foreign command"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", earlier+string(os.PathListSeparator)+dir)
	result, err := Install(context.Background(), source, dir)
	if err != nil || result.OnPath {
		t.Fatal("a shadowed CLI must not be reported as selected", result, err)
	}
}

func TestConcurrentInstallHasConsistentOwnership(t *testing.T) {
	source, dir := fixture(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := Install(context.Background(), source, dir); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	body, err := os.ReadFile(filepath.Join(dir, "hrelay"))
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(dir, ".hrelay-install.json"))
	var owner ownership
	if err != nil || json.Unmarshal(marker, &owner) != nil || owner.SHA256 != digest(body) {
		t.Fatal("ownership must match installed executable", err)
	}
}
