package queueui

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestYankCombinationsUseOriginalSnapshot(t *testing.T) {
	for _, combo := range []struct{ key, value string }{{"y", "full\n原文\x1b]52;c;malicious\a"}, {"i", "msg-selected"}, {"b", "batch-selected"}, {"s", "dev"}, {"r", "gm"}} {
		t.Run(combo.key, func(t *testing.T) {
			m, _ := retryModel(t)
			m.rows[0].Body = "full\n原文\x1b]52;c;malicious\a"
			m.rows[0].Sender = "dev"
			calls := []string{}
			m.clipboardWrite = func(_ context.Context, s string) error { calls = append(calls, s); return nil }
			_, cmd := m.Update(key("y"))
			if cmd != nil || !m.yankOpen || len(calls) != 0 {
				t.Fatal("first key wrote clipboard")
			}
			m.rows[0].ID = "different"
			m.rows[0].Body = "different"
			m.rows[0].Recipient = "different"
			_, cmd = m.Update(key(combo.key))
			if cmd == nil || m.yankOpen || !m.copyBusy {
				t.Fatal("missing copy command")
			}
			m.Update(key("y"))
			if m.yankOpen {
				t.Fatal("duplicate copy while pending")
			}
			m.Update(cmd())
			if len(calls) != 1 || calls[0] != combo.value || m.copyBusy {
				t.Fatal("incorrect snapshot or duplicate write")
			}
			if m.notice != m.text("Copied via pbcopy.") {
				t.Fatal(m.notice)
			}
		})
	}
}

func TestYankCancelMissingAndModalIsolation(t *testing.T) {
	for _, cancel := range []string{"esc", "enter", "q", "j", "ctrl+enter"} {
		m, _ := retryModel(t)
		m.Update(key("y"))
		selected := m.selected
		_, cmd := m.Update(key(cancel))
		if cmd != nil || m.yankOpen || m.retryOpen || m.selected != selected {
			t.Fatal("invalid key passed through modal", cancel)
		}
	}
	m, _ := retryModel(t)
	m.rows[0].BatchID = sql.NullString{}
	m.Update(key("y"))
	_, cmd := m.Update(key("b"))
	if cmd != nil || m.copyBusy {
		t.Fatal("missing batch overwrote clipboard")
	}
	m.rows = nil
	m.Update(key("y"))
	if m.yankOpen {
		t.Fatal("no message to copy")
	}
	m, _ = retryModel(t)
	m.openRetry()
	_, cmd = m.Update(key("y"))
	if cmd == nil || m.yankOpen || !m.retryBusy {
		t.Fatal("yank intercepted retry confirmation")
	}
	m, _ = retryModel(t)
	m.helpOpen = true
	m.Update(key("y"))
	if m.yankOpen {
		t.Fatal("help leaked y")
	}
	m.helpOpen = false
	m.preferencesOpen = true
	m.Update(key("y"))
	if m.yankOpen {
		t.Fatal("preferences leaked y")
	}
	m.preferencesOpen = false
	m.popup = true
	m.Update(key("y"))
	if m.yankOpen {
		t.Fatal("filters leaked y")
	}
}

func TestYankPanelAllChoicesCenteredAndBounded(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, size := range [][2]int{{120, 35}, {60, 20}, {20, 10}} {
			m, _ := retryModel(t)
			m.language = lang
			m.width, m.height = size[0], size[1]
			m.Update(key("y"))
			panel := ansi.Strip(m.yankPanel())
			for _, choice := range yankChoices {
				if !strings.Contains(panel, choice[0]+"  "+m.text(choice[1])) {
					t.Fatalf("missing %s in %q", choice[0], panel)
				}
			}
			if lipgloss.Width(panel) > m.tableWidth() || lipgloss.Height(panel) > m.contentHeight() {
				t.Fatal("panel overflow", size, panel)
			}
			view := ansi.Strip(m.View().Content)
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("view overflow", size)
				}
			}
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("view height overflow")
			}
			lines := strings.Split(view, "\n")
			found := false
			expectedX := 1 + (m.tableWidth()-lipgloss.Width(panel))/2
			for _, line := range lines {
				if x := strings.Index(line, "╭"); x >= 0 {
					if ansi.StringWidth(line[:x]) != expectedX {
						t.Fatal("not centered", line)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("central box missing")
			}
		}
	}
}

