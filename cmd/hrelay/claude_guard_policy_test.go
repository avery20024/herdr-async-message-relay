package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnconfiguredClaudeHooksRecordWithoutService(t *testing.T) {
	c := cliConfig(t)
	if err := os.Mkdir(filepath.Join(c.Root, "角色", "gm"), 0700); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	t.Cleanup(func() { os.Stdin = old })
	for _, name := range []string{"SessionStart", "PreToolUse", "PostToolUseFailure", "PostToolUse", "SessionEnd"} {
		input, err := os.CreateTemp(t.TempDir(), "event")
		if err != nil {
			t.Fatal(err)
		}
		body := `{"hook_event_name":"` + name + `","session_id":"s1","tool_name":"AskUserQuestion","tool_use_id":"q1"}`
		if _, err := input.WriteString(body); err != nil {
			t.Fatal(err)
		}
		if _, err := input.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		os.Stdin = input
		var runErr error
		var stderr string
		stdout := captureOutput(t, &os.Stdout, func() {
			stderr = captureOutput(t, &os.Stderr, func() {
				runErr = run(t.Context(), []string{"--root", c.Root, "claude-hook", "--role", "gm", "--pane", "wC:p5"})
			})
		})
		if err := input.Close(); err != nil {
			t.Fatal(err)
		}
		if runErr != nil || stdout != "" || stderr != "" {
			t.Fatalf("%s: err=%v stdout=%q stderr=%q", name, runErr, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(c.StateDir, "claude-guard", "gm", "events.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.Socket()); !os.IsNotExist(err) {
		t.Fatal("hook started service", err)
	}
}

func TestCorruptObservationIsBlockingHookError(t *testing.T) {
	for _, policy := range []string{`broken`, `[]`} {
		t.Run(policy, func(t *testing.T) {
			c := cliConfig(t)
			if err := os.Mkdir(filepath.Join(c.Root, "角色", "gm"), 0700); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(c.StateDir, "claude-guard", "gm")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(policy), 0600); err != nil {
				t.Fatal(err)
			}
			input, err := os.CreateTemp(t.TempDir(), "event")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if _, err := input.WriteString(`{"hook_event_name":"SessionStart","session_id":"s1"}`); err != nil {
				t.Fatal(err)
			}
			if _, err := input.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			old := os.Stdin
			os.Stdin = input
			t.Cleanup(func() { os.Stdin = old })
			err = run(t.Context(), []string{"--root", c.Root, "claude-hook", "--role", "gm", "--pane", "wC:p5"})
			var hookErr claudeHookError
			if !errors.As(err, &hookErr) {
				t.Fatalf("expected blocking hook error: %v", err)
			}
			if got, err := os.ReadFile(filepath.Join(dir, "state.json")); err != nil || string(got) != policy {
				t.Fatal("corrupt state was replaced", err)
			}
		})
	}
}
