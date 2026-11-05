package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tabCLI(t *testing.T, agents []Agent, response string, exit int) CLIHerdr {
	t.Helper()
	data, err := json.Marshal(map[string]any{"result": map[string]any{"agents": agents}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// 資料以檔案餵給假 CLI，避免測試字串被 shell 解讀。
	for name, body := range map[string]string{"agents.json": string(data), "tabs.json": response} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "herdr")
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1 $2\" in\n'agent list') cat '%s/agents.json';;\n 'tab list') cat '%s/tabs.json'; exit %d;;\n*) exit 9;;\nesac\n", dir, dir, exit)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return CLIHerdr{Binary: path, Timeout: time.Second}
}

func TestTabDiscoveryRoutesSharedDirectoryIndependently(t *testing.T) {
	s, c := fixture(t)
	first := seat(c, "dev", "idle", "w1:p1", 1)
	first.Name, first.TabID = "shared", "w1:t1"
	second := first
	second.Pane, second.TabID = "w1:p2", "w1:t2"
	cli := tabCLI(t, []Agent{first, second}, `{"result":{"tabs":[{"tab_id":"w1:t2","label":"[12] dispatch-back-sort"},{"tab_id":"w1:t1","label":"[8] dispatch-front-login"}]}}`, 0)
	agents, err := cli.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for role, pane := range map[string]string{"dispatch-front-login": "w1:p1", "dispatch-back-sort": "w1:p2"} {
		a, err := ResolveAgent(c.Root, role, agents)
		if err != nil || a.Pane != pane {
			t.Fatalf("%s: %+v %v", role, a, err)
		}
		enqueue(t, s, role, role+" body")
	}
	if _, err := ResolveAgent(c.Root, "shared", agents); err == nil {
		t.Fatal("tab seats retained a shared alias")
	}
	h := &fakeHerdr{agents: agents}
	w := worker(s, c, h)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	if h.count() != 2 {
		t.Fatalf("wanted two independent deliveries, got %d", h.count())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, pane := range h.panes {
		wanted := "dispatch-front-login body"
		if pane == "w1:p2" {
			wanted = "dispatch-back-sort body"
		}
		if !strings.Contains(h.prompts[i], wanted) {
			t.Fatal("message crossed tab boundary")
		}
	}
}

func TestTabDiscoveryRejectsDuplicateAndOutsideSeats(t *testing.T) {
	_, c := fixture(t)
	a := seat(c, "dev", "idle", "w1:p1", 1)
	a.TabLabel = "dispatch-front-login"
	b := a
	b.Pane, b.Status = "w1:p2", "working"
	if _, err := ResolveAgent(c.Root, a.TabLabel, []Agent{a, b}); err == nil {
		t.Fatal("duplicate tab role accepted")
	}
	b.ForegroundCWD = t.TempDir()
	if got, err := ResolveAgent(c.Root, a.TabLabel, []Agent{a, b}); err != nil || got.Pane != a.Pane {
		t.Fatal("outside root seat participated", err)
	}
	b.ForegroundCWD = c.Root
	if _, err := ResolveAgent(c.Root, b.TabLabel, []Agent{b}); err == nil {
		t.Fatal("root itself accepted")
	}
	a.TabLabel = "generated label › shell"
	a.Name = "named-agent"
	if name, err := RoleFromAgent(c.Root, a); err != nil || name != "named-agent" {
		t.Fatal("invalid tab label did not preserve name fallback", name, err)
	}
	a.Name = ""
	if name, err := RoleFromAgent(c.Root, a); err != nil || name != "dev" {
		t.Fatal("directory fallback lost", name, err)
	}
}

func TestTabDiscoveryFailsClosed(t *testing.T) {
	a := Agent{TabID: "w1:t1"}
	for _, tc := range []struct {
		body string
		exit int
	}{
		{`help`, 0}, {`{}`, 0}, {`{"error":{"code":"failed"},"result":{"tabs":[]}}`, 0},
		{`{"result":{"tabs":[]}}`, 0},
		{`{"result":{"tabs":[{"label":"dispatch-job"}]}}`, 0},
		{`{"result":{"tabs":[{"tab_id":"w1:t1","label":"one"},{"tab_id":"w1:t1","label":"two"}]}}`, 0},
		{`{"result":{"tabs":[{"tab_id":"w1:t1","label":"dispatch-job"}]}}`, 1},
	} {
		cli := tabCLI(t, []Agent{a}, tc.body, tc.exit)
		if _, err := cli.List(context.Background()); err == nil {
			t.Fatalf("accepted failed topology: %s exit=%d", tc.body, tc.exit)
		}
	}
	// 舊 Herdr 沒有 tab_id，不會呼叫失敗的 tab list。
	cli := tabCLI(t, []Agent{{Name: "legacy"}}, "invalid", 1)
	if agents, err := cli.List(context.Background()); err != nil || len(agents) != 1 {
		t.Fatal("legacy discovery regressed", err)
	}
}
