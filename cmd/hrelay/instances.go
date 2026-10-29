package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/avery20024/herdr-async-message-relay/internal/service"
)

func instanceState(s service.Status) string {
	switch {
	case s.Ready:
		return "running"
	case !s.AutoStart:
		return "stopped"
	default:
		return "down"
	}
}

// runInstances 處理 ps／stop／start／rm；以實例 ID 操作，不需要也不讀取團隊根目錄。
func runInstances(ctx context.Context, command string, args []string) error {
	f := newFlags(command)
	asJSON := f.Bool("json", false, "print machine-readable JSON")
	purge := f.Bool("purge", false, "also delete the queue database of the removed instance")
	confirm := f.Bool("confirm", false, "explicitly confirm queue deletion with --purge")
	if err := flagsResult(ctx, f, f.Parse(args)); err != nil {
		return err
	}
	m, err := service.New()
	if err != nil {
		return err
	}
	if command == "ps" {
		if f.NArg() != 0 {
			return i18n.NewError("usage: hrelay ps [--json]")
		}
		statuses, err := m.Instances(ctx)
		if err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(statuses)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tAUTOSTART\tROOT")
		for _, s := range statuses {
			fmt.Fprintf(w, "%s\t%s\t%t\t%s\n", s.ID, instanceState(s), s.AutoStart, s.Root)
		}
		return w.Flush()
	}
	if f.NArg() != 1 {
		return i18n.Errorf("usage: hrelay %s [OPTIONS] ID", command)
	}
	id := f.Arg(0)
	var got string
	switch command {
	case "stop":
		got, err = m.StopByID(ctx, id)
	case "start":
		got, err = m.StartByID(ctx, id)
	case "rm":
		if *purge && !*confirm {
			return i18n.NewError("--purge deletes the queue database; add --confirm to proceed")
		}
		if !*purge && *confirm {
			return i18n.NewError("--confirm is only meaningful with --purge")
		}
		got, err = m.RemoveByID(ctx, id, *purge)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, got)
	return err
}
