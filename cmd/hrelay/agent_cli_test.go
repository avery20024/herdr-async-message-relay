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

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func TestAgentShortSendEnsuresDaemonWithoutWrapper(t *testing.T) {
	c := cliConfig(t)
	for _, role := range []string{"dev", "gm"} {
		if err := os.Mkdir(filepath.Join(c.Root, "角色", role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DELIVER_ROOT", c.Root)
	t.Setenv("HERDR_RELAY_FROM", "dev")
	t.Setenv("HERDR_RELAY_SERVICE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HERDR_BIN_PATH", "/fake/herdr")
	t.Setenv("HERDR_SOCKET_PATH", "/fake/herdr.sock")
	t.Setenv("PATH", t.TempDir())
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	const body = "original\n'quotes' $HOME ; --ensure=false"
	var probes, posts atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]string{"project_root": c.Root})
			return
		}
		posts.Add(1)
		var request deliver.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Sender != "dev" || request.Recipient != "gm" || request.Body != body || request.Key != "stable-key" {
			t.Errorf("body or identity changed: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	if err := run(context.Background(), []string{"send", "--key", "stable-key", "gm", body}); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 || posts.Load() != 1 {
		t.Fatal("short form must ensure readiness before enqueue")
	}
	if err := run(context.Background(), []string{"send", "--ensure=false", "--key", "stable-key", "gm", body}); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 1 || posts.Load() != 2 {
		t.Fatal("explicit manual-daemon mode should skip ensure")
	}
	t.Setenv("HERDR_RELAY_FROM", "not-the-sender")
	if err := run(context.Background(), []string{"send", "--from", "dev", "--ensure=false", "--key", "stable-key", "gm", body}); err != nil {
		t.Fatal("explicit sender should override launch context", err)
	}
}

func TestShortSendRejectsMissingIdentityAndMixedForms(t *testing.T) {
	c := cliConfig(t)
	base := filepath.Join(t.TempDir(), "no-manager")
	t.Setenv("DELIVER_ROOT", c.Root)
	t.Setenv("HERDR_RELAY_FROM", "")
	t.Setenv("HERDR_RELAY_SERVICE_HOME", base)
	for _, args := range [][]string{
		{"send", "gm", "body"},
		{"send", "gm"},
		{"send", "gm", "body", "extra"},
		{"send", "--to", "gm", "gm", "body"},
		{"send", "--body", "other", "gm", "body"},
	} {
		if err := run(context.Background(), args); err == nil {
			t.Fatal("invalid short send accepted", args)
		}
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatal("invalid request must fail before manager registration")
	}
}

func TestShortSendAmbiguousResponsePreservesKey(t *testing.T) {
	c := cliConfig(t)
	for _, role := range []string{"dev", "gm"} {
		if err := os.Mkdir(filepath.Join(c.Root, "角色", role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DELIVER_ROOT", c.Root)
	t.Setenv("HERDR_RELAY_FROM", "dev")
	err := run(context.Background(), []string{"send", "--ensure=false", "--key", "keep-key", "gm", "body"})
	if err == nil || !strings.Contains(err.Error(), `--key "keep-key"`) {
		t.Fatal("network failure must retain the retry key", err)
	}
}

func TestInstallCLIRequiresNeitherRootNorDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERDR_RELAY_BIN_DIR", "")
	if err := run(context.Background(), []string{"--root", "/nonexistent/project", "cli", "install"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(home, ".local/bin/hrelay"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("installer must create a native executable copy", err)
	}
	for _, args := range [][]string{{"cli"}, {"cli", "unknown"}, {"cli", "install", "extra"}, {"cli", "install", "--bin-dir", "relative"}} {
		if err := run(context.Background(), args); err == nil {
			t.Fatal("invalid install arguments accepted", args)
		}
	}
}
