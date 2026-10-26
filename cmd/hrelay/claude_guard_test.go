package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

func TestClaudeGuardCLIWithoutService(t *testing.T) {
	c := cliConfig(t)
	if err := os.Mkdir(filepath.Join(c.Root, "角色", "gm"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), []string{"--root", c.Root, "claude-guard", "--team", "enable"}); err != nil {
		t.Fatal(err)
	}
	input, err := os.CreateTemp(t.TempDir(), "event")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err = input.WriteString(`{"hook_event_name":"SessionStart","session_id":"s1"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = old })
	t.Setenv("HERDR_PANE_ID", "wC:p5")
	if err := run(t.Context(), []string{"--root", c.Root, "claude-hook", "--role", "gm"}); err != nil {
		t.Fatal(err)
	}
	manager := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	release, _, err := manager.Acquire(t.Context(), "gm", "wC:p5", "s1")
	if err != nil {
		t.Fatal(err)
	}
	release()
	// hook 錯誤保留可辨識型別供 main 回傳 exit 2，不被當作普通 exit 1。
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	err = run(t.Context(), []string{"--root", c.Root, "claude-hook", "--role", "gm", "--pane", "invalid"})
	var hookErr claudeHookError
	if !errors.As(err, &hookErr) {
		t.Fatalf("not a blocking hook error: %v", err)
	}
	if _, err = os.Stat(c.Socket()); !os.IsNotExist(err) {
		t.Fatalf("hook created service socket: %v", err)
	}
}
