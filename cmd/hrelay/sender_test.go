package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func fakeSenderHerdr(t *testing.T, root string) string {
	t.Helper()
	var agents []deliver.Agent
	var tabs []map[string]string
	seq := int64(1)
	for i, role := range []string{"dev", "gm", "reviewer", "dispatch-one", "dispatch-two"} {
		dir := filepath.Join(root, "seats", role)
		if strings.HasPrefix(role, "dispatch-") {
			dir = filepath.Join(root, "shared")
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		pane, tab := fmt.Sprintf("wT:p%d", i+1), fmt.Sprintf("wT:t%d", i+1)
		agents = append(agents, deliver.Agent{ForegroundCWD: dir, Name: role, Pane: pane, TabID: tab, Seq: &seq})
		tabs = append(tabs, map[string]string{"tab_id": tab, "label": "[1] " + role})
	}
	agentJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"agents": agents}})
	tabJSON, _ := json.Marshal(map[string]any{"result": map[string]any{"tabs": tabs}})
	path := filepath.Join(t.TempDir(), "herdr")
	script := "#!/bin/sh\ncase \"$1 $2\" in\n'agent list') /bin/cat <<'AGENTS'\n" + string(agentJSON) + "\nAGENTS\n;;\n 'tab list') /bin/cat <<'TABS'\n" + string(tabJSON) + "\nTABS\n;;\n*) exit 99;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureSenderSeat(t *testing.T, c deliver.Config) {
	t.Helper()
	t.Setenv("HERDR_BIN_PATH", fakeSenderHerdr(t, c.Root))
	t.Setenv("HERDR_PANE_ID", "wT:p1")
	t.Setenv("HERDR_TAB_ID", "wT:t1")
	t.Chdir(filepath.Join(c.Root, "seats", "dev"))
}

func TestSendIdentityBeforeAnyServiceOrQueueSideEffect(t *testing.T) {
	c := cliConfig(t)
	t.Setenv("DELIVER_ROOT", c.Root)
	t.Setenv("HERDR_BIN_PATH", fakeSenderHerdr(t, c.Root))
	t.Setenv("HERDR_ENV", "1")
	t.Chdir(filepath.Join(c.Root, "seats", "reviewer"))
	t.Setenv("HERDR_RELAY_FROM", "gm")
	base := filepath.Join(t.TempDir(), "service-not-created")
	t.Setenv("HERDR_RELAY_SERVICE_HOME", base)
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var sender string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var req deliver.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		sender = req.Sender
		if req.Key != "same-key" || req.Body != "original body" {
			t.Error("payload changed", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	for _, args := range [][]string{
		{"send", "--key", "same-key", "gm", "original body"},
		{"send", "--ensure=false", "--key", "same-key", "gm", "original body"},
		{"send", "--from", "gm", "--to", "gm", "--body", "original body", "--key", "same-key"},
	} {
		if err := run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "identity conflict") {
			t.Fatal("wrong sender not rejected", args, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("rejected identity contacted queue/health", requests.Load())
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatal("rejected identity registered service", err)
	}
	// 刪除錯誤提示後，直接由發送端 pwd 與 live 目錄推導，而非 daemon cwd。
	t.Setenv("HERDR_RELAY_FROM", "")
	if err := run(context.Background(), []string{"send", "--ensure=false", "--key", "same-key", "gm", "original body"}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || sender != "reviewer" {
		t.Fatal("caller pwd was not used", requests.Load(), sender)
	}
	// 相容入口即使傳入 --from，仍不能覆蓋驗證結果。
	t.Setenv("HERDR_SEND_FROM", "gm")
	if err := run(context.Background(), []string{"send", "--from", "gm", "--ensure=false", "gm", "original body"}); err == nil {
		t.Fatal("legacy wrapper bypassed verification")
	}
	if requests.Load() != 1 {
		t.Fatal("conflicting wrapper posted")
	}
}
