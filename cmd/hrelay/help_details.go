package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

type helpOption struct {
	name, value, description string
}

type commandHelp struct {
	syntax, summary string
	options         []helpOption
	subcommands     []helpEntry
	notes, examples []string
}

var commandHelpTopics = buildCommandHelpTopics()

func buildCommandHelpTopics() map[string]commandHelp {
	topics := map[string]commandHelp{
		"help": {
			syntax:   "help [COMMAND [SUBCOMMAND]]",
			notes:    []string{"Use a command or a command plus subcommand as the help topic.", "Unknown help topics fail without loading project or service settings."},
			examples: []string{"hrelay help send", "hrelay --lang zh-TW help service restart", "hrelay cli install --help"},
		},
		"claude-guard": {
			syntax: "claude-guard --role ROLE [--pane ID] enable|disable|status",
			options: []helpOption{
				{"role", "ROLE", "explicit recipient role"},
				{"pane", "ID", "Claude pane ID; defaults to HERDR_PANE_ID"},
			},
			notes:    []string{"Experimental opt-in guard; --role is required and must name an existing role directory.", "enable preserves pending state; a new binding requires SessionStart. disable restores Herdr-only gating.", "status reads policy and state; malformed policies fail closed. No service is started."},
			examples: []string{"hrelay --root /absolute/team claude-guard --role gm status", "hrelay --root /absolute/team claude-guard --role gm enable"},
		},
		"claude-hook": {
			syntax: "claude-hook --role ROLE [--pane ID] < event.json",
			options: []helpOption{
				{"role", "ROLE", "explicit recipient role"},
				{"pane", "ID", "Claude pane ID; defaults to HERDR_PANE_ID"},
			},
			notes:    []string{"Experimental synchronous hook; reads one Claude event from stdin, never an answer.", "--role is required; --pane defaults to HERDR_PANE_ID. Hook failures exit 2.", "Do not install these hooks in global Claude settings; missing context can block unrelated questions.", "Use only with a verified role/session binding; never synthesize ready events for production panes."},
			examples: []string{"hrelay --root /absolute/test-team claude-hook --role gm --pane REAL_TEST_PANE < event.json"},
		},
		"version": {
			syntax:   "version",
			notes:    []string{"Prints the running binary's version, not the version of an existing worker."},
			examples: []string{"hrelay version"},
		},
		"send": {
			syntax: "send [OPTIONS] ROLE MESSAGE\nsend [OPTIONS] --to ROLE --body MESSAGE",
			options: []helpOption{
				{"from", "ROLE", "sender role; defaults to HERDR_RELAY_FROM"},
				{"to", "ROLE", "recipient role"},
				{"body", "MESSAGE", "exact message body"},
				{"key", "KEY", "idempotency key; reuse for network retries"},
				{"ensure", "", "ensure a supervised relay is ready before enqueue"},
			},
			notes: []string{
				"Flags must precede ROLE MESSAGE; do not mix positional arguments with --to/--body.",
				"--from defaults to HERDR_RELAY_FROM; sender and recipient must name existing role directories.",
				"--key defaults to a generated key. On an ambiguous result, reuse the reported key and identical content.",
				"--ensure defaults to true for ROLE MESSAGE, false for --to/--body. Use --ensure=false to avoid service startup.",
				"--to and --body have no defaults; provide both in the flag-only form.",
				"accepted = durably stored; submitted is not a read or task-acceptance receipt.",
			},
			examples: []string{"hrelay --root /absolute/team send --from dev --key report-1 gm 'Report ready.'", "hrelay --root /absolute/team send --from dev --ensure=false --to gm --body 'Report ready.'"},
		},
		"ui": {
			syntax: "ui",
			notes: []string{
				"Read-only history over the existing relay socket; never starts a service or writes to the queue.",
				"Manual refresh by default: r refreshes; Ctrl+W toggles one-second auto refresh.",
				"j/k or arrows select; u/d move the list by half a page; U/D page the message body.",
				"[ and ] page metadata up/down; list, body and metadata keys never switch focus.",
				"Layout: list above; metadata bottom-left and message body bottom-right; no focus switching.",
				"Below 100 columns: two-line message cards, compact labels and filters; minimum 20 x 10 cells.",
				"Manual/auto mode and key hints share one footer row; narrow M/A means manual/auto.",
				"On phones, u/d counts complete visible messages, not text lines; delivery time remains in metadata.",
				"? opens the concise keyboard-shortcut dialog; Esc or ? closes and returns to the previous screen.",
				"In help: j/k or arrows scroll, u/d or PgUp/PgDn page, g/G or Home/End jump to top/bottom.",
				"Ctrl+F opens filters; Enter applies and Esc cancels.",
				"Ctrl+P opens Preferences (UI language and theme); Enter saves, Esc cancels.",
				"Ctrl+R reloads shared preferences without refreshing messages; other UIs do not auto-sync.",
				"Preferences use the user config directory; HERDR_RELAY_UI_CONFIG overrides with an absolute file path.",
				"Themes: Terminal (default), Catppuccin Latte, Dracula, Rose Pine, Vesper.",
				"In filters, d clears the selected draft field; P (Shift+P) clears all draft fields.",
				"On the main screen, P (Shift+P) clears applied filters; U/D still pages the body.",
				"q on the main screen or Ctrl+C anywhere exits the UI, not the relay. An interactive terminal and UI-capable worker are required.",
				"Times use Asia/Taipei. Submission is not a read receipt; missing delivery evidence is shown as a dash.",
			},
			examples: []string{"hrelay --root /absolute/team ui", "hrelay --lang zh-TW --root /absolute/team ui"},
		},
		"status": {
			syntax:   "status",
			notes:    []string{"Reads queue status from the existing relay; does not automatically start a service.", "accepted = durably stored; submitted is not a read or task-acceptance receipt."},
			examples: []string{"hrelay --root /absolute/team status"},
		},
		"show": {
			syntax:   "show msg-ID|batch-ID",
			notes:    []string{"The ID is required and must start with msg- or batch-; it cannot contain /, ? or #.", "Reads the stored record from the existing relay; message bodies are not translated."},
			examples: []string{"hrelay --root /absolute/team show msg-EXAMPLE", "hrelay --root /absolute/team show batch-EXAMPLE"},
		},
		"serve": {
			syntax: "serve",
			notes: []string{
				"Runs until interrupted; use a foreground terminal or an explicit process supervisor.",
				"DELIVER_STATE_DIR overrides the state directory; otherwise use XDG_STATE_HOME or ~/.local/state plus the project hash.",
				"DELIVER_BATCH_SIZE defaults to 3 (1..100); DELIVER_BATCH_BYTES to 12000 (2048..64000); DELIVER_POLL_MS to 500 (50..60000).",
				"DELIVER_RETRY_BACKOFF_MS defaults to 30000 (0..600000); DELIVER_MAX_ATTEMPTS to 3 (1..10); delivery is at least once.",
				"Seats resolve through herdr agent list: exact role directory, then a path inside it, then an exact foreground_cwd; the match must be unique or nothing is sent.",
				"HERDR_BIN_PATH defaults to herdr; delivery requires the correct inherited Herdr socket context.",
			},
			examples: []string{"hrelay --root /absolute/team serve"},
		},
		"resolve": {
			syntax: "resolve --batch batch-ID --action retry|submitted --note EVIDENCE --confirm",
			options: []helpOption{
				{"batch", "batch-ID", "uncertain batch ID"},
				{"action", "ACTION", "retry or submitted"},
				{"note", "EVIDENCE", "inspection evidence"},
				{"confirm", "", "explicitly confirm inspection and resolution"},
			},
			notes: []string{
				"--batch, --action and --note default to empty; --confirm defaults to false. All must be explicitly supplied.",
				"Only uncertain batches can be resolved; uncertain batches are retried automatically until DELIVER_MAX_ATTEMPTS.",
				"Inspect the target pane and stored records before resolving a held batch.",
				"retry requeues the batch and may duplicate a prior submission; submitted records it without replay.",
				"Inspect uncertain batches before resolving; never blindly replay a message.",
			},
			examples: []string{"hrelay --root /absolute/team show batch-EXAMPLE", "hrelay --root /absolute/team resolve --batch batch-EXAMPLE --action submitted --note 'Verified the target received this batch.' --confirm"},
		},
		"server": {
			syntax: "server on|off|restart|status",
			subcommands: []helpEntry{
				{"on", "Start or resume this project's supervised relay, including a paused service."},
				{"off", "Persist a pause and stop this project's managed relay."},
				{"restart", "Restart this project's managed relay and resume a paused service."},
				{"status", "Inspect the project's supervised service; this does not start a relay."},
			},
			notes:    []string{"server is an alias for service: on=start, off=stop, restart=restart, status=status.", "First service registration needs the inherited absolute HERDR_SOCKET_PATH.", "off persists a pause; on/restart can resume it. Restart may interrupt dispatch."},
			examples: []string{"hrelay --root /absolute/team server status"},
		},
		"service": {
			syntax: "service ensure|start|stop|restart|status|restore|list",
			subcommands: []helpEntry{
				{"ensure", "Ensure a supervised relay is ready; refuse to resume an explicitly paused service."},
				{"start", "Start or resume this project's supervised relay, including a paused service."},
				{"stop", "Persist a pause and stop this project's managed relay."},
				{"restart", "Restart this project's managed relay and resume a paused service."},
				{"status", "Inspect the project's supervised service; this does not start a relay."},
				{"restore", "Restore registered, unpaused services across projects; no team root is required."},
				{"list", "List registered services across projects; no team root is required."},
			},
			notes:    []string{"Service management uses macOS launchd or Linux systemd --user; it does not use shell backgrounding.", "First service registration needs the inherited absolute HERDR_SOCKET_PATH.", "stop persists a pause; start/restart can resume it. Restart may interrupt dispatch.", "Service status/list may create service-manager bookkeeping; only help is guaranteed side-effect free."},
			examples: []string{"hrelay --root /absolute/team service status", "hrelay service list"},
		},
		"cli": {
			syntax:      "cli install [--bin-dir ABSOLUTE_PATH]",
			subcommands: []helpEntry{{"install", "Install or repair the stable native CLI."}},
			notes:       []string{"Installation is explicit; reading help never installs or repairs the CLI."},
			examples:    []string{"hrelay cli install --help"},
		},
		"cli install": {
			syntax:   "cli install [--bin-dir ABSOLUTE_PATH]",
			summary:  "Install or repair the stable native CLI.",
			options:  []helpOption{{"bin-dir", "ABSOLUTE_PATH", "stable native CLI directory; defaults to ~/.local/bin"}},
			notes:    []string{"--bin-dir takes precedence over HERDR_RELAY_BIN_DIR, then ~/.local/bin; the path must be absolute.", "Copies the running executable; refuses to overwrite files not owned by this installer.", "Put the installation directory first in the agent launch PATH; existing processes and workers are not upgraded."},
			examples: []string{"hrelay cli install --bin-dir /absolute/bin"},
		},
	}
	for _, parent := range []string{"server", "service"} {
		for _, action := range topics[parent].subcommands {
			topics[parent+" "+action.syntax] = commandHelp{
				syntax: parent + " " + action.syntax, summary: action.description,
				notes:    topics[parent].notes,
				examples: []string{"hrelay --root /absolute/team " + parent + " " + action.syntax},
			}
		}
	}
	return topics
}

