package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

type claudeHookError struct{ error }

func runClaudeGuard(ctx context.Context, root string, args []string, hook bool) error {
	f := newFlags("claude-guard")
	role := f.String("role", "", "explicit recipient role")
	pane := f.String("pane", os.Getenv("HERDR_PANE_ID"), "Claude pane ID; defaults to HERDR_PANE_ID")
	if err := flagsResult(ctx, f, f.Parse(args)); err != nil {
		return err
	}
	if (hook && f.NArg() != 0) || (!hook && f.NArg() != 1) {
		return errors.New("usage: claude-guard --role ROLE enable|disable|status; claude-hook --role ROLE [--pane ID] < event.json")
	}
	c, err := deliver.LoadConfig(root)
	if err != nil {
		return err
	}
	// 發送方與目前工具的 CWD 不用來猜測接收角色。
	info, err := os.Stat(filepath.Join(c.Root, "角色", *role))
	if err != nil || !info.IsDir() {
		return errors.New("guard role directory must exist")
	}
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if hook {
		event, err := claudeguard.Decode(os.Stdin)
		if err != nil {
			return err
		}
		// 不回傳 permissionDecision，也不輸出任何答案或問題本文。
		return m.Apply(ctx, *role, *pane, event)
	}
	switch f.Arg(0) {
	case "enable":
		return m.Configure(ctx, *role, true)
	case "disable":
		return m.Configure(ctx, *role, false)
	case "status":
		policy, state, err := m.Inspect(ctx, *role)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			Policy claudeguard.Policy `json:"policy"`
			State  claudeguard.State  `json:"state"`
		}{policy, state})
	default:
		return errors.New("unknown claude-guard action")
	}
}
