package queueui

import (
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

type palette struct {
	name, short                     string
	light                           bool
	ink, muted, accent, base, panel string
}

// 官方色票來源列於docs/ui.md；主文字／弱化／強調與兩種著色背景共用語意角色。
var palettes = map[string]palette{
	"catppuccin-latte": {name: "Catppuccin Latte", short: "Latte", light: true, ink: "#4C4F69", muted: "#6C6F85", accent: "#8839EF", base: "#EFF1F5", panel: "#CCD0DA"},
	"dracula":          {name: "Dracula", short: "Dracula", ink: "#F8F8F2", muted: "#6272A4", accent: "#BD93F9", base: "#282A36", panel: "#44475A"},
	"rose-pine":        {name: "Rosé Pine", short: "Rosé Pine", ink: "#E0DEF4", muted: "#908CAA", accent: "#C4A7E7", base: "#191724", panel: "#26233A"},
	"vesper":           {name: "Vesper", short: "Vesper", ink: "#FFFFFF", muted: "#A0A0A0", accent: "#FFC799", base: "#101010", panel: "#232323"},
}
var themeIDs = []string{"terminal", "catppuccin-latte", "dracula", "rose-pine", "vesper"}

func paletteTheme(p palette, mono bool) theme {
	if mono {
		return newTheme(true, true)
	}
	plain := lipgloss.NewStyle()
	fg := plain.Foreground(lipgloss.Color(p.ink))
	accent := plain.Foreground(lipgloss.Color(p.accent)).Bold(true)
	panel := fg.Background(lipgloss.Color(p.panel))
	highlight := plain.Foreground(lipgloss.Color(p.base)).Background(lipgloss.Color(p.accent)).Bold(true)
	t := theme{border: plain.Foreground(lipgloss.Color(p.muted)), title: panel.Bold(true), heading: panel.Bold(true).Foreground(lipgloss.Color(p.accent)), muted: plain.Foreground(lipgloss.Color(p.muted)), selected: highlight, accent: accent, warning: accent, failure: highlight, dialog: panel, mode: highlight, states: map[string]lipgloss.Style{}}
	for _, state := range []string{"queued", "dispatching", "submitted", "uncertain"} {
		t.states[state] = accent
	}
	return t
}

// 補畫布缺省色而非設定終端OSC預設色；逐cell保留選取／面板配對，reset不會漏掉Latte底色。
func (m *Model) paintCanvas(output string) string {
	p, ok := palettes[m.preferences.Theme]
	if !ok || m.noColor {
		return output
	}
	fg := lipgloss.Color(p.ink)
	bg := lipgloss.Color(p.base)
	lines := uv.NewStyledString(output).Lines(ansi.GraphemeWidth)
	result := make([]string, len(lines))
	for y, line := range lines {
		for x := range line {
			if line[x].IsZero() {
				continue
			}
			if line[x].Style.Fg == nil {
				line[x].Style.Fg = fg
			}
			if p.light && line[x].Style.Bg == nil {
				line[x].Style.Bg = bg
			}
		}
		result[y] = line.Render()
	}
	return strings.Join(result, "\n")
}
