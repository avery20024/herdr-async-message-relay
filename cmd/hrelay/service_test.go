package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEnsureSendReusesDaemonAndRespectsPauseBeforeEnqueue(t *testing.T) {
	c := cliConfig(t)
	for _, role := range []string{"dev", "gm"} {
		if err := os.Mkdir(filepath.Join(c.Root, "角色", role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	base, home := t.TempDir(), t.TempDir()
	t.Setenv("HERDR_RELAY_SERVICE_HOME", base)
	t.Setenv("HERDR_SOCKET_PATH", "/fake/herdr.sock")
	t.Setenv("HERDR_BIN_PATH", "/fake/herdr")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// 即使測試失敗，也不可意外找到本機服務管理器。
	t.Setenv("PATH", t.TempDir())
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	var broken atomic.Bool
	var posts atomic.Int32
	broken.Store(true)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
			return
		}
		if broken.Load() {
			http.Error(w, "unhealthy", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"project_root": c.Root})
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	args := []string{"--root", c.Root, "send", "--ensure", "--from", "dev", "--to", "gm", "--body", "original body", "--key", "same-key"}
	if err := run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "no message was enqueued") || posts.Load() != 0 {
		t.Fatalf("unhealthy daemon must prevent enqueue: %v", err)
	}
	broken.Store(false)
	if err := run(context.Background(), args); err != nil || posts.Load() != 1 {
		t.Fatalf("healthy legacy daemon should accept: %v", err)
	}
	for _, command := range []string{"on", "status"} {
		if err := run(context.Background(), []string{"--root", c.Root, "server", command}); err != nil {
			t.Fatal("server alias should use the existing healthy daemon", err)
		}
	}
	for _, command := range []string{"off", "restart"} {
		if err := run(context.Background(), []string{"--root", c.Root, "server", command}); err == nil {
			t.Fatal("server alias must not terminate unmanaged legacy daemon", command)
		}
	}
	paths, err := filepath.Glob(filepath.Join(base, "*", "service.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("missing project record: %v %v", paths, err)
	}
	body, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err = json.Unmarshal(body, &record); err != nil {
		t.Fatal(err)
	}
	if record["managed"] != false {
		t.Fatal("legacy daemon must not be marked as plugin-managed")
	}
	record["auto_start"] = false
	body, _ = json.Marshal(record)
	if err = os.WriteFile(paths[0], body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args); err == nil || !strings.Contains(err.Error(), "paused") || posts.Load() != 1 {
		t.Fatalf("paused service must not accept a new message: %v", err)
	}
	if err := run(context.Background(), []string{"service", "restore"}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"service", "list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Library")); !os.IsNotExist(err) {
		t.Fatal("a ready legacy daemon must not install a launch agent")
	}
}

func TestServiceCLIRejectsInvalidCommandsAndSendBeforeValidation(t *testing.T) {
	for _, args := range [][]string{{"service"}, {"service", "unknown"}, {"service", "list", "extra"}, {"server"}, {"server", "unknown"}, {"server", "on", "extra"}} {
		if err := run(context.Background(), args); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
	c := cliConfig(t)
	base := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("HERDR_RELAY_SERVICE_HOME", base)
	if err := run(context.Background(), []string{"--root", c.Root, "send", "--ensure", "--from", "missing", "--to", "gm", "--body", "body"}); err == nil {
		t.Fatal("invalid request must be rejected")
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatal("request validation must happen before service setup")
	}
}
