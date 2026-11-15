package deliver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreNamedSeatsShareForegroundButNotAddress(t *testing.T) {
	f := coreSetup(t)
	f.terminal["name"] = "dispatch-alpha"
	f.write(t, "get", f.terminal)
	agents, err := f.h.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beta := agents[0]
	beta.Name, beta.Pane = "dispatch-beta", "t3"
	agents = append(agents, beta)
	for _, role := range []string{"dispatch-alpha", "dispatch-beta"} {
		if _, err := ResolveAgent(f.h.Root, role, agents); err != nil {
			t.Fatal(role, err)
		}
	}
	duplicate := agents[0]
	duplicate.Pane, duplicate.Status = "t4", "working"
	if _, err := ResolveAgent(f.h.Root, "dispatch-alpha", append(agents, duplicate)); err == nil {
		t.Fatal("busy duplicate was ignored")
	}
	if _, err := ResolveAgent(f.h.Root, "shared", agents); err == nil {
		t.Fatal("valid names must not add basename aliases")
	}
	f.terminal["name"] = "invalid name"
	f.write(t, "get", f.terminal)
	agents, err = f.h.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAgent(f.h.Root, "shared", agents); err != nil {
		t.Fatal("physical directory fallback", err)
	}
}

func TestCoreDoneUsesNormalSendWithoutForceOrWait(t *testing.T) {
	f := coreSetup(t)
	f.terminal["status"] = map[string]any{"app": "pi", "state": "done"}
	f.write(t, "get", f.terminal)
	f.write(t, "send", map[string]any{"turn": 4, "status": "done", "started": false})
	if err := f.h.PromptAgent(context.Background(), "gm", f.agent(t), "body"); err != nil {
		t.Fatal(err)
	}
	calls := f.calls(t)
	if !strings.Contains(calls, "send\x00--json\x00--\x00t2\x00body\x00") || strings.Contains(calls, "--force") || strings.Contains(calls, "--wait") {
		t.Fatal(calls)
	}
}

func TestCoreGuardAPIReportsUnsupportedWithoutChangingPolicy(t *testing.T) {
	c := Config{Root: t.TempDir(), StateDir: t.TempDir(), Backend: "core", CoreSocket: "/fake.sock"}
	handler := (API{Config: c}).Handler()
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/claude-guard", nil))
	var settings GuardSettings
	if err := json.Unmarshal(get.Body.Bytes(), &settings); err != nil || get.Code != 200 || settings.Policy.Enabled || settings.Policy.Version != 1 || !strings.Contains(settings.DiscoveryError, "unavailable") {
		t.Fatal(settings, get.Code, err)
	}
	put := httptest.NewRecorder()
	handler.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/v1/claude-guard", bytes.NewBufferString(`{"version":1,"enabled":true}`)))
	if put.Code != http.StatusConflict {
		t.Fatal(put.Code)
	}
	if _, err := os.Stat(filepath.Join(c.StateDir, "claude-guard")); !os.IsNotExist(err) {
		t.Fatal("core policy was modified", err)
	}
}
