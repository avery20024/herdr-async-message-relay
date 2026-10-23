package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

func TestDetailedHelpBeforeConfiguration(t *testing.T) {
	base := t.TempDir()
	for _, key := range []string{"HOME", "DELIVER_ROOT", "DELIVER_STATE_DIR", "HERDR_RELAY_SERVICE_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "HERDR_SOCKET_PATH"} {
		t.Setenv(key, filepath.Join(base, key))
	}
	t.Setenv("PATH", "")
	t.Setenv("DELIVER_POLL_MS", "invalid")
	t.Setenv("HERDR_RELAY_BIN_DIR", "invalid-relative-path")
	t.Setenv("HERDR_RELAY_FROM", "do-not-disclose-this-environment-value")
	var topics []string
	for topic := range commandHelpTopics {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	for _, language := range []string{"en", "zh-TW"} {
		for _, topic := range topics {
			t.Run(language+"/"+topic, func(t *testing.T) {
				parts := strings.Fields(topic)
				var want string
				for _, form := range []string{"help", "--help", "-h"} {
					args := []string{"--lang", language}
					if form == "help" {
						args = append(args, "help")
						args = append(args, parts...)
					} else {
						args = append(args, parts...)
						args = append(args, form)
					}
					var stderr string
					var err error
					stdout := captureOutput(t, &os.Stdout, func() {
						stderr = captureOutput(t, &os.Stderr, func() { err = run(context.Background(), args) })
					})
					if form == "help" {
						if err != nil || stderr != "" || stdout == "" {
							t.Fatalf("%v: stdout=%q stderr=%q err=%v", args, stdout, stderr, err)
						}
						want = stdout
					} else if !errors.Is(err, flag.ErrHelp) || stdout != "" || stderr != want {
						t.Fatalf("%v: help forms differ: stdout=%q stderr=%q err=%v", args, stdout, stderr, err)
					}
				}
				for _, text := range []string{"hrelay " + topic, "--root PATH", "--lang auto|en|zh-TW", "DELIVER_ROOT", "HERDR_RELAY_LANG", "hrelay"} {
					if !strings.Contains(want, text) {
						t.Errorf("missing %q in %s", text, want)
					}
				}
				for _, label := range []string{"Usage:", "Options:", "Context and safety:", "Examples (replace the root and sender with your actual context):"} {
					if !strings.Contains(want, i18n.Language(language).Text(label)) {
						t.Errorf("missing translated section %q", label)
					}
				}
				if strings.Contains(want, "do-not-disclose-this-environment-value") {
					t.Fatal("help exposed the environment value")
				}
			})
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help created configuration, queue or service files: %v, %v", entries, err)
	}
}

func TestHelpRecognitionRespectsFlagValuesAndPositionals(t *testing.T) {
	for _, args := range [][]string{
		{"send", "--from", "--help"},
		{"send", "--to", "gm", "--body", "--help"},
		{"send", "--key", "--help"},
		{"send", "--from", "dev", "gm", "--help"},
		{"send", "--", "gm", "--help"},
		{"send", "--body=--help"},
		{"send", "--unknown", "--help"},
		{"show", "msg-example", "--help"},
		{"cli", "install", "--bin-dir", "--help"},
		{"service", "unknown", "--help"},
	} {
		var handled bool
		var err error
		out := captureOutput(t, &os.Stderr, func() { handled, err = dispatchHelp(args, i18n.English) })
		if handled || err != nil || out != "" {
			t.Fatalf("misread data as help: %v -> handled=%v, err=%v, output=%q", args, handled, err, out)
		}
	}
	for _, args := range [][]string{
		{"send", "--body", "text", "--help"},
		{"send", "--key=example", "--help"},
		{"send", "--ensure=false", "--help"},
		{"cli", "install", "--bin-dir", "/some/path", "--help"},
	} {
		var handled bool
		var err error
		out := captureOutput(t, &os.Stderr, func() { handled, err = dispatchHelp(args, i18n.English) })
		if !handled || !errors.Is(err, flag.ErrHelp) || out == "" {
			t.Fatalf("missed help: %v -> handled=%v, err=%v, output=%q", args, handled, err, out)
		}
	}
}

func TestDetailedHelpCoverageAndTranslations(t *testing.T) {
	for _, entry := range helpCommands {
		if _, ok := commandHelpTopics[strings.Fields(entry.syntax)[0]]; !ok {
			t.Errorf("no detailed help for %s", entry.syntax)
		}
	}
	for _, text := range []string{"Usage:", "Options:", "Subcommands:", "No command-specific options.", "Global options (before the command):", "Help only reads static documentation; it never opens the queue or starts a service."} {
		if i18n.TraditionalChinese.Text(text) == text {
			t.Errorf("missing heading translation: %s", text)
		}
	}
	for topic, detail := range commandHelpTopics {
		if detail.syntax == "" || len(detail.examples) == 0 {
			t.Errorf("incomplete topic: %s", topic)
		}
		texts := append([]string{detail.summary}, detail.notes...)
		for _, option := range detail.options {
			texts = append(texts, option.description)
		}
		for _, action := range detail.subcommands {
			if _, ok := commandHelpTopics[topic+" "+action.syntax]; !ok {
				t.Errorf("no subcommand topic: %s %s", topic, action.syntax)
			}
			texts = append(texts, action.description)
		}
		for _, text := range texts {
			if text != "" && i18n.TraditionalChinese.Text(text) == text {
				t.Errorf("%s: missing translation: %s", topic, text)
			}
		}
	}
}

func TestDetailedHelpDocumentsDefaults(t *testing.T) {
	for topic, wanted := range map[string][]string{
		"send":            {"HERDR_RELAY_FROM", "generated key", "true for ROLE MESSAGE", "false for --to/--body", "--ensure=false"},
		"resolve":         {"default to empty", "defaults to false", "--confirm", "may duplicate"},
		"cli install":     {"HERDR_RELAY_BIN_DIR", "~/.local/bin", "must be absolute"},
		"serve":           {"DELIVER_BATCH_SIZE defaults to 3", "12000", "500", "HERDR_BIN_PATH defaults to herdr"},
		"service ensure":  {"refuse to resume"},
		"service restore": {"no team root is required"},
	} {
		var b strings.Builder
		if err := printCommandHelp(&b, i18n.English, topic); err != nil {
			t.Fatal(err)
		}
		for _, text := range wanted {
			if !strings.Contains(b.String(), text) {
				t.Errorf("%s: missing %q", topic, text)
			}
		}
	}
}

func TestDetailedHelpPropagatesOutputError(t *testing.T) {
	if err := printCommandHelp(failedHelpWriter{}, i18n.English, "send"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}
