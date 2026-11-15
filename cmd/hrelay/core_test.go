package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func TestMain(m *testing.M) {
	// CLI 回歸不應自動綁到執行測試的真實 core；專屬案例另選 auto。
	_ = os.Setenv(deliver.BackendEnv, "herdr")
	os.Exit(m.Run())
}

func TestCoreCLIExplicitOutsideSeatAcceptsOfflineMessageWithoutHC(t *testing.T) {
	c := cliConfig(t)
	t.Setenv(deliver.BackendEnv, "core")
	t.Setenv("HC_TERMINAL_ID", "")
	t.Setenv("HC_TERMINAL_SOCKET", "")
	t.Setenv("HC_SOCKET", "/fake/core.sock")
	core, err := deliver.LoadConfig(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", core.Socket())
	if err != nil {
		t.Fatal(err)
	}
	store, err := deliver.OpenStore(context.Background(), filepath.Join(core.StateDir, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB.Close()
	server := &http.Server{Handler: (deliver.API{Config: core, Store: store}).Handler()}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-done }()
	// 非席位的顯式人工發送保留離線入口；PATH 無 hc 時不等待收件或投遞。
	// 席位內發送必須驗證 caller，另由 sender/core 交互案例覆蓋。
	t.Setenv("PATH", t.TempDir())
	for range 2 {
		// core state 綁定通常由 Serve 建立；此處使用獨立 API 測試 CLI，移除 DB 存在檢查的影響。
		binding, _ := json.Marshal(map[string]any{"schema": 1, "project_root": core.Root, "backend": "core", "core_socket": core.CoreSocket})
		if err := os.WriteFile(filepath.Join(core.StateDir, "backend.json"), binding, 0600); err != nil {
			t.Fatal(err)
		}
		if err := run(context.Background(), []string{"--root", core.Root, "send", "--ensure=false", "--from", "dev", "--key", "offline-1", "missing", "exact\nbody"}); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := store.Q.ListMessages(context.Background(), 100)
	if err != nil || len(messages) != 1 || messages[0].State != "queued" || messages[0].Body != "exact\nbody" {
		t.Fatal(messages, err)
	}
}

func TestCoreCLIRejectsLegacyListenerBeforeEnqueue(t *testing.T) {
	c := cliConfig(t)
	c.Backend, c.CoreSocket = "core", "/bound/core.sock"
	listener, err := net.Listen("unix", c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	var posts atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"project_root": c.Root, "backend": "herdr"})
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(listener) }()
	defer func() { _ = server.Close(); <-done }()
	if err := call(context.Background(), c, http.MethodPost, "/v1/messages", deliver.SendRequest{Sender: "dev", Recipient: "gm", Body: "must not enqueue", Key: "wrong-backend"}); err == nil {
		t.Fatal("core accepted old listener")
	}
	if posts.Load() != 0 {
		t.Fatal("POST reached old queue")
	}
}

func TestCoreServeUsesOnlyIsolatedResourcesAndShutsDown(t *testing.T) {
	t.Setenv(deliver.BackendEnv, "core")
	t.Setenv("HC_TERMINAL_ID", "")
	t.Setenv("HC_TERMINAL_SOCKET", "")
	t.Setenv("HC_SOCKET", "/fake/core.sock")
	t.Setenv("HC_BIN_PATH", filepath.Join(t.TempDir(), "no-real-hc"))
	state, err := os.MkdirTemp("/tmp", "hc-api-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(state)
	t.Setenv("DELIVER_STATE_DIR", state)
	c, err := deliver.LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- deliver.Serve(ctx, c, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("isolated worker did not stop")
		}
	}()
	deadline := time.After(3 * time.Second)
	for {
		connection, err := net.DialTimeout("unix", c.Socket(), 10*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			break
		}
		select {
		case err := <-done:
			t.Fatalf("serve failed: %v", err)
		case <-deadline:
			t.Fatal("serve startup timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := c.CheckStateBinding(); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--root", c.Root, "status"}); err != nil {
		t.Fatal(err)
	}
}
