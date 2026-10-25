package queueui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// 只處理已淨化的顯示文字；外來本文不可直接交給樣式或圖層。
type theme struct {
	border, title, heading, muted, selected, accent, warning, failure, dialog, mode lipgloss.Style
	states                                                                          map[string]lipgloss.Style
}

func newTheme(dark, noColor bool) theme {
	plain := lipgloss.NewStyle()
	t := theme{border: plain, title: plain.Bold(true), heading: plain.Bold(true), muted: plain,
		selected: plain.Bold(true).Reverse(true), accent: plain.Bold(true), warning: plain.Bold(true), failure: plain.Bold(true).Reverse(true), dialog: plain, mode: plain.Bold(true).Reverse(true),
		states: map[string]lipgloss.Style{}}
	if noColor {
		return t
	}
	// 有自訂底色的面板使用深色預設；未回覆背景時無底色的muted由refreshTheme保留終端預設前景。
	ink, dim, edge, panel, selection, accent := "#E2E8F0", "#A6B4C8", "#64748B", "#162033", "#244761", "#7DD3FC"
	if !dark {
		ink, dim, edge, panel, selection, accent = "#172033", "#526276", "#718096", "#F1F5F9", "#D8EAF5", "#075985"
	}
	fg := plain.Foreground(lipgloss.Color(ink))
	t.border = plain.Foreground(lipgloss.Color(edge))
	t.title = fg.Bold(true).Background(lipgloss.Color(panel))
	t.heading = plain.Bold(true).Foreground(lipgloss.Color(accent)).Background(lipgloss.Color(panel))
	t.muted = plain.Foreground(lipgloss.Color(dim))
	t.selected = fg.Bold(true).Background(lipgloss.Color(selection))
	t.accent = plain.Bold(true).Foreground(lipgloss.Color(accent))
	warningColor := "3"
	if dark {
		warningColor = "11"
	}
	t.warning = plain.Bold(true).Foreground(lipgloss.Color(warningColor))
	t.failure = plain.Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1"))
	t.dialog = fg.Background(lipgloss.Color(panel))
	t.mode = plain.Bold(true).Foreground(lipgloss.Color("#7DD3FC")).Background(lipgloss.Color("#173348"))
	for state, c := range map[string]string{"queued": "3", "dispatching": "4", "submitted": "2", "uncertain": "1"} {
		if dark {
			c = map[string]string{"3": "11", "4": "12", "2": "10", "1": "9"}[c]
		}
		t.states[state] = plain.Bold(true).Foreground(lipgloss.Color(c))
	}
	return t
}

// 背景未知時不假設淺灰在終端上可讀，也不用faint再降低預設前景的對比。
func (m *Model) refreshTheme() {
	if p, ok := palettes[m.preferences.Theme]; ok {
		m.styles = paletteTheme(p, m.noColor)
		return
	}
	m.styles = newTheme(m.dark, m.noColor)
	if m.auto && !m.noColor {
		m.styles.mode = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#86EFAC")).Background(lipgloss.Color("#163325"))
	}
	if !m.backgroundKnown {
		m.styles.muted = lipgloss.NewStyle()
		// 符號和文字也需可讀，不能只拿同樣亮色的符號當淺底備援。
		for _, state := range []string{"queued", "dispatching", "submitted", "uncertain"} {
			m.styles.states[state] = lipgloss.NewStyle().Bold(true)
		}
	}
}
func noColorEnv() bool { return os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" }
func stateLabel(state string) string {
	symbol := map[string]string{"queued": "○", "dispatching": "◐", "submitted": "✓", "uncertain": "▲"}[state]
	if symbol == "" {
		return clean(state)
	}
	return symbol + " " + state
}

// 由應用程式固定分派鍵位，元件不接受預設鍵盤或滑鼠事件，避免隱性焦點。
func textViewport(lines []string, width, height, offset int) viewport.Model {
	v := viewport.New(viewport.WithWidth(max(1, width)), viewport.WithHeight(max(1, height)))
	v.KeyMap = viewport.KeyMap{}
	v.MouseWheelEnabled = false
	v.FillHeight = true
	v.SetContentLines(append([]string(nil), lines...))
	v.SetYOffset(offset)
	return v
}
func (m *Model) scrollText(metadata, down bool) {
	lines, width, offset := m.detailLines(), m.detailWidth(), m.detailOffset
	if metadata {
		lines, width, offset = m.metadataLines(), m.metadataWidth(), m.metadataOffset
	}
	v := textViewport(lines, width, m.detailPageHeight(), offset)
	if down {
		v.PageDown()
	} else {
		v.PageUp()
	}
	if metadata {
		m.metadataOffset = v.YOffset()
	} else {
		m.detailOffset = v.YOffset()
	}
}

// trustedCell保留本程式產生的樣式；輸入必須先經clean/cell，不用於外來資料。
func trustedCell(s string, width int) string {
	s = ansi.Truncate(s, max(0, width), "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}
