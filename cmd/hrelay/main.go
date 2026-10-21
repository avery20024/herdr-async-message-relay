package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/service"
)

// version 由發布建置的 ldflags 注入；開發時直接 go run 則顯示 dev。
var version = "dev"

func call(ctx context.Context, c deliver.Config, method, path string, input any) error {
	var data []byte
	var err error
	if input != nil {
		data, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket())
	}}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	_, err = os.Stdout.Write(body)
	return err
}

func run(ctx context.Context, args []string) error {
	global := flag.NewFlagSet("hrelay", flag.ContinueOnError)
	root := global.String("root", os.Getenv("DELIVER_ROOT"), "team root containing 角色")
	if err := global.Parse(args); err != nil {
		return err
	}
	if global.NArg() < 1 {
		return errors.New("usage: hrelay [--root PATH] version|cli|server|service|serve|send|status|show|resolve")
	}
	if global.NArg() == 1 && global.Arg(0) == "version" {
		_, err := fmt.Fprintf(os.Stdout, "hrelay %s\n", version)
		return err
	}
	if global.Arg(0) == "cli" {
		return runCLIInstall(ctx, global.Args()[1:])
	}
	if global.Arg(0) == "server" {
		return runServer(ctx, *root, global.Args()[1:])
	}
	if global.Arg(0) == "service" {
		return runService(ctx, *root, global.Args()[1:])
	}
	c, err := deliver.LoadConfig(*root)
	if err != nil {
		return err
	}
	command, rest := global.Arg(0), global.Args()[1:]
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	switch command {
	case "serve":
		if len(rest) != 0 {
			return errors.New("serve takes no arguments")
		}
		return deliver.Serve(ctx, c, slog.Default())
	case "send":
		from := f.String("from", os.Getenv("HERDR_RELAY_FROM"), "sender role; defaults to HERDR_RELAY_FROM")
		to := f.String("to", "", "recipient role")
		body := f.String("body", "", "exact message body")
		key := f.String("key", "", "idempotency key; reuse for network retries")
		ensure := f.Bool("ensure", false, "ensure a supervised relay is ready before enqueue")
		if err := f.Parse(rest); err != nil {
			return err
		}
		specified := make(map[string]bool)
		f.Visit(func(option *flag.Flag) { specified[option.Name] = true })
		short := f.NArg() != 0
		if short {
			if f.NArg() != 2 || specified["to"] || specified["body"] {
				return errors.New("usage: hrelay send [--from ROLE] [--key KEY] ROLE MESSAGE; do not mix positional and --to/--body forms")
			}
			*to, *body = f.Arg(0), f.Arg(1)
			if !specified["ensure"] {
				*ensure = true
			}
		}
		if *key == "" {
			*key = deliver.NewID("key-")
		}
		request := deliver.SendRequest{Sender: *from, Recipient: *to, Body: *body, Key: *key}
		if short || *ensure {
			if *from == "" {
				return errors.New("sender role is required: set HERDR_RELAY_FROM in the agent environment or pass --from; hrelay does not guess roles")
			}
			if err := c.Validate(request); err != nil {
				return err
			}
		}
		if *ensure {
			manager, err := service.New()
			if err != nil {
				return err
			}
			if err := manager.Ensure(ctx, c, false); err != nil {
				return fmt.Errorf("relay startup failed; no message was enqueued: %w", err)
			}
		}
		err := call(ctx, c, "POST", "/v1/messages", request)
		if err != nil {
			return fmt.Errorf("send not confirmed; it may already be stored. Inspect status or retry identical content with --key %q: %w", *key, err)
		}
		return nil
	case "status":
		if len(rest) != 0 {
			return errors.New("status takes no arguments")
		}
		return call(ctx, c, "GET", "/v1/status", nil)
	case "show":
		if len(rest) != 1 || strings.ContainsAny(rest[0], "/?#") {
			return errors.New("show requires message or batch ID")
		}
		if strings.HasPrefix(rest[0], "batch-") {
			return call(ctx, c, "GET", "/v1/batches/"+rest[0], nil)
		}
		if !strings.HasPrefix(rest[0], "msg-") {
			return errors.New("show requires message or batch ID")
		}
		return call(ctx, c, "GET", "/v1/messages/"+rest[0], nil)
	case "resolve":
		id := f.String("batch", "", "uncertain batch ID")
		action := f.String("action", "", "retry or submitted")
		note := f.String("note", "", "inspection evidence")
		confirm := f.Bool("confirm", false, "explicitly confirm inspection and resolution")
		if err := f.Parse(rest); err != nil {
			return err
		}
		if f.NArg() != 0 || !strings.HasPrefix(*id, "batch-") || strings.ContainsAny(*id, "/?#") {
			return errors.New("resolve requires --batch batch-ID")
		}
		return call(ctx, c, "POST", "/v1/batches/"+*id+"/resolve", map[string]any{"action": *action, "note": *note, "confirm": *confirm})
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func main() {
	// 此工具只支援 macOS／Linux；新建 DB、WAL、socket 與鎖皆使用私人權限。
	syscall.Umask(0077)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hrelay:", err)
		os.Exit(1)
	}
}