// dispatchHelp 在設定載入前處理說明，旗標解析沿用 Go 的停止與取值規則。
func dispatchHelp(args []string, language i18n.Language) (bool, error) {
	if args[0] == "help" && !(len(args) == 2 && (args[1] == "--help" || args[1] == "-h")) {
		return true, printCommandHelp(os.Stdout, language, strings.Join(args[1:], " "))
	}
	topic, rest := args[0], args[1:]
	if len(rest) > 0 {
		if _, ok := commandHelpTopics[topic+" "+rest[0]]; ok {
			topic, rest = topic+" "+rest[0], rest[1:]
		}
	}
	detail, ok := commandHelpTopics[topic]
	if !ok {
		return false, nil
	}
	flags := newFlags(topic)
	for _, option := range detail.options {
		if option.value == "" {
			flags.Bool(option.name, false, option.description)
		} else {
			flags.String(option.name, "", option.description)
		}
	}
	if !errors.Is(flags.Parse(rest), flag.ErrHelp) {
		return false, nil
	}
	if err := printCommandHelp(os.Stderr, language, topic); err != nil {
		return true, err
	}
	return true, flag.ErrHelp
}

func printCommandHelp(w io.Writer, language i18n.Language, topic string) error {
	if topic == "" {
		return printHelp(w, language)
	}
	detail, ok := commandHelpTopics[topic]
	if !ok {
		return i18n.Errorf("usage: hrelay help [COMMAND [SUBCOMMAND]]; unknown topic %q", topic)
	}
	if detail.summary == "" {
		for _, entry := range helpCommands {
			if strings.Fields(entry.syntax)[0] == topic {
				detail.summary = entry.description
				break
			}
		}
	}
	var b strings.Builder
	line := func(text string) { fmt.Fprintln(&b, language.Text(text)) }
	fmt.Fprintf(&b, "hrelay %s\n\n", topic)
	line(detail.summary)
	b.WriteByte('\n')
	line("Usage:")
	for _, syntax := range strings.Split(detail.syntax, "\n") {
		fmt.Fprintf(&b, "  hrelay [--lang auto|en|zh-TW] [--root PATH] %s\n", syntax)
	}
	b.WriteByte('\n')
	line("Options:")
	if len(detail.options) == 0 {
		line("No command-specific options.")
	}
	for _, option := range detail.options {
		syntax := "--" + option.name
		if option.value != "" {
			syntax += " " + option.value
		} else {
			syntax += "[=true|false]"
		}
		fmt.Fprintf(&b, "  %-28s %s\n", syntax, language.Text(option.description))
	}
	if len(detail.subcommands) > 0 {
		b.WriteByte('\n')
		line("Subcommands:")
		for _, entry := range detail.subcommands {
			fmt.Fprintf(&b, "  %-12s %s\n", entry.syntax, language.Text(entry.description))
		}
	}
	b.WriteByte('\n')
	line("Global options (before the command):")
	fmt.Fprintf(&b, "  --root PATH                 %s\n  --lang auto|en|zh-TW         %s\n", language.Text("team root containing 角色"), language.Text("display language: auto, en or zh-TW"))
	b.WriteByte('\n')
	line("Context and safety:")
	for _, note := range append([]string{
		"Root: --root PATH > DELIVER_ROOT > current directory; no upward search.",
		"Language: --lang auto|en|zh-TW > HERDR_RELAY_LANG > LC_ALL / LC_MESSAGES / LANG.",
		"Help only reads static documentation; it never opens the queue or starts a service.",
	}, detail.notes...) {
		fmt.Fprintf(&b, "  %s\n", language.Text(note))
	}
	b.WriteByte('\n')
	line("Examples (replace the root and sender with your actual context):")
	for _, example := range detail.examples {
		fmt.Fprintf(&b, "  %s\n", example)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
