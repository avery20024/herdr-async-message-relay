package main

import (
	"context"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

// server 是同一套服務管理器的友善入口，不另建 server 程序。
func runServer(ctx context.Context, root string, args []string) error {
	commands := map[string]string{"on": "start", "off": "stop", "restart": "restart", "status": "status"}
	if len(args) != 1 || commands[args[0]] == "" {
		return i18n.NewError("usage: hrelay [--root PATH] server on|off|restart|status")
	}
	return runService(ctx, root, []string{commands[args[0]]})
}
