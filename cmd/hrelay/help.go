package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

type helpEntry struct {
	syntax, description string
}

var helpCommands = []helpEntry{
	{"help [COMMAND [SUBCOMMAND]]", "Show overview or detailed help; no project or service required."},
	{"version", "Show the native CLI version."},
	{"send [--from ROLE] [--key KEY] ROLE MESSAGE", "Queue an original message; ensure service readiness by default."},
	{"init [PATH]", "Create the .hrelay root marker in a team root and print environment examples."},
	{"ps [--json]", "List every registered relay instance with its ID, status and root."},
	{"stop ID", "Pause one registered instance by ID; registration and queue are kept."},
	{"start ID", "Resume one registered instance by ID from its saved settings."},
	{"rm [--purge --confirm] ID", "Stop and unregister an instance; the queue is kept unless --purge --confirm."},
	{"status", "Show queue and batch status."},
	{"ui", "Browse message history in a read-only terminal UI."},
	{"show msg-ID|batch-ID", "Inspect one stored message or batch."},
	{"server on|off|restart|status", "Resume/start, pause/stop, restart, or inspect this project's relay."},
	{"service ensure|start|stop|restart|status|restore|list", "Advanced service controls; restore unpaused registrations or list services."},
	{"cli install [--bin-dir ABSOLUTE_PATH]", "Install or repair the stable native CLI."},
	{"serve", "Run the relay in the foreground."},
	{"claude-guard --team enable|disable|status", "Configure team interception; --role ROLE status inspects observations."},
	{"claude-hook --role ROLE [--pane ID]", "Experimental: read a Claude hook event from stdin; no service startup."},
	{"resolve --batch ID --action retry|submitted", "Resolve an uncertain batch only after inspection; requires --note and --confirm."},
}

var helpActions = []helpEntry{
	{"restore", "Start registered, unpaused relays."},
	{"services", "Show registered relay services."},
	{"install-cli", "Install or repair the hrelay command."},
	{"version", "Show the plugin binary version."},
}

var helpNotes = []string{
	"Detailed help: hrelay help COMMAND [SUBCOMMAND] or COMMAND [SUBCOMMAND] --help.",
	"Global --root and --lang precede the command; send flags precede ROLE MESSAGE.",
	"Root: --root PATH > DELIVER_ROOT > nearest parent directory containing .hrelay (hrelay init); never the current directory by itself.",
	"Sender: --from ROLE or HERDR_RELAY_FROM; hrelay never guesses a role.",
	"Language: --lang auto|en|zh-TW > HERDR_RELAY_LANG > LC_ALL / LC_MESSAGES / LANG.",
	"First service registration needs the inherited absolute HERDR_SOCKET_PATH.",
	"accepted = durably stored; submitted is not a read or task-acceptance receipt.",
	"off persists a pause; on/restart can resume it. Restart may interrupt dispatch.",
	"Inspect uncertain batches before resolving; never blindly replay a message.",
}

// printHelp 僅輸出靜態說明，不載入設定或觸碰佇列與服務。
func printHelp(w io.Writer, language i18n.Language) error {
	var b strings.Builder
	line := func(text string) { fmt.Fprintln(&b, language.Text(text)) }
	line("hrelay - durable, role-addressed asynchronous messages for Herdr")
	b.WriteString("\nhrelay [--lang auto|en|zh-TW] [--root PATH] COMMAND [OPTIONS]\n\n")
	line("Commands:")
	for _, entry := range helpCommands {
		fmt.Fprintf(&b, "  %-50s %s\n", entry.syntax, language.Text(entry.description))
	}
	b.WriteByte('\n')
	line("Context and safety:")
	for _, note := range helpNotes {
		fmt.Fprintf(&b, "  %s\n", language.Text(note))
	}
	b.WriteByte('\n')
	line("Examples (replace the root and sender with your actual context):")
	b.WriteString("  hrelay --root /absolute/team send --from dev gm 'Report ready.'\n  hrelay --root /absolute/team server status\n  hrelay --lang zh-TW help\n\n")
	line("Herdr plugin installation (use --ref only to pin a specific version):")
	b.WriteString("  herdr plugin install avery20024/herdr-async-message-relay\n\n")
	line("Herdr plugin actions (replace ACTION with one of the IDs below):")
	b.WriteString("  herdr plugin action invoke herdr-async-message-relay.ACTION\n")
	for _, entry := range helpActions {
		fmt.Fprintf(&b, "  %-12s %s\n", entry.syntax, language.Text(entry.description))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