func TestOSC52CapabilityAndFallback(t *testing.T) {
	for _, c := range []string{"", "Ms", "Ms=", "RGB", "NotMs=\x1b]52;anything", "Ms=unknown"} {
		if supportsOSC52(c) {
			t.Fatal("false OSC52 detection", c)
		}
	}
	for _, c := range []string{"Ms=\x1b]52;%p1%s;%p2%s\a", `Ms=\E]52;%p1%s;%p2%s\007`, "RGB;Ms=\x1b]52;%p1%s;%p2%s\a"} {
		if !supportsOSC52(c) {
			t.Fatal("missing OSC52 capability")
		}
	}
	m, _ := retryModel(t)
	calls := 0
	m.clipboardWrite = func(_ context.Context, _ string) error { calls++; return errors.New("unavailable") }
	m.Update(tea.CapabilityMsg{Content: "RGB"})
	m.Update(key("y"))
	_, cmd := m.Update(key("i"))
	m.Update(cmd())
	if calls != 1 || !strings.Contains(m.notice, m.text("Copy failed; clipboard was not confirmed.")) {
		t.Fatal("fallback error hidden")
	}
	m.Update(tea.CapabilityMsg{Content: "Ms=\x1b]52;%p1%s;%p2%s\a"})
	if !m.clipboardOSC52 {
		t.Fatal("ignored positive capability")
	}
}

type yankProgram struct {
	m   *Model
	cmd tea.Cmd
}

func (p yankProgram) Init() tea.Cmd { return p.cmd }
func (p yankProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := p.m.Update(msg)
	if _, ok := msg.(copiedMsg); ok {
		return p, tea.Quit
	}
	return p, cmd
}
func (p yankProgram) View() tea.View { return tea.NewView("") }

func TestYankOSC52EncodesPayloadWithoutPBCopy(t *testing.T) {
	m, _ := retryModel(t)
	value := "中文\n\x1b]52;c;injection\a"
	m.rows[0].Body = value
	m.clipboardWrite = func(context.Context, string) error { t.Error("pbcopy used despite OSC52 support"); return nil }
	m.Update(tea.CapabilityMsg{Content: "Ms=\x1b]52;%p1%s;%p2%s\a"})
	m.Update(key("y"))
	_, cmd := m.Update(key("y"))
	var out bytes.Buffer
	p := tea.NewProgram(yankProgram{m, cmd}, tea.WithInput(nil), tea.WithOutput(&out), tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), ansi.SetSystemClipboard(value)) || strings.Contains(out.String(), value) {
		t.Fatal("clipboard not safely encoded")
	}
	if m.notice != m.text("OSC 52 copy request sent; paste to verify.") {
		t.Fatal(m.notice)
	}
}

func TestPBCopyUsesExactStdinAndReportsErrors(t *testing.T) {
	// PATH只含假程式，測試絕不碰真實剪貼簿。
	dir := t.TempDir()
	dest := filepath.Join(dir, "captured")
	fake := filepath.Join(dir, "pbcopy")
	t.Setenv("PATH", dir)
	t.Setenv("COPY_DEST", dest)
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n/bin/cat > \"$COPY_DEST\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	value := "first\n原文\n$(touch unwanted);\x00\x1b[31m\n"
	if err := writePBCopy(t.Context(), value); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != value {
		t.Fatal("stdin changed", err)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'private error' >&2\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := writePBCopy(t.Context(), "sensitive"); err == nil || strings.Contains(err.Error(), "private error") {
		t.Fatal("failed clipboard or stderr leaked")
	}
	if err := os.Remove(fake); err != nil {
		t.Fatal(err)
	}
	if err := writePBCopy(t.Context(), "x"); err == nil {
		t.Fatal("missing pbcopy not reported")
	}
}

func TestPBCopyCancellation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := os.WriteFile(filepath.Join(dir, "pbcopy"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := writePBCopy(ctx, "x"); err == nil {
		t.Fatal("cancelled clipboard write succeeded")
	}
}
