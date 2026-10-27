package queueui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// 按瀏覽、篩選、設置、其他分組；每個快捷鍵只列一次。
var shortcutEntries = [][3]string{
	{"j/k, ↓/↑", "Select next/previous message", "Browsing"},
	{"u/d", "List half-page up/down", "Browsing"},
	{"gg", "Jump to first loaded message", "Browsing"},
	{"G (Shift+g)", "Jump to last loaded message", "Browsing"},
	{"U/D (Shift+u/d)", "Body page up/down", "Browsing"},
	{"[ / ]", "Metadata page up/down", "Browsing"},
	{"r", "Refresh messages only", "Browsing"},
	{"Ctrl+F", "Open filters", "Filters"},
	{"P (Shift+p)", "Clear applied filters", "Filters"},
	{"Ctrl+P", "Open Preferences", "Settings"},
	{"Ctrl+R", "Reload settings, not messages", "Settings"},
	{"Ctrl+W", "Toggle one-second auto refresh", "Settings"},
	{"?", "Open/close this help", "Other"},
	{"q / Ctrl+C", "Quit UI", "Other"},
}

func (m *Model) helpDocument() []string {
	width := m.helpInnerWidth()
	var lines []string
	group := ""
	for _, entry := range shortcutEntries {
		if group != entry[2] {
			if group != "" {
				lines = append(lines, strings.Repeat(" ", width))
			}
			group = entry[2]
			lines = append(lines, m.styles.heading.Bold(true).Render(cell(m.text(group), width)))
		}
		description := m.text(entry[1])
		keyWidth := 16
		descWidth := width - keyWidth - 2
		if width < 36 {
			lines = append(lines, m.styles.accent.Render(cell(entry[0], width)))
			for _, line := range strings.Split(ansi.Wrap(description, max(1, width-1), " /"), "\n") {
				lines = append(lines, m.styles.muted.Render(cell(" "+line, width)))
			}
		} else {
			for i, line := range strings.Split(ansi.Wrap(description, descWidth, " /"), "\n") {
				keyText := ""
				if i == 0 {
					keyText = entry[0]
				}
				lines = append(lines, m.styles.accent.Render(cell(keyText, keyWidth))+"  "+m.styles.muted.Render(cell(line, descWidth)))
			}
		}
	}
	return lines
}

// 外框目標62×27；小終端才縮小，內容不足也保留高度與底部提示。
func (m *Model) helpOuterWidth() int {
	if m.width < 40 {
		return m.tableWidth()
	}
	return min(62, m.tableWidth()-4)
}
func (m *Model) helpPadding() int {
	if m.width < 40 {
		return 0
	}
	return 1
}
func (m *Model) helpInnerWidth() int { return max(1, m.helpOuterWidth()-2-2*m.helpPadding()) }
func (m *Model) helpOuterHeight() int {
	available := m.contentHeight()
	if available >= 11 {
		available -= 2
	}
	return min(available, 27)
}
func (m *Model) helpPageHeight() int { return max(1, m.helpOuterHeight()-5) }
func (m *Model) clampHelp() {
	if m.helpOpen {
		m.helpOffset = max(0, min(m.helpOffset, max(0, len(m.helpDocument())-m.helpPageHeight())))
	}
}
func (m *Model) helpKey(key string) {
	switch key {
	case "esc":
		m.helpOpen = false
	case "j", "down":
		m.helpOffset++
	case "k", "up":
		m.helpOffset--
	case "u", "pgup":
		m.helpOffset -= m.helpPageHeight()
	case "d", "pgdown":
		m.helpOffset += m.helpPageHeight()
	case "g", "home":
		m.helpOffset = 0
	case "G", "shift+g", "end":
		m.helpOffset = max(0, len(m.helpDocument())-m.helpPageHeight())
	}
}
func (m *Model) helpContent() []string {
	width := m.helpInnerWidth()
	title := m.text("Keyboard shortcuts")
	if ansi.StringWidth(title) > width {
		title = "?"
	}
	lines := []string{m.styles.title.Render(cell(title, width)), strings.Repeat(" ", width)}
	document := m.helpDocument()
	offset := max(0, min(m.helpOffset, max(0, len(document)-m.helpPageHeight())))
	for i := 0; i < m.helpPageHeight(); i++ {
		row := ""
		if offset+i < len(document) {
			row = document[offset+i]
		}
		lines = append(lines, trustedCell(row, width))
	}
	lines = append(lines, m.styles.muted.Render(cell(m.text("ESC close"), width)))
	return lines
}
func (m *Model) helpPanel() string {
	panel := m.styles.dialog.Padding(0, m.helpPadding()).Border(lipgloss.RoundedBorder()).BorderForeground(m.styles.border.GetForeground()).Render(strings.Join(m.helpContent(), "\n"))
	bg := m.styles.dialog.GetBackground()
	if m.noColor || bg == nil {
		return panel
	}
	// 內嵌樣式reset與補齊空白不可漏回畫布底色；只統一面板底，不改文字前景。
	lines := uv.NewStyledString(panel).Lines(ansi.GraphemeWidth)
	rendered := make([]string, len(lines))
	for y, line := range lines {
		for x := range line {
			if !line[x].IsZero() {
				line[x].Style.Bg = bg
			}
		}
		rendered[y] = line.Render()
	}
	return strings.Join(rendered, "\n")
}
func (m *Model) helpOverlay(content []string) []string {
	panel := m.helpPanel()
	base := lipgloss.NewLayer(strings.Join(content, "\n"))
	overlay := lipgloss.NewLayer(panel).X((m.tableWidth() - m.helpOuterWidth()) / 2).Y((len(content) - m.helpOuterHeight()) / 2).Z(2)
	lines := strings.Split(lipgloss.NewCompositor(base, overlay).Render(), "\n")
	for i := range lines {
		lines[i] = trustedCell(lines[i], m.tableWidth())
	}
	return lines
}
