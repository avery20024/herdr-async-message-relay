package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/service"
)

func runService(ctx context.Context, root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: hrelay [--root PATH] service ensure|start|stop|restart|status|restore|list")
	}
	m, err := service.New()
	if err != nil {
		return err
	}
	switch args[0] {
	case "restore":
		return m.Restore(ctx)
	case "list":
		statuses, err := m.List(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(statuses)
	case "ensure", "start", "stop", "restart", "status":
		c, err := deliver.LoadConfig(root)
		if err != nil {
			return err
		}
		switch args[0] {
		case "ensure", "start":
			if err = m.Ensure(ctx, c, args[0] == "start"); err != nil {
				return err
			}
		case "stop":
			return m.Stop(ctx, c)
		case "restart":
			if err = m.Restart(ctx, c); err != nil {
				return err
			}
		}
		status, err := m.Status(ctx, c)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(status)
	default:
		return fmt.Errorf("unknown service command %q", args[0])
	}
}
