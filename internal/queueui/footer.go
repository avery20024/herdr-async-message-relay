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
		return []string{m.text("↑↓ field ←→ value Enter save Esc cancel ^R reload") + " ?", "↑↓ ←→ Enter Esc ^R ?", "↑↓←→Enter Esc?"}
	}
	if m.popup {
		return []string{m.text("↑↓ field ←→ value d clear P clear-all Enter apply Esc cancel") + " ?", "↑↓ ←→ d P Enter Esc ?", "↑↓←→dPEnter Esc?"}
	}
	return []string{
		m.text("j/k select u/d list U/D body [] data r refresh P clear filters ^F filters ^P prefs ^R config ^W auto q ? help"),
		m.text("j/k u/d U/D [] r P clear filters ^F filter ^P prefs ^R reload ^W auto q ? help"),
		"j/k u/d U/D [] r P ^FPRW q ?",
		"jk ud UD [] r P ^FPRWq?",
		"jkudUD[]rP^FPRWq?",
	}
}

// 預留spinner一欄，busy不會改變模式寬或使快捷鍵每秒跳動。
func (m *Model) footerLine() string {
	// 說明只在彈窗底部顯示淡色ESC退出，不重複模式／按鍵／計數。
	if m.helpOpen {
		return strings.Repeat(" ", max(0, m.width))
	}
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
	if m.busy && !(m.width < 40 && (m.popup || m.preferencesOpen || m.helpOpen)) {
		indicator = m.loading.View()
	}
	mode += indicator
	if m.width >= 40 {
		mode += " "
	}
	tail := fmt.Sprintf("%d/%d", min(m.selected+1, len(m.rows)), len(m.rows))
	if m.next > 0 {
		tail += "+"
	}
	if m.popup {
		tail = fmt.Sprintf("%s %d/3", m.text("Filters"), m.field+1)
	}
	if m.preferencesOpen {
		tail = fmt.Sprintf("%s %d/3", m.text("Preferences"), m.preferenceField+1)
	}
	tailStyle := m.styles.muted
	if m.lastError != "" {
		tail = m.staleText() + ": " + m.lastError
		tailStyle = m.styles.failure
	}
	if m.configError != "" {
		tail = "! CFG: " + m.configError
		tailStyle = m.styles.failure
	} else if m.lastError == "" && !m.popup && !m.preferencesOpen && !m.helpOpen {
		if m.notice != "" {
			tail = m.notice
		} else if m.configNotice != "" {
			tail = m.configNotice
		}
	}
	// 先替資訊尾端保留空間，避免新提示剛好塞滿一列時吞掉計數／欄標／錯誤。
	// 最小必要鍵仍優先；只有兩者確實放不下，才依剩餘寬度截短資訊。
	tail = strings.ReplaceAll(clean(tail), "\n", " ")
	remaining := m.width - ansi.StringWidth(mode) - 1
	choices := m.helpChoices()
	help := choices[len(choices)-1]
	reserved := min(ansi.StringWidth(tail)+1, max(0, remaining-ansi.StringWidth(help)))
	for _, candidate := range choices {
		if ansi.StringWidth(candidate) <= remaining-reserved {
			help = candidate
			break
		}
	}
	prefix := m.styles.mode.Render(mode) + " " + m.styles.muted.Render(help)
	available := m.width - ansi.StringWidth(prefix)
	if available > 1 {
		tail = ansi.Truncate(tail, available-1, "…")
		prefix += strings.Repeat(" ", available-ansi.StringWidth(tail)) + tailStyle.Render(tail)
	}
	return trustedCell(prefix, m.width)
}
