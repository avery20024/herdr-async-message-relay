package queueui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	mobileBreakpoint = 100
	minimumWidth     = 20
	minimumHeight    = 10
)

func (m *Model) mobile() bool { return m.width < mobileBreakpoint }

// top／selected始終以訊息索引計算；窄版每筆兩列，不讓奇數剩餘列顯示半筆。
func (m *Model) visibleMessages() int {
	if m.mobile() {
		return max(1, m.tableHeight()/2)
	}
	return max(1, m.tableHeight()-1)
}

func summaryText(body string) string { return strings.Join(strings.Fields(clean(body)), " ") }

func (m *Model) metadataLabel(label string) string {
	if m.mobile() {
		if label == "ID" {
			return "ID"
		}
		if short, ok := map[string]string{"Sender": "From", "Recipient": "To", "Stored": "Time", "Delivery time": "Delivery", "Sequence": "Seq", "Idempotency key": "Key", "Batch updated": "Updated", "Root": "Path", "ID": "ID", "Batch evidence": "Evidence"}[label]; ok {
			return m.text(short)
		}
	}
	return m.text(label)
}

func mobileState(state string, width int) string {
	if width >= 32 {
		return stateLabel(state)
	}
	if code, ok := map[string]string{"queued": "○Q", "dispatching": "◐D", "submitted": "✓S", "uncertain": "▲?"}[state]; ok {
		return code
	}
	return "?"
}

func (m *Model) cardLines() []string {
	width, height := m.tableWidth(), m.tableHeight()
	result := make([]string, 0, height)
	for i := m.top; i < len(m.rows) && i < m.top+m.visibleMessages(); i++ {
		row := m.rows[i]
		state := cell(mobileState(row.State, width), min(13, ansi.StringWidth(mobileState(row.State, width))))
		routeWidth := max(5, width-3-ansi.StringWidth(state))
		names := routeWidth - 3
		route := cell(row.Sender, (names+1)/2) + " → " + cell(row.Recipient, names/2)
		prefix := "  "
		if i == m.selected {
			prefix = "▌ "
		}
		first := prefix + route + " "
		if i == m.selected {
			first += state
		} else {
			first += m.styles.states[row.State].Render(state)
		}
		stored := "—"
		if row.CreatedAt > 0 {
			stored = time.UnixMilli(row.CreatedAt).In(taipei).Format("15:04")
		}
		second := "  " + cell(stored, 5) + " " + cell(summaryText(row.Body), max(1, width-8))
		if i == m.selected {
			first = m.styles.selected.Render(trustedCell(first, width))
			second = m.styles.selected.Render(cell(second, width))
		} else {
			first = trustedCell(first, width)
			second = "  " + m.styles.muted.Render(cell(stored, 5)) + " " + cell(summaryText(row.Body), max(1, width-8))
		}
		result = append(result, first, second)
	}
	if len(m.rows) == 0 {
		text := m.text("No messages.")
		if !m.loaded {
			text = m.text("Loading…")
			if m.lastError != "" {
				text = m.text("Unable to load history.")
			}
		} else if m.filter != (Filter{}) {
			text = m.text("No matching messages.")
		}
		style := m.styles.muted
		if m.lastError != "" {
			style = m.styles.failure
		}
		result = append(result, style.Render(cell(text, width)))
	}
	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	return result
}

func (m *Model) lowerTitles(metadataLines, bodyLines int) (string, string) {
	meta := fmt.Sprintf(" %s · %s %d/%d [] ", m.text("Details"), m.text("line"), m.metadataOffset+1, metadataLines)
	body := fmt.Sprintf(" %s · %s %d/%d U/D ", m.text("Message body"), m.text("line"), m.detailOffset+1, bodyLines)
	if m.mobile() {
		meta = fmt.Sprintf("%d/%d []", m.metadataOffset+1, metadataLines)
		body = fmt.Sprintf("%d/%d U/D", m.detailOffset+1, bodyLines)
		if m.metadataWidth() >= 16 {
			meta = m.text("Data") + " " + meta
		}
		if m.detailWidth() >= 22 {
			body = m.text("Body") + " " + body
		}
	}
	return meta, body
}
