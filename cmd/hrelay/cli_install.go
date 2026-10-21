package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/avery20024/herdr-async-message-relay/internal/cliinstall"
)

func runCLIInstall(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "install" {
		return errors.New("usage: hrelay cli install [--bin-dir ABSOLUTE_PATH]")
	}
	binDir := os.Getenv("HERDR_RELAY_BIN_DIR")
	if binDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		binDir = filepath.Join(home, ".local", "bin")
	}
	flags := flag.NewFlagSet("cli install", flag.ContinueOnError)
	flags.StringVar(&binDir, "bin-dir", binDir, "stable native CLI directory; defaults to ~/.local/bin")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("cli install takes no positional arguments")
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	result, err := cliinstall.Install(ctx, source, binDir)
	if err != nil {
		return err
	}
	if !result.OnPath {
		fmt.Fprintf(os.Stderr, "hrelay: CLI installed at %s; put its directory first in the agent launch PATH (existing processes are not modified)\n", result.Path)
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
