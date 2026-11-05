package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeRunRecordsGenerationWithoutStartingService(t *testing.T) {
	c := cliConfig(t)
	t.Setenv("HRELAY_CLAUDE_RUN_ID", "stale")
	t.Setenv("HERDR_RELAY_FROM", "wrong")
	path := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("fake exec")
	var previous string
	for i := 0; i < 2; i++ {
		err := runClaudeHarness(t.Context(), c.Root, []string{"--role", "gm", "--pane", "wC:p5", "--", path, "--resume", "session-1"}, func(binary string, args, env []string) error {
			values := map[string]string{}
			counts := map[string]int{}
			for _, entry := range env {
				k, v, _ := strings.Cut(entry, "=")
				values[k] = v
				counts[k]++
			}
			id := values["HRELAY_CLAUDE_RUN_ID"]
			if binary != path || len(args) != 3 || args[1] != "--resume" || id == "" || id == "stale" || id == previous || counts["HRELAY_CLAUDE_RUN_ID"] != 1 || values["HERDR_RELAY_FROM"] != "gm" || values["HERDR_PANE_ID"] != "wC:p5" || values["DELIVER_ROOT"] != c.Root || values["DELIVER_STATE_DIR"] != c.StateDir || values["HRELAY_GUARD_BIN"] == "" {
				t.Fatal("incorrect harness launch context", binary, args)
			}
			previous = id
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(c.Socket()); !os.IsNotExist(err) {
		t.Fatal("started service", err)
	}
	if _, err := os.Stat(filepath.Join(c.StateDir, "claude-guard", "gm", "launch.json")); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeRunInvalidArgumentsDoNotExec(t *testing.T) {
	c := cliConfig(t)
	for _, args := range [][]string{{}, {"--role", "gm", "--pane", "invalid", "--", "sh"}, {"--role", "../escape", "--pane", "wC:p5", "--", "sh"}, {"--role", "gm", "--pane", "wC:p5", "--", "/missing/claude"}} {
		err := runClaudeHarness(t.Context(), c.Root, args, func(string, []string, []string) error { t.Fatal("executed invalid launch"); return nil })
		if err == nil {
			t.Fatal("invalid launch accepted", args)
		}
	}
}
