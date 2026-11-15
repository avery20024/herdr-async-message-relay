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

func TestCoreCallerIdentityBeforeEnqueue(t *testing.T) {
	for _, tc := range []struct {
		name, from, environment, legacy, caller, recipient               string
		duplicate, shared, wrongPWD, inheritedHerdr, unavailable, reject bool
	}{
		{name: "offline-recipient"},
		{name: "busy-recipient", recipient: "gm"},
		{name: "environment-conflict", environment: "gm", reject: true},
		{name: "legacy-conflict", legacy: "gm", reject: true},
		{name: "explicit-conflict", from: "gm", reject: true},
		{name: "duplicate-address", duplicate: true, reject: true},
		{name: "pwd-seat-conflict", wrongPWD: true, reject: true},
		{name: "ignores-old-herdr-binding", inheritedHerdr: true},
		{name: "shared-dispatch-binding", shared: true, caller: "t2", from: "dispatch-b"},
		{name: "lookup-failure-is-not-offline-exception", unavailable: true, reject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := cliConfig(t)
			first, second := "dev", "gm"
			if tc.duplicate {
				second = first
			}
			if tc.shared {
				first, second = "dispatch-a", "dispatch-b"
			}
			dir1, dir2 := filepath.Join(base.Root, "slot1"), filepath.Join(base.Root, "slot2")
			for _, path := range []string{dir1, dir2} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.shared {
				dir2 = dir1
			}
			if tc.wrongPWD || tc.shared {
				previous, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chdir(dir2); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chdir(previous); err != nil {
						t.Error(err)
					}
				})
			}
			peer := filepath.Join(base.StateDir, "core.sock")
			listener, err := net.Listen("unix", peer)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					_ = conn.Close()
				}
			}()
			t.Cleanup(func() { _ = listener.Close(); <-done })
			writeJSON := func(name string, data any) string {
				path := filepath.Join(base.StateDir, name)
				body, err := json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0600); err != nil {
					t.Fatal(err)
				}
				return path
			}
			list := writeJSON("terminals.json", map[string]any{"terminals": []map[string]string{{"address": "t1", "state": "live"}, {"address": "t2", "state": "live"}}})
			meta := func(address, name, cwd string) string {
				return writeJSON(address+".json", map[string]any{"address": address, "name": name, "state": "live", "pid": 100, "created_ms": 123,
					"foreground": map[string]any{"cwd": cwd, "pid": 200}, "status": map[string]string{"app": "pi", "state": "working"}, "turn": 0})
			}
			one, two := meta("t1", first, dir1), meta("t2", second, dir2)
			quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
			binary := filepath.Join(base.StateDir, "hc-fake")
			// 只提供唯讀 topology；任何 send／其他命令皆拒絕，不接真實 core。
			script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\nls) /bin/cat %s;;\nget) case \"$4\" in t1) /bin/cat %s;; t2) /bin/cat %s;; *) exit 92;; esac;;\n*) exit 93;;\nesac\n", quote(list), quote(one), quote(two))
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if tc.unavailable {
				binary += "-missing"
			}
			t.Setenv("HC_BIN_PATH", binary)
			t.Setenv(deliver.BackendEnv, "auto")
			caller := tc.caller
			if caller == "" {
				caller = "t1"
			}
			t.Setenv("HC_TERMINAL_ID", caller)
			t.Setenv("HC_TERMINAL_SOCKET", peer)
			t.Setenv("HC_SOCKET", peer)
			t.Setenv("HERDR_RELAY_FROM", tc.environment)
			t.Setenv("HERDR_SEND_FROM", tc.legacy)
			if tc.inheritedHerdr {
				t.Setenv("HERDR_PANE_ID", "w9:p9")
				t.Setenv("HERDR_TAB_ID", "w9:t9")
				t.Setenv("HERDR_ENV", "inherited")
			}
			config, err := deliver.LoadConfig(base.Root)
			if err != nil {
				t.Fatal(err)
			}
			relay, err := net.Listen("unix", config.Socket())
			if err != nil {
				t.Fatal(err)
			}
			var posts atomic.Int32
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/v1/status" {
					_ = json.NewEncoder(w).Encode(map[string]string{"project_root": config.Root, "backend": "core", "core_socket": peer})
					return
				}
				if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
					t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
				}
				posts.Add(1)
				var request deliver.SendRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				want := first
				if caller == "t2" {
					want = second
				}
				if request.Sender != want || request.Body != "body\nline" {
					t.Errorf("wrong enqueue content: %#v", request)
				}
				_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": true})
			})}
			served := make(chan struct{})
			go func() { defer close(served); _ = server.Serve(relay) }()
			t.Cleanup(func() { _ = server.Close(); <-served })
			from, to := tc.from, tc.recipient
			if from == "" {
				from = first
			}
			if to == "" {
				to = "missing"
			}
			err = run(context.Background(), []string{"--root", base.Root, "send", "--ensure=false", "--from", from, "--key", "core-sender", to, "body\nline"})
			if tc.reject {
				if err == nil || posts.Load() != 0 {
					t.Fatalf("identity failure enqueued: err=%v posts=%d", err, posts.Load())
				}
			} else if err != nil || posts.Load() != 1 {
				t.Fatalf("verified sender rejected: err=%v posts=%d", err, posts.Load())
			}
		})
	}
}
