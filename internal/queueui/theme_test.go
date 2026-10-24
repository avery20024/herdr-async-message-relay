package queueui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestStyledViewsProfilesAndPopupBounds(t *testing.T) {
	m, _ := ready(t)
	m.rows[0].Body = "\x1b]52;c;PWNED\a中文🙂\n" + strings.Repeat("text\n", 30)
	m.rows[0].Sender = "全形中文角色"
	for _, mono := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			m.noColor, m.dark = mono, dark
			m.styles = newTheme(dark, mono)
			for _, size := range [][2]int{{170, 44}, {136, 44}, {80, 24}, {74, 16}, {201, 40}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for _, popup := range []bool{false, true} {
					m.popup = popup
					view := m.View().Content
					lines := strings.Split(view, "\n")
					if len(lines) != size[1] {
						t.Fatal("styled view height mismatch")
					}
					for _, line := range lines {
						if ansi.StringWidth(line) != size[0] {
							t.Fatalf("styled width mismatch %v: %d %q", size, ansi.StringWidth(line), line)
						}
					}
					if strings.Contains(view, "PWNED") || strings.Contains(view, "\x1b]52") {
						t.Fatal("untrusted sequence reached styled view")
					}
					if mono && (strings.Contains(view, "38;") || strings.Contains(view, "48;")) {
						t.Fatal("NO_COLOR emitted RGB or indexed colors")
					}
					if !popup && !strings.Contains(ansi.Strip(view), "▌") {
						t.Fatal("selection only expressed by color")
					}
				}
			}
		}
	}
	m.popup = false
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if m.dark {
		t.Fatal("light background not detected")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if !m.dark {
		t.Fatal("dark background not detected")
	}
}

func TestNoColorEnvAndProfile(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "1")
	m, _ := ready(t)
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	if !m.noColor {
		t.Fatal("profile ignored NO_COLOR")
	}
	t.Setenv("NO_COLOR", "")
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.Ascii})
	if !m.noColor {
		t.Fatal("ASCII profile still colored")
	}
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	if m.noColor {
		t.Fatal("truecolor did not recover")
	}
}

func TestSpinnerOnlyRunsForCurrentRequest(t *testing.T) {
	m, f := ready(t)
	old := m.loading.Tick().(spinner.TickMsg)
	if _, cmd := m.Update(old); cmd != nil {
		t.Fatal("idle spinner scheduled work")
	}
	_, request := m.Update(key("r"))
	current := m.loading.Tick().(spinner.TickMsg)
	calls := len(f.calls)
	if _, cmd := m.Update(old); cmd != nil {
		t.Fatal("old generation kept spinning")
	}
	_, tick := m.Update(current)
	if tick == nil || len(f.calls) != calls {
		t.Fatal("spinner did not animate or fetched data")
	}
	apply(m, request)
	if m.busy {
		t.Fatal("request did not finish")
	}
	if _, cmd := m.Update(tick()); cmd != nil {
		t.Fatal("completed request kept spinner alive")
	}
	_, request = m.Update(key("r"))
	cancelled := m.loading.Tick()
	m.cancelFetch()
	if _, cmd := m.Update(cancelled); cmd != nil {
		t.Fatal("cancelled request kept spinner alive")
	}
	// 取消後即使已排程的請求有結果，token也不得套用；不得產生新的HTTP請求輪詢。
	apply(m, request)
	if m.busy {
		t.Fatal("cancelled response restarted request")
	}
}

func TestViewportDisablesCompetingInput(t *testing.T) {
	v := textViewport([]string{"one", "two", "three"}, 10, 1, 1)
	before := v.YOffset()
	for _, name := range []string{"j", "k", "d", "u", "D", "U"} {
		v, _ = v.Update(key(name))
	}
	if v.YOffset() != before || v.MouseWheelEnabled {
		t.Fatal("viewport accepted unassigned default keys")
	}
	v.PageDown()
	if v.YOffset() != 2 {
		t.Fatal("viewport page API not in use")
	}
}
