package queueui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
	"github.com/charmbracelet/x/ansi"
)

// 清除終端序列與控制碼，不修改儲存本文；保留完整可見文字與換行。
func clean(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f {
			return -1
		}
		return r
	}, s)
}
func cell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(strings.ReplaceAll(clean(s), "\n", " "), width, "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

var taipei = time.FixedZone("Asia/Taipei", 8*60*60)

func stamp(ms int64, full bool) string {
	if ms <= 0 {
		return "—"
	}
	layout := "01-02 15:04"
	if full {
		layout = "2006-01-02 15:04:05 -07:00"
	}
	return time.UnixMilli(ms).In(taipei).Format(layout)
}
func delivered(row sqlc.BrowseMessagesRow) int64 {
	if row.State == "submitted" && row.BatchState.Valid && row.BatchState.String == "submitted" && row.BatchUpdatedAt.Valid {
		return row.BatchUpdatedAt.Int64
	}
	return 0
}
func (m *Model) text(s string) string {
	if m.language == i18n.TraditionalChinese {
		if translated, ok := labels[s]; ok {
			return translated
		}
	}
	return s
}

var labels = map[string]string{
	"Sender": "發件者", "Stored": "收件／入庫", "State": "狀態", "Message": "訊息內容", "Recipient": "收件人", "Delivery time": "投遞時間",
	"Details": "訊息資料", "Message body": "訊息本文", "Sequence": "序號", "Idempotency key": "冪等鍵", "Batch updated": "批次更新", "line": "行", "No messages.": "沒有訊息。", "No matching messages.": "沒有符合篩選的訊息。", "Loading…": "載入中…", "Unable to load history.": "無法載入歷史資料。", "Manual": "手動", "Auto (1s)": "自動（每秒）", "DATA NOT UPDATED": "資料未更新", "Filters": "篩選", "All": "全部", "Enlarge terminal (minimum 74 × 16).": "請放大終端（至少 74 × 16）。",
	"Root": "實例路徑", "ID": "識別碼", "Batch": "批次", "Automatic submission record": "自動提交紀錄", "Manual confirmation record (not original delivery time)": "人工確認紀錄（非原始投遞時間）", "Batch evidence": "批次證據", "No reliable delivery time": "無可靠投遞時間", "Submission is not a read receipt.": "提交不代表已讀、接單或完成。", "Control sequences are hidden; stored body is unchanged.": "控制序列已隱藏；儲存本文未改寫。",
	"Selection no longer matches filters; moved to first row.":                                   "選取訊息已不符合篩選，移至第一列。",
	"↑/↓ field  ←/→ value  Enter apply  Esc cancel":                                              "↑/↓ 選項  ←/→ 條件  Enter 套用  Esc 取消",
	"j/k select  u/d list ½  U/D body  [/] data  r refresh  Ctrl+P filters  Ctrl+W auto  q quit": "j/k 選列  u/d 列表半頁  U/D 本文  [/] 資料  r 刷新  Ctrl+P 篩選  Ctrl+W 自動  q 離開",
	"j/k select u/d list U/D body [] data r ^P filters ^W auto q":                                "j/k選列 u/d半頁 U/D本文 []資料 r刷新 ^P篩選 ^W自動 q離開",
}

func (m *Model) contentHeight() int    { return max(1, m.height-4) }
func (m *Model) tableWidth() int       { return max(1, m.width-2) }
func (m *Model) tableHeight() int      { return max(1, (m.contentHeight()-1)/2) }
func (m *Model) metadataWidth() int    { return max(1, (m.tableWidth()-1)*4/10) }
func (m *Model) detailWidth() int      { return max(1, m.tableWidth()-m.metadataWidth()-1) }
func (m *Model) detailPageHeight() int { return max(1, m.contentHeight()-m.tableHeight()-1) }
func (m *Model) listHalfPage() int     { return max(1, (m.tableHeight()-1)/2) }

// 本文只有右下區使用，與左側中繼資料的內容及捲動位置完全分離。
func (m *Model) detailLines() []string {
	if len(m.rows) == 0 {
		return []string{m.text("No messages.")}
	}
	return strings.Split(ansi.Hardwrap(clean(m.rows[m.selected].Body), m.detailWidth(), true), "\n")
}
func (m *Model) metadataLines() []string {
	if len(m.rows) == 0 {
		return []string{m.text("No messages.")}
	}
	row := m.rows[m.selected]
	text := m.text("Sender") + ": " + row.Sender + " → " + m.text("Recipient") + ": " + row.Recipient + "\n" + m.text("State") + ": " + row.State + "\n" + m.text("Stored") + ": " + stamp(row.CreatedAt, true) + "\n" + m.text("Delivery time") + ": " + stamp(delivered(row), true)
	if delivered(row) > 0 {
		text += "\n" + m.text("Automatic submission record")
	}
	if row.BatchID.Valid {
		text += "\n" + m.text("Batch") + ": " + row.BatchID.String + " (" + row.BatchState.String + ")"
	}
	if row.BatchState.String == "resolved_submitted" && row.BatchUpdatedAt.Valid {
		text += "\n" + m.text("Manual confirmation record (not original delivery time)") + ": " + stamp(row.BatchUpdatedAt.Int64, true)
	} else if delivered(row) == 0 {
		text += "\n" + m.text("No reliable delivery time")
	}
	if row.BatchDetail.Valid && row.BatchDetail.String != "" {
		text += "\n" + m.text("Batch evidence") + ": " + row.BatchDetail.String
	}
	text += "\n" + m.text("ID") + ": " + row.ID + "\n" + m.text("Sequence") + ": " + fmt.Sprint(row.Seq) + "\n" + m.text("Idempotency key") + ": " + row.IdempotencyKey
	if row.BatchUpdatedAt.Valid {
		text += "\n" + m.text("Batch updated") + ": " + stamp(row.BatchUpdatedAt.Int64, true)
	}
	text += "\n" + m.text("Root") + ": " + m.root + "\n" + m.text("Submission is not a read receipt.") + "\n" + m.text("Control sequences are hidden; stored body is unchanged.")
	return strings.Split(ansi.Hardwrap(clean(text), m.metadataWidth(), true), "\n")
}
func (m *Model) clampLayout() {
	m.selected = max(0, min(m.selected, max(0, len(m.rows)-1)))
	visible := max(1, m.tableHeight()-1)
	if m.selected < m.top {
		m.top = m.selected
	}
	if m.selected >= m.top+visible {
		m.top = m.selected - visible + 1
	}
	m.top = max(0, min(m.top, max(0, len(m.rows)-visible)))
	m.detailOffset = max(0, min(m.detailOffset, max(0, len(m.detailLines())-m.detailPageHeight())))
	m.metadataOffset = max(0, min(m.metadataOffset, max(0, len(m.metadataLines())-m.detailPageHeight())))
}
func (m *Model) tableLines() []string {
	width, height := max(1, m.tableWidth()), m.tableHeight()
	widths := []int{10, 11, 11, max(1, width-62), 10, 13}
	line := func(fields []string) string {
		parts := make([]string, 6)
		for i := range parts {
			parts[i] = cell(fields[i], widths[i])
		}
		return strings.Join(parts, " ")
	}
	result := []string{cell("  "+line([]string{m.text("Sender"), m.text("Stored"), m.text("State"), m.text("Message"), m.text("Recipient"), m.text("Delivery time")}), width)}
	for i := m.top; i < len(m.rows) && len(result) < height; i++ {
		row := m.rows[i]
		prefix := "  "
		if i == m.selected {
			prefix = "> "
		}
		rendered := cell(prefix+line([]string{row.Sender, stamp(row.CreatedAt, false), row.State, row.Body, row.Recipient, stamp(delivered(row), false)}), width)
		if i == m.selected {
			rendered = "\x1b[7m" + rendered + "\x1b[0m"
		}
		result = append(result, rendered)
	}
	if len(m.rows) == 0 && height > 1 {
		text := m.text("No messages.")
		if !m.loaded {
			text = m.text("Loading…")
			if m.lastError != "" {
				text = m.text("Unable to load history.")
			}
		} else if m.filter != (Filter{}) {
			text = m.text("No matching messages.")
		}
		result = append(result, cell(text, width))
	}
	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	return result
}
func (m *Model) View() tea.View {
	width := max(1, m.width)
	if m.width < 74 || m.height < 16 {
		v := tea.NewView(cell(m.text("Enlarge terminal (minimum 74 × 16)."), width))
		v.AltScreen = true
		return v
	}
	content := m.tableLines()
	detail, metadata := m.detailLines(), m.metadataLines()
	dh, dw, mw := m.detailPageHeight(), m.detailWidth(), m.metadataWidth()
	bodyTitle := fmt.Sprintf(" %s · %s %d/%d U/D ", m.text("Message body"), m.text("line"), m.detailOffset+1, len(detail))
	metadataTitle := fmt.Sprintf(" %s · %s %d/%d [] ", m.text("Details"), m.text("line"), m.metadataOffset+1, len(metadata))
	content = append(content, cell(metadataTitle, mw)+"┬"+cell(bodyTitle, dw))
	for i := 0; i < dh; i++ {
		left, right := "", ""
		if idx := i + m.metadataOffset; idx < len(metadata) {
			left = metadata[idx]
		}
		if idx := i + m.detailOffset; idx < len(detail) {
			right = detail[idx]
		}
		content = append(content, cell(left, mw)+"│"+cell(right, dw))
	}
	if m.popup {
		popup := []string{m.text("Filters"), "", m.text("Sender") + ": " + m.choice(m.draft.Sender), m.text("Recipient") + ": " + m.choice(m.draft.Recipient), m.text("State") + ": " + m.choice(m.draft.State), "", m.text("↑/↓ field  ←/→ value  Enter apply  Esc cancel")}
		popup[m.field+2] = "> " + popup[m.field+2]
		// 彈窗覆蓋內容中央，不讓終端序列或底層畫面穿透。
		pw := min(width-6, 72)
		start := (len(content) - len(popup) - 2) / 2
		panel := []string{"┌" + strings.Repeat("─", pw-2) + "┐"}
		for _, line := range popup {
			panel = append(panel, "│"+cell(line, pw-2)+"│")
		}
		panel = append(panel, "└"+strings.Repeat("─", pw-2)+"┘")
		for i, line := range panel {
			if start+i >= 0 && start+i < len(content) {
				pad := (width - 2 - pw) / 2
				content[start+i] = strings.Repeat(" ", pad) + line + strings.Repeat(" ", width-2-pad-pw)
			}
		}
	}
	title := " " + clean(filepath.Base(m.root)) + " · hrelay ui "
	output := "┌" + cell(title, width-2) + "┐\n"
	for _, line := range content {
		output += "│" + line + "│\n"
	}
	output += "└" + strings.Repeat("─", width-2) + "┘\n"
	mode := m.text("Manual")
	if m.auto {
		mode = m.text("Auto (1s)")
	}
	status := fmt.Sprintf("%s | %d/%d", mode, min(m.selected+1, len(m.rows)), len(m.rows))
	if m.busy {
		status += " | " + m.text("Loading…")
	}
	if m.lastError != "" {
		status += " | " + m.text("DATA NOT UPDATED") + ": " + m.lastError
	} else if m.notice != "" {
		status += " | " + m.notice
	}
	help := m.text("j/k select  u/d list ½  U/D body  [/] data  r refresh  Ctrl+P filters  Ctrl+W auto  q quit")
	if ansi.StringWidth(help) > width {
		help = m.text("j/k select u/d list U/D body [] data r ^P filters ^W auto q")
	}
	output += cell(status, width) + "\n" + cell(help, width)
	v := tea.NewView(output)
	v.AltScreen = true
	return v
}
func (m *Model) choice(value string) string {
	if value == "" {
		return m.text("All")
	}
	return clean(value)
}
