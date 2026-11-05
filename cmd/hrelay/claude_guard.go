package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/roles"
)

type claudeHookError struct{ error }

func runClaudeGuard(ctx context.Context, root string, args []string, hook bool) error {
	f := newFlags("claude-guard")
	role := f.String("role", "", "explicit recipient role (Hooks or observation status)")
	team := f.Bool("team", false, "team-wide interception policy")
	pane := f.String("pane", os.Getenv("HERDR_PANE_ID"), "Claude pane ID; defaults to HERDR_PANE_ID")
	if err := flagsResult(ctx, f, f.Parse(args)); err != nil {
		return err
	}
	if (hook && f.NArg() != 0) || (!hook && f.NArg() != 1) {
		return errors.New("usage: claude-guard --team enable|disable|status; claude-guard --role ROLE status; claude-hook --role ROLE [--pane ID] < event.json")
	}
	c, err := deliver.LoadConfig(root)
	if err != nil {
		return err
	}
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if *team {
		if hook || *role != "" {
			return errors.New("--team cannot be combined with Hooks or --role")
		}
		switch f.Arg(0) {
		case "enable":
			return m.ConfigureTeam(ctx, true)
		case "disable":
			return m.ConfigureTeam(ctx, false)
		case "status":
			p, err := m.TeamPolicy()
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(p)
		default:
			return errors.New("unknown claude-guard action")
		}
	}
	if !hook && f.Arg(0) != "status" {
		return errors.New("interception is now team-wide; use claude-guard --team enable|disable")
	}
	// Hook 身分由啟動環境提供，SessionEnd 也可在座位離開後記錄，不要求固定目錄。
	if !roles.ValidName(*role) {
		return errors.New("invalid guard role")
	}
	if hook {
		event, err := claudeguard.Decode(os.Stdin)
		if err != nil {
			return err
		}
		event.RunID = os.Getenv("HRELAY_CLAUDE_RUN_ID")
		// 不回傳 permissionDecision；觀測無法寫入只警告，不因缺觀測阻止問答。
		if err := m.Apply(ctx, *role, *pane, event); err != nil {
			if errors.Is(err, claudeguard.ErrInvalidEvent) {
				return err
			}
			if _, policyErr := m.TeamPolicy(); policyErr != nil {
				return policyErr
			}
			fmt.Fprintln(os.Stderr, "hrelay: question observation unavailable; continuing without additional interception:", err)
			return nil
		}
		return nil
	}
	switch f.Arg(0) {
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
