package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func runClaudeHarness(ctx context.Context, root string, args []string, execute func(string, []string, []string) error) error {
	f := newFlags("claude-run")
	role := f.String("role", os.Getenv("HERDR_RELAY_FROM"), "explicit Claude role")
	pane := f.String("pane", os.Getenv("HERDR_PANE_ID"), "actual Claude pane")
	if err := flagsResult(ctx, f, f.Parse(args)); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return errors.New("usage: claude-run --role ROLE [--pane ID] -- claude [ARGS...]")
	}
	path, err := exec.LookPath(f.Arg(0))
	if err != nil {
		return err
	}
	c, err := deliver.LoadConfig(root)
	if err != nil {
		return err
	}
	recordCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	launch, err := (claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}).StartRun(recordCtx, *role, *pane)
	if err != nil {
		return err
	}
	// exec 保留終端與訊號行為；每次實際啟動產生新世代，不啟動背景行程或worker。
	env := append([]string{}, os.Environ()...)
	env = replaceEnv(env, "HRELAY_CLAUDE_RUN_ID", launch.ID)
	env = replaceEnv(env, "DELIVER_ROOT", c.Root)
	env = replaceEnv(env, "DELIVER_STATE_DIR", c.StateDir)
	env = replaceEnv(env, "HERDR_RELAY_FROM", *role)
	env = replaceEnv(env, "HERDR_PANE_ID", *pane)
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	env = replaceEnv(env, "HRELAY_GUARD_BIN", binary)
	return execute(path, f.Args(), env)
}

func replaceEnv(env []string, key, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if len(entry) > len(key) && entry[:len(key)+1] == key+"=" {
			continue
		}
		result = append(result, entry)
	}
	return append(result, key+"="+value)
}

func runClaude(ctx context.Context, root string, args []string) error {
	return runClaudeHarness(ctx, root, args, syscall.Exec)
}
