package deliver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 舊測試必須獨立於執行測試的真實終端；core 案例自行選 auto/core。
func TestMain(m *testing.M) {
	_ = os.Setenv(BackendEnv, "herdr")
	os.Exit(m.Run())
}

func TestBackendDetection(t *testing.T) {
	for _, tt := range []struct {
		name            string
		env             map[string]string
		backend, socket string
		bad             bool
	}{
		{"legacy", nil, "herdr", "", false},
		{"scope-only", map[string]string{"HC_SCOPE": "scope"}, "herdr", "", false},
		{"core", map[string]string{"HC_TERMINAL_ID": "t13", "HC_TERMINAL_SOCKET": "/core.sock"}, "core", "/core.sock", false},
		{"partial-id", map[string]string{"HC_TERMINAL_ID": "t13"}, "", "", true},
		{"partial-socket", map[string]string{"HC_TERMINAL_SOCKET": "/core.sock"}, "", "", true},
		{"relative", map[string]string{"HC_TERMINAL_ID": "t13", "HC_TERMINAL_SOCKET": "core.sock"}, "", "", true},
		{"remote-id", map[string]string{"HC_TERMINAL_ID": "box/t13", "HC_TERMINAL_SOCKET": "/core.sock"}, "", "", true},
		{"conflict", map[string]string{"HC_TERMINAL_ID": "t13", "HC_TERMINAL_SOCKET": "/core.sock", "HC_SOCKET": "/other.sock"}, "", "", true},
		{"socket-only-auto", map[string]string{"HC_SOCKET": "/core.sock"}, "", "", true},
		{"daemon", map[string]string{BackendEnv: "core", "HC_SOCKET": "/core.sock"}, "core", "/core.sock", false},
		{"explicit-core-no-socket", map[string]string{BackendEnv: "core"}, "", "", true},
		{"explicit-legacy", map[string]string{BackendEnv: "herdr", "HC_TERMINAL_ID": "t13"}, "herdr", "", false},
		{"invalid", map[string]string{BackendEnv: "unexpected"}, "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := DetectBackend(func(key string) string { return tt.env[key] })
			if (err != nil) != tt.bad {
				t.Fatalf("config=%+v err=%v", c, err)
			}
			if !tt.bad && (c.BackendName() != tt.backend || c.CoreSocket != tt.socket) {
				t.Fatalf("config=%+v", c)
			}
		})
	}
}

func TestBackendStatePathsKeepLegacyAndSeparateCoreSockets(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("DELIVER_STATE_DIR", "")
	legacy, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(state, "herdr-deliver", fmt.Sprintf("%x", sha256.Sum256([]byte(legacy.Root)))[:16])
	if legacy.StateDir != expected {
		t.Fatalf("legacy path changed: %s", legacy.StateDir)
	}
	t.Setenv(BackendEnv, "core")
	t.Setenv("HC_TERMINAL_ID", "")
	t.Setenv("HC_TERMINAL_SOCKET", "")
	t.Setenv("HC_SOCKET", "/core-a.sock")
	a, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HC_SOCKET", "/core-b.sock")
	b, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if a.StateDir == b.StateDir || a.StateDir == legacy.StateDir || a.MaxAttempts != 1 {
		t.Fatalf("isolation failure: %+v %+v", a, b)
	}
}

func TestStateBindingRefusesLegacyDatabaseAndCrossBackend(t *testing.T) {
	c := Config{Root: t.TempDir(), StateDir: t.TempDir(), Backend: "core", CoreSocket: "/a.sock"}
	db := filepath.Join(c.StateDir, "queue.db")
	if err := os.WriteFile(db, []byte("legacy data"), 0600); err != nil {
		t.Fatal(err)
	}
	if c.bindState() == nil {
		t.Fatal("must not claim an old database")
	}
	if body, _ := os.ReadFile(db); string(body) != "legacy data" {
		t.Fatal("old data was modified")
	}
	c.StateDir = t.TempDir()
	if err := c.bindState(); err != nil {
		t.Fatal(err)
	}
	if err := c.CheckStateBinding(); err != nil {
		t.Fatal(err)
	}
	other := c
	other.CoreSocket = "/b.sock"
	if other.CheckStateBinding() == nil {
		t.Fatal("cross core takeover")
	}
	other.Backend, other.CoreSocket = "", ""
	if other.CheckStateBinding() == nil {
		t.Fatal("legacy claimed core state")
	}
	if err := os.WriteFile(filepath.Join(c.StateDir, "backend.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if c.CheckStateBinding() == nil {
		t.Fatal("malformed binding")
	}
}

func TestBackendStatusBinding(t *testing.T) {
	legacy := Config{Root: "/team"}
	if err := legacy.ValidateStatusBinding([]byte(`{"project_root":"/team"}`)); err != nil {
		t.Fatal(err)
	}
	core := Config{Root: "/team", Backend: "core", CoreSocket: "/a.sock"}
	for _, body := range []string{`{"project_root":"/team"}`, `{"project_root":"/team","backend":"core","core_socket":"/b.sock"}`, `null`, `bad`} {
		if core.ValidateStatusBinding([]byte(body)) == nil {
			t.Fatal("accepted invalid binding", body)
		}
	}
	body := `{"project_root":"/team","backend":"core","core_socket":"/a.sock"}`
	if err := core.ValidateStatusBinding([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.ValidateStatusBinding([]byte(body)); err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatal("legacy accepted core listener", err)
	}
}
