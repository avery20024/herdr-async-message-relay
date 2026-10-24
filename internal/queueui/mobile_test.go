package queueui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestMobileBoundsAndAllControls(t *testing.T) {
	m, _ := ready(t)
	m.rows[0].Sender = "手機中文寄件角色"
	m.rows[0].Body = "\x1b]52;c;PWNED\a中文🙂\n" + strings.Repeat("body line\n", 30)
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		m.language = lang
		for _, mono := range []bool{false, true} {
			m.noColor = mono
			m.refreshTheme()
			for _, size := range [][2]int{{20, 10}, {24, 12}, {30, 16}, {40, 20}, {60, 24}, {73, 16}, {74, 10}, {74, 16}, {99, 16}, {100, 16}, {136, 44}, {170, 44}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for _, state := range []string{"normal", "popup", "busy", "stale", "empty"} {
					rows := m.rows
					m.popup = state == "popup"
					m.busy = state == "busy"
					m.lastError = ""
					if state == "stale" {
						m.lastError = strings.Repeat("offline ", 20)
					}
					if state == "empty" {
						m.rows = nil
					}
					view := m.View().Content
					lines := strings.Split(view, "\n")
					if len(lines) != size[1] {
						t.Fatalf("height %v %s: %d", size, state, len(lines))
					}
					for _, line := range lines {
						if ansi.StringWidth(line) != size[0] {
							t.Fatalf("width %v %s: %d %q", size, state, ansi.StringWidth(line), line)
						}
					}
					plain := ansi.Strip(view)
					if strings.Contains(view, "\x1b]52") || strings.Contains(plain, "PWNED") || strings.Contains(plain, "Minimum") {
						t.Fatal("unsafe or blocked mobile view")
					}
					if state == "stale" && !strings.Contains(plain, m.staleText()) {
						t.Fatal("stale warning hidden on phone")
					}
					help := lines[len(lines)-1]
					if m.popup {
						if !strings.Contains(help, "Enter") || !strings.Contains(help, "Esc") {
							t.Fatal("popup lost apply/cancel help")
						}
					} else {
						for _, keys := range []string{"ud", "UD", "[]", "^F", "^P", "^R", "^W", "q"} {
							normalized := strings.ReplaceAll(strings.ReplaceAll(help, "/", ""), "^FPRW", "^F ^P ^R ^W")
							if m.mobile() && !strings.Contains(normalized, keys) {
								t.Fatalf("missing phone key %s in %q", keys, help)
							}
						}
					}
					m.rows = rows
				}
				m.popup, m.busy, m.lastError = false, false, ""
			}
		}
	}
}

