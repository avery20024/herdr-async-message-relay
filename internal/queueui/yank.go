package queueui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

type copiedMsg struct {
	osc52 bool
	err   error
}

var yankChoices = [][2]string{
	{"y", "Message body"},
	{"i", "Message ID"},
	{"b", "Batch ID"},
	{"s", "Sender"},
	{"r", "Recipient"},
}

func (m *Model) openYank() {
	if m.copyBusy {
		return
	}
	if len(m.rows) == 0 {
		m.notice = m.text("Select a message to copy.")
		return
	}
	// 第一鍵即固定完整資料，刷新不會把第二鍵導向另一則訊息。
	m.yankTarget = m.rows[m.selected]
	m.yankOpen = true
}

func yankValue(row sqlc.BrowseMessagesRow, key string) (string, bool) {
	switch key {
	case "y":
		return row.Body, true
	case "i":
		return row.ID, true
	case "b":
		return row.BatchID.String, row.BatchID.Valid
	case "s":
		return row.Sender, true
	case "r":
		return row.Recipient, true
	}
	return "", false
}

func (m *Model) yankKey(key string) tea.Cmd {
	m.yankOpen = false
	value, ok := yankValue(m.yankTarget, key)
	if key == "esc" {
		return nil
	}
	if !ok || value == "" {
		m.notice = m.text("Nothing copied; key or field unavailable.")
		return nil
	}
	m.copyBusy = true
	if m.clipboardOSC52 {
		return tea.Sequence(tea.SetClipboard(value), func() tea.Msg { return copiedMsg{osc52: true} })
	}
	writer := m.clipboardWrite
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 2*time.Second)
		defer cancel()
		return copiedMsg{err: writer(ctx, value)}
	}
}

func (m *Model) yankPanel() string {
	width := max(1, min(42, m.tableWidth())-2)
	lines := []string{}
	// 最小20×10畫面仍同時列出五個鍵與用途；僅省略標題與底部提示。
	if m.contentHeight() >= 9 {
		lines = append(lines, m.styles.title.Render(cell(m.text("y: copy selected message"), width)))
	}
	for _, choice := range yankChoices {
		lines = append(lines, m.styles.accent.Render(choice[0])+"  "+cell(m.text(choice[1]), max(1, width-3)))
	}
	if m.contentHeight() >= 9 {
		lines = append(lines, m.styles.muted.Render(cell(m.text("ESC cancel"), width)))
	}
	return m.styles.dialog.Border(lipgloss.RoundedBorder()).BorderForeground(m.styles.border.GetForeground()).Render(strings.Join(lines, "\n"))
}

func (m *Model) yankOverlay(content []string) []string {
	panel := m.yankPanel()
	overlay := lipgloss.NewLayer(panel).X(max(0, (m.tableWidth()-lipgloss.Width(panel))/2)).Y(max(0, (len(content)-lipgloss.Height(panel))/2)).Z(2)
	result := strings.Split(lipgloss.NewCompositor(lipgloss.NewLayer(strings.Join(content, "\n")), overlay).Render(), "\n")
	for i := range result {
		result[i] = trustedCell(result[i], m.tableWidth())
	}
	return result
}
