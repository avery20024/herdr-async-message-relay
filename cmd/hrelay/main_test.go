package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func cliConfig(t *testing.T) deliver.Config {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "角色"), 0700); err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp("/tmp", "hd-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	t.Setenv("DELIVER_STATE_DIR", state)
	t.Setenv("DELIVER_BATCH_SIZE", "")
	t.Setenv("DELIVER_BATCH_BYTES", "")
	t.Setenv("DELIVER_POLL_MS", "")
	c, err := deliver.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestVersionDoesNotRequireProjectOrDaemon(t *testing.T) {
	if err := run(context.Background(), []string{"--root", "/nonexistent/hrelay-project", "version"}); err != nil {
		t.Fatal(err)
	}
}

func TestCLICommandsUseUnixSocket(t *testing.T) {
	c := cliConfig(t)
	if _, err := os.Lstat(c.Socket()); !os.IsNotExist(err) {
		t.Fatalf("socket already exists: %v", err)
	}
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 8)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.URL.Path == "/v1/messages" {
			var req deliver.SendRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Sender != "dev" || req.Recipient != "gm" || req.Body != "original\n'body'" || req.Key != "stable-key" {
				t.Errorf("unexpected request: %+v", req)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	commands := [][]string{
		{"send", "--from", "dev", "--to", "gm", "--body", "original\n'body'", "--key", "stable-key"},
		{"status"}, {"show", "msg-test"}, {"show", "batch-test"},
		{"resolve", "--batch", "batch-test", "--action", "retry", "--confirm", "--note", "checked"},
	}
	want := []string{"POST /v1/messages", "GET /v1/status", "GET /v1/messages/msg-test", "GET /v1/batches/batch-test", "POST /v1/batches/batch-test/resolve"}
	for i, args := range commands {
		if err := run(context.Background(), append([]string{"--root", c.Root}, args...)); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-requests:
			if got != want[i] {
				t.Fatal(got)
			}
		case <-time.After(time.Second):
			t.Fatal("missing request")
		}
	}
}

func TestCLIInvalidCommandsAndUnavailableDaemon(t *testing.T) {
	c := cliConfig(t)
	for _, args := range [][]string{{}, {"unknown"}, {"serve", "extra"}, {"status", "extra"}, {"show"}, {"show", "msg-../x"}, {"resolve", "--batch", "../x"}, {"send", "unexpected"}} {
		if err := run(context.Background(), append([]string{"--root", c.Root}, args...)); err == nil {
			t.Fatal(args)
		}
	}
	err := run(context.Background(), []string{"--root", c.Root, "send", "--from", "dev", "--to", "gm", "--body", "body", "--key", "keep-key"})
	if err == nil || !strings.Contains(err.Error(), `--key "keep-key"`) {
		t.Fatal(err)
	}
}
