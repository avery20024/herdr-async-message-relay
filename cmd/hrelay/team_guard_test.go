package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

func TestTeamGuardCLIAndLegacyScope(t *testing.T) {
	c := cliConfig(t)
	if err := os.Mkdir(filepath.Join(c.Root, "角色", "gm"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"disable", "enable"} {
		if err := run(t.Context(), []string{"--root", c.Root, "claude-guard", "--team", arg}); err != nil {
			t.Fatal(err)
		}
		p, err := (claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}).TeamPolicy()
		if err != nil || p.Enabled != (arg == "enable") {
			t.Fatal(p, err)
		}
	}
	for _, args := range [][]string{{"--role", "gm", "disable"}, {"--team", "--role", "gm", "enable"}} {
		if err := run(t.Context(), append([]string{"--root", c.Root, "claude-guard"}, args...)); err == nil {
			t.Fatal("accepted ambiguous/legacy scope")
		}
	}
	if _, err := os.Stat(c.Socket()); !os.IsNotExist(err) {
		t.Fatal("CLI started service", err)
	}
}
func TestOffRecorderErrorWarnsWithoutBlockingQuestion(t *testing.T) {
	c := cliConfig(t)
	if err := os.Mkdir(filepath.Join(c.Root, "角色", "gm"), 0700); err != nil {
		t.Fatal(err)
	}
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	if err := m.ConfigureTeam(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	// 刻意缺少開始事件，結束事件無法保存；只告警，不阻止Claude。
	input, err := os.CreateTemp(t.TempDir(), "event")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err = input.WriteString(`{"hook_event_name":"PostToolUse","session_id":"s","tool_name":"AskUserQuestion","tool_use_id":"q"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = input
	t.Cleanup(func() { os.Stdin = old })
	var runErr error
	stderr := captureOutput(t, &os.Stderr, func() {
		runErr = run(t.Context(), []string{"--root", c.Root, "claude-hook", "--role", "gm", "--pane", "wC:p5"})
	})
	if runErr != nil || !strings.Contains(stderr, "observation unavailable") {
		t.Fatal(runErr, stderr)
	}
}
