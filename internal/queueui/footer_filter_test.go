package queueui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterClearFilterHelpIsExplicit(t *testing.T) {
	m, _ := ready(t)
	for _, tc := range []struct {
		language i18n.Language
		want     string
	}{
		{i18n.English, "P clear filters"},
		{i18n.TraditionalChinese, "P 清除篩選"},
	} {
		m.language = tc.language
		for _, width := range []int{100, 168, 180} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, tc.want) {
				t.Fatalf("width %d missing explicit filter help %q: %q", width, tc.want, footer)
			}
		}
	}
}

func TestOneFooterModeAndKeysAllSizes(t *testing.T) {
	m, _ := ready(t)
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		m.language = lang
		for _, mono := range []bool{false, true} {
			m.noColor = mono
			m.refreshTheme()
			for _, width := range []int{20, 24, 30, 40, 67, 74, 99, 100, 170} {
				m.Update(tea.WindowSizeMsg{Width: width, Height: 16})
				for _, auto := range []bool{false, true} {
					m.auto = auto
					for _, popup := range []bool{false, true} {
						m.popup = popup
						for _, busy := range []bool{false, true} {
							m.busy = busy
							for _, stale := range []bool{false, true} {
								m.lastError = ""
								if stale {
									m.lastError = "socket unavailable"
								}
								lines := strings.Split(ansi.Strip(m.View().Content), "\n")
								if len(lines) != 16 || !strings.HasPrefix(lines[len(lines)-2], "└") {
									t.Fatal("more than one footer row")
								}
								footer := lines[len(lines)-1]
								if ansi.StringWidth(footer) != width {
									t.Fatalf("footer overflow %d: %q", width, footer)
								}
								mode := m.text("Manual")
								if auto {
									mode = m.text("Auto (1s)")
								}
								if width < 40 {
									mode = "M"
									if auto {
										mode = "A"
									}
								}
								prefix := " " + mode
								if width < 40 {
									prefix = mode
								}
								if !strings.HasPrefix(footer, prefix) {
									t.Fatalf("mode not alongside help: %q", footer)
								}
								if popup {
									for _, key := range []string{"d", "P", "Enter", "Esc"} {
										if !strings.Contains(footer, key) {
											t.Fatalf("popup key missing %s: %q", key, footer)
										}
									}
								} else {
									normalized := strings.ReplaceAll(strings.ReplaceAll(footer, "/", ""), "^FPRW", "^F ^P ^R ^W")
									for _, key := range []string{"jk", "ud", "UD", "[]", "r", "P", "^F", "^P", "^R", "^W", "q"} {
										if !strings.Contains(normalized, key) {
											t.Fatalf("main key missing %s: %q", key, footer)
										}
									}
								}
								if stale && !strings.Contains(strings.Join(lines, "\n"), m.staleText()) {
									t.Fatal("single-footer hid stale warning")
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestFilterDraftClearOneAllCancelAndApply(t *testing.T) {
	full := Filter{Sender: "dev", Recipient: "gm", State: "queued"}
	for field := 0; field < 3; field++ {
		m, f := ready(t)
		m.filter = full
		m.Update(key("ctrl+f"))
		m.field = field
		calls := len(f.calls)
		_, cmd := m.Update(key("d"))
		want := full
		switch field {
		case 0:
			want.Sender = ""
		case 1:
			want.Recipient = ""
		case 2:
			want.State = ""
		}
		if cmd != nil || m.draft != want || m.filter != full || len(f.calls) != calls {
			t.Fatal("d changed other field or applied draft")
		}
		m.Update(key("esc"))
		if m.filter != full || len(f.calls) != calls {
			t.Fatal("Esc did not cancel cleared draft")
		}
		m.Update(key("ctrl+f"))
		m.field = field
		m.Update(key("d"))
		_, cmd = m.Update(key("enter"))
		apply(m, cmd)
		if m.filter != want || f.filters[len(f.filters)-1] != want {
			t.Fatal("Enter did not apply single cleared field")
		}
		m.filter = full
		m.Update(key("ctrl+f"))
		m.field = field
		m.Update(tea.KeyPressMsg{Code: 'p', Text: "P", Mod: tea.ModShift, ShiftedCode: 'P'})
		if m.draft != (Filter{}) || m.filter != full {
			t.Fatal("Shift+P did not clear draft only")
		}
		m.Update(key("D"))
		if m.filter != full || m.draft != (Filter{}) {
			t.Fatal("D changed filters")
		}
		_, cmd = m.Update(key("enter"))
		apply(m, cmd)
		if m.filter != (Filter{}) || f.filters[len(f.filters)-1] != (Filter{}) {
			t.Fatal("all-clear draft did not apply")
		}
	}
}

func TestMainShiftPCancelsOldRequestAndLeavesBodyD(t *testing.T) {
	m, f := ready(t)
	m.filter = Filter{Sender: "dev"}
	m.Update(key("D"))
	if m.detailOffset == 0 || m.filter.Sender != "dev" {
		t.Fatal("D no longer pages body")
	}
	m.Update(key("]"))
	m.top = 10
	m.selected = 12
	_, old := m.Update(key("r"))
	token := m.token
	_, clear := m.Update(key("P"))
	if clear == nil || m.token == token || m.filter != (Filter{}) || m.next != 0 || m.selected != 0 || m.top != 0 || m.metadataOffset != 0 || m.detailOffset != 0 {
		t.Fatal("main P did not reset/cancel filtered state")
	}
	apply(m, old)
	if len(m.rows) != 0 || !m.busy {
		t.Fatal("old filtered response overrode clear")
	}
	apply(m, clear)
	if m.busy || f.filters[len(f.filters)-1] != (Filter{}) || m.selectedID() != "125" {
		t.Fatal("clear did not load unfiltered first page")
	}
	calls := len(f.calls)
	if _, cmd := m.Update(key("P")); cmd != nil || len(f.calls) != calls {
		t.Fatal("already clear P issued request")
	}
	m.Update(key("ctrl+f"))
	if !m.popup {
		t.Fatal("Ctrl+F did not open filters")
	}
}