func TestMobileCardFieldsAndSummaryOnly(t *testing.T) {
	m, _ := ready(t)
	original := "  FIRST\n\tsecond   third  "
	m.rows[0].Body = original
	for _, width := range []int{20, 40, 73, 99} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		lines := m.tableLines()
		first := ansi.Strip(lines[0])
		if !strings.Contains(first, "dev") || !strings.Contains(first, "gm") || !strings.Contains(first, "→") || !strings.Contains(first, "▌") {
			t.Fatalf("mobile route missing: %q", first)
		}
		if !strings.Contains(first, "○Q") && !strings.Contains(first, "○ queued") {
			t.Fatal("state lacks text fallback")
		}
		if strings.Contains(first, "Sender") {
			t.Fatal("desktop column header on phone")
		}
		if m.rows[0].Body != original || !strings.Contains(strings.Join(m.detailLines(), "\n"), "FIRST\n") {
			t.Fatal("summary altered full body")
		}
		if width >= 40 && !strings.Contains(ansi.Strip(lines[1]), "FIRST second third") {
			t.Fatal("summary whitespace not normalized")
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if !strings.Contains(ansi.Strip(m.tableLines()[0]), "Sender") {
		t.Fatal("desktop six-column header replaced")
	}
	if summaryText(original) != "FIRST second third" {
		t.Fatal("summary normalization")
	}
}

func TestMobileHalfPageLazyAndResize(t *testing.T) {
	for _, size := range [][2]int{{20, 10}, {40, 20}, {60, 24}} {
		m, _ := ready(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		visible := m.tableHeight() / 2
		if m.visibleMessages() != visible || m.listHalfPage() != max(1, visible/2) {
			t.Fatal("half-page counts screen lines instead of messages")
		}
		m.Update(key("d"))
		if m.selected != max(1, visible/2) || m.top != m.selected {
			t.Fatal("card half-page distance")
		}
		m.Update(key("u"))
		if m.selected != 0 || m.top != 0 {
			t.Fatal("card reverse half-page")
		}
		m.selected = 49
		m.clampLayout()
		oldIndex, oldTop := m.selected, m.top
		_, cmd := m.Update(key("d"))
		if cmd == nil {
			t.Fatal("missing mobile lazy page")
		}
		apply(m, cmd)
		if m.selected != oldIndex+m.listHalfPage() || m.top != oldTop+m.listHalfPage() {
			t.Fatal("mobile lazy boundary lost selection distance")
		}
		id := m.selectedID()
		for _, nextSize := range [][2]int{{170, 44}, {20, 10}, {73, 16}, {99, 16}, {100, 16}, {40, 20}} {
			m.Update(tea.WindowSizeMsg{Width: nextSize[0], Height: nextSize[1]})
			if m.selectedID() != id || m.selected < m.top || m.selected >= m.top+m.visibleMessages() {
				t.Fatal("resize lost selection or hid card")
			}
			if m.metadataWidth() != (nextSize[0]-3)*4/10 || m.metadataWidth()+m.detailWidth() != nextSize[0]-3 {
				t.Fatal("mobile changed 40/60")
			}
		}
	}
}

func TestMobileMetadataReachabilityAndIndependentOffsets(t *testing.T) {
	for _, size := range [][2]int{{20, 10}, {24, 12}, {40, 20}, {73, 16}} {
		m, f := ready(t)
		f.metadataKey = strings.Repeat("long-key-", 20) + "LAST"
		m.rows[0].IdempotencyKey = f.metadataKey
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(key("D"))
		bodyOffset, id, selected, top := m.detailOffset, m.selectedID(), m.selected, m.top
		seen := map[string]bool{}
		for {
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			start := m.tableHeight() + 2
			for _, line := range lines[start : start+m.detailPageHeight()] {
				seen[strings.TrimSpace(ansi.Cut(line, 1, 1+m.metadataWidth()))] = true
			}
			old := m.metadataOffset
			m.Update(key("]"))
			if old == m.metadataOffset {
				break
			}
		}
		for _, line := range m.metadataLines() {
			if !seen[strings.TrimSpace(line)] {
				t.Fatalf("unreachable mobile metadata %v: %q", size, line)
			}
		}
		if m.detailOffset != bodyOffset || m.selectedID() != id || m.selected != selected || m.top != top {
			t.Fatal("metadata paging changed other region")
		}
		offset := m.metadataOffset
		_, cmd := m.Update(key("r"))
		apply(m, cmd)
		if m.metadataOffset != offset || m.detailOffset != bodyOffset {
			t.Fatal("mobile refresh lost offsets")
		}
		m.Update(key("ctrl+f"))
		m.Update(key("["))
		m.Update(key("U"))
		if m.metadataOffset != offset || m.detailOffset != bodyOffset {
			t.Fatal("mobile popup leaked region keys")
		}
		m.Update(key("esc"))
		m.Update(key("j"))
		if m.metadataOffset != 0 || m.detailOffset != 0 {
			t.Fatal("new mobile selection did not reset details")
		}
	}
}

func TestUnknownBackgroundUsesDefaultForeground(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	m, _ := ready(t)
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	if m.backgroundKnown || m.styles.muted.Render("muted") != "muted" {
		t.Fatal("unknown background assumes light foreground")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if !m.backgroundKnown || m.dark || m.styles.muted.Render("muted") == "muted" {
		t.Fatal("light reply did not activate palette")
	}
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.Ascii})
	if m.styles.muted.Render("muted") != "muted" {
		t.Fatal("NO_COLOR emitted muted color")
	}
}

func TestUnknownBackgroundStatesUseDefaultForegroundAcrossProfiles(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	plainBold := lipgloss.NewStyle().Bold(true)
	for _, noColor := range []string{"", "1"} {
		t.Setenv("NO_COLOR", noColor)
		for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.Ascii} {
			m, _ := ready(t)
			for _, size := range [][2]int{{20, 10}, {40, 20}, {120, 30}} {
				m.Update(tea.ColorProfileMsg{Profile: profile})
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.Update(key("j"))
				m.Update(key("k"))
				for _, state := range []string{"queued", "dispatching", "submitted", "uncertain"} {
					label := stateLabel(state)
					got := m.styles.states[state].Render(label)
					if got != plainBold.Render(label) {
						t.Fatalf("unknown background colored state: profile=%v NO_COLOR=%q state=%s got=%q", profile, noColor, state, got)
					}
				}
			}
			for _, bg := range []color.Color{color.White, color.Black} {
				m.Update(tea.BackgroundColorMsg{Color: bg})
				m.Update(tea.ColorProfileMsg{Profile: profile})
				for _, state := range []string{"queued", "dispatching", "submitted", "uncertain"} {
					label := stateLabel(state)
					got := m.styles.states[state].Render(label)
					if !m.noColor && got == plainBold.Render(label) {
						t.Fatal("known background did not restore state palette")
					}
					if m.noColor && got != label && got != plainBold.Render(label) {
						t.Fatal("NO_COLOR restored state color")
					}
				}
			}
		}
	}
}

func TestExtremeSizeStillAllowsQuit(t *testing.T) {
	m, _ := ready(t)
	for _, size := range [][2]int{{1, 1}, {19, 10}, {20, 9}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > size[1] {
			t.Fatal("tiny view height overflow")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("tiny view width overflow")
			}
		}
	}
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Fatal("tiny view trapped quit")
	}
}
