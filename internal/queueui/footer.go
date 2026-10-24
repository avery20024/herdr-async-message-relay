package queueui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m *Model) staleText() string {
	if m.width < 40 {
		return m.text("STALE")
	}
	return m.text("DATA NOT UPDATED")
}

func (m *Model) helpChoices() []string {
	if m.preferencesOpen {
		return []string{m.text("↑↓ field ←→ value Enter save Esc cancel ^R reload"), "↑↓ ←→ Enter Esc ^R", "↑↓←→ Enter Esc"}
	}
	if m.popup {
		return []string{m.text("↑↓ field ←→ value d clear P clear-all Enter apply Esc cancel"), "↑↓ ←→ d P Enter Esc", "↑↓←→dP Enter Esc"}
	}
	return []string{
		m.text("j/k select u/d list U/D body [] data r refresh P clear filters ^F filters ^P prefs ^R config ^W auto q"),
		m.text("j/k u/d U/D [] r P clear filters ^F filter ^P prefs ^R reload ^W auto q"),
		"j/k u/d U/D [] r P ^FPRW q",
		"jk ud UD [] r P ^FPRWq",
		"jkudUD[]rP ^FPRWq",
	}
}

// 預留spinner一欄，busy不會改變模式寬或使快捷鍵每秒跳動。
func (m *Model) footerLine() string {
	mode := m.text("Manual")
	if m.auto {
		mode = m.text("Auto (1s)")
	}
	if m.width < 40 {
		mode = "M"
		if m.auto {
			mode = "A"
		}
	} else {
		mode = " " + mode
	}
	indicator := " "
	if m.busy && !(m.width < 40 && (m.popup || m.preferencesOpen)) {
		indicator = m.loading.View()
	}
	mode += indicator
	if m.width >= 40 {
		mode += " "
	}
	remaining := m.width - ansi.StringWidth(mode) - 1
	choices := m.helpChoices()
	help := choices[len(choices)-1]
	for _, candidate := range choices {
		if ansi.StringWidth(candidate) <= remaining {
			help = candidate
			break
		}
	}
	prefix := m.styles.mode.Render(mode) + " " + m.styles.muted.Render(help)
	tail := fmt.Sprintf("%d/%d", min(m.selected+1, len(m.rows)), len(m.rows))
	if m.next > 0 {
		tail += "+"
	}
	if m.popup {
		tail = fmt.Sprintf("%s %d/3", m.text("Filters"), m.field+1)
	}
	if m.preferencesOpen {
		tail = fmt.Sprintf("%s %d/2", m.text("Preferences"), m.preferenceField+1)
	}
	tailStyle := m.styles.muted
	if m.lastError != "" {
		tail = m.staleText() + ": " + m.lastError
		tailStyle = m.styles.failure
	}
	if m.configError != "" {
		tail = "! CFG: " + m.configError
		tailStyle = m.styles.failure
	} else if m.lastError == "" && !m.popup && !m.preferencesOpen {
		if m.notice != "" {
			tail = m.notice
		} else if m.configNotice != "" {
			tail = m.configNotice
		}
	}
	available := m.width - ansi.StringWidth(prefix)
	if available > 1 {
		tail = ansi.Truncate(strings.ReplaceAll(clean(tail), "\n", " "), available-1, "…")
		prefix += strings.Repeat(" ", available-ansi.StringWidth(tail)) + tailStyle.Render(tail)
	}
	return trustedCell(prefix, m.width)
}
