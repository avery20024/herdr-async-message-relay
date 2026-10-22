package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

func TestOverviewBeforeProjectAndServiceSetup(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"HOME": home, "PATH": "", "DELIVER_ROOT": filepath.Join(base, "missing-project"),
		"DELIVER_STATE_DIR": filepath.Join(base, "state"), "DELIVER_POLL_MS": "invalid",
		"HERDR_RELAY_SERVICE_HOME": filepath.Join(base, "services"),
		"XDG_STATE_HOME":           filepath.Join(base, "xdg-state"), "XDG_CONFIG_HOME": filepath.Join(base, "config"),
		"HERDR_SOCKET_PATH": filepath.Join(base, "missing-herdr.sock"), "HERDR_RELAY_FROM": "",
		"LC_ALL": "zh_TW.UTF-8", "HERDR_RELAY_LANG": "zh-TW",
	} {
		t.Setenv(key, value)
	}
	for _, test := range []struct {
		name, title string
		args        []string
		flagHelp    bool
	}{
		{"English", "Commands:", []string{"--lang", "en", "help"}, false},
		{"Traditional Chinese", "指令：", []string{"--lang", "zh-TW", "help"}, false},
		{"environment", "指令：", []string{"help"}, false},
		{"auto", "指令：", []string{"--lang", "auto", "help"}, false},
		{"explicit nonexistent root", "指令：", []string{"--root", filepath.Join(base, "other-missing"), "help"}, false},
		{"flag English", "Commands:", []string{"--lang", "en", "--help"}, true},
		{"flag Chinese", "指令：", []string{"--lang", "zh-TW", "--help"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var err error
			var stderr string
			stdout := captureOutput(t, &os.Stdout, func() {
				stderr = captureOutput(t, &os.Stderr, func() { err = run(context.Background(), test.args) })
			})
			output := stdout
			if test.flagHelp {
				if !errors.Is(err, flag.ErrHelp) || stdout != "" {
					t.Fatalf("stdout=%q, error=%v", stdout, err)
				}
				output = stderr
			} else if err != nil || stderr != "" {
				t.Fatalf("stderr=%q, error=%v", stderr, err)
			}
			for _, want := range []string{
				test.title, "send [--from ROLE] [--key KEY] ROLE MESSAGE", "status", "show msg-ID|batch-ID",
				"server on|off|restart|status", "service ensure|start|stop|restart|status|restore|list",
				"cli install", "serve", "resolve --batch", "--confirm", "--note", "HERDR_SOCKET_PATH",
				"--root PATH > DELIVER_ROOT >", "HERDR_RELAY_FROM", "HERDR_RELAY_LANG",
				"accepted", "submitted", "uncertain", "herdr plugin action invoke herdr-async-message-relay.ACTION",
				"herdr plugin install avery20024/herdr-async-message-relay\n",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("missing %q in %s", want, output)
				}
			}
		})
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("help modified HOME: %v %v", entries, err)
	}
	for _, name := range []string{"missing-project", "other-missing", "state", "services", "xdg-state", "config"} {
		if _, err := os.Lstat(filepath.Join(base, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("help created %s: %v", name, err)
		}
	}
}

func TestHelpRejectsExtraArgumentsBeforeConfig(t *testing.T) {
	t.Setenv("DELIVER_ROOT", filepath.Join(t.TempDir(), "missing"))
	for _, language := range []string{"en", "zh-TW"} {
		for _, extra := range []string{"send", "--help", "unexpected"} {
			err := run(context.Background(), []string{"--lang", language, "help", extra})
			want := "usage: hrelay help"
			if language == "zh-TW" {
				want = "用法：hrelay help"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s %s: %v", language, extra, err)
			}
		}
	}
	if err := run(context.Background(), []string{"--lang", "fr", "help"}); err == nil || !strings.Contains(err.Error(), "unsupported language") {
		t.Fatal(err)
	}
}

func TestHelpCatalogAndPluginActionCoverage(t *testing.T) {
	texts := append([]string(nil), helpNotes...)
	for _, entry := range append(append([]helpEntry(nil), helpCommands...), helpActions...) {
		texts = append(texts, entry.description)
	}
	for _, text := range texts {
		if i18n.TraditionalChinese.Text(text) == text {
			t.Errorf("missing help translation: %s", text)
		}
	}
	manifest, err := os.ReadFile("../../herdr-plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`(?m)^\[\[actions\]\]\nid = "([^"]+)"`).FindAllStringSubmatch(string(manifest), -1)
	if len(matches) != len(helpActions) {
		t.Fatalf("manifest actions=%d, help actions=%d", len(matches), len(helpActions))
	}
	for _, match := range matches {
		found := false
		for _, entry := range helpActions {
			found = found || entry.syntax == match[1]
		}
		if !found {
			t.Errorf("missing plugin action %s", match[1])
		}
	}
}

type failedHelpWriter struct{}

func (failedHelpWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestHelpPropagatesOutputError(t *testing.T) {
	if err := printHelp(failedHelpWriter{}, i18n.English); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}
