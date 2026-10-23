package queueui

import (
	"context"
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/term"
)

func Run(ctx context.Context, c deliver.Config, language i18n.Language) error {
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		return i18n.NewError("ui requires an interactive terminal")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := NewClient(c)
	defer client.Close()
	model := NewModel(ctx, client, c.Root, language)
	defer model.cancelFetch()
	_, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
	if ctx.Err() != nil && (errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, tea.ErrInterrupted)) {
		return nil
	}
	return err
}
