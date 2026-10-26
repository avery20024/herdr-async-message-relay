package queueui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	"Preferences": "偏好設定", "Language": "語系", "Theme": "主題", "Auto": "自動", "Terminal (default)": "終端預設",
	"Team guard unavailable.":                                   "團隊攔截設定無法使用。",
	"Team save unconfirmed; reload before retrying.":            "團隊設定儲存未確認，請先重載再重試。",
	"Claude interception (team)":                                "Claude 問答攔截（全團隊）",
	"Recording continues when off.":                             "關閉攔截仍持續記錄問答時間。",
	"Observed %d · pending %d · uninitialized %d · abnormal %d": "已觀測 %d · 問答中 %d · 未初始化 %d · 異常 %d",
	"Observed events are not a Hooks health guarantee.":         "已觀測事件不代表 Hooks 持續健康；未初始化須接線／新會話。",
	"Local preferences saved; team save pending.":               "本機偏好已儲存；團隊設定儲存中。",
	"Loading": "載入中", "Unavailable": "無法使用", "On": "開", "Off": "關", "Reload required": "須重載",
	"Preferences storage unavailable.": "偏好設定儲存位置無法使用。", "Unable to load preferences.": "無法載入偏好設定。", "Unable to save preferences.": "無法儲存偏好設定。", "Preferences reloaded.": "已重載偏好設定。", "Preferences saved.": "已儲存偏好設定。",
	"↑↓ field ←→ value Enter save Esc cancel ^R reload":                                                             "↑↓選項 ←→設定 Enter儲存 Esc取消 ^R重載",
	"j/k select u/d list U/D body [] data r refresh P clear filters ^F filters ^P prefs ^R config ^W auto q ? help": "j/k選列 u/d半頁 U/D本文 []資料 r刷新 P 清除篩選 ^F篩選 ^P設定 ^R重載 ^W自動 q離開 ?說明",
	"j/k u/d U/D [] r P clear filters ^F filter ^P prefs ^R reload ^W auto q ? help":                                "j/k u/d U/D [] r P 清除篩選 ^F篩選 ^P設定 ^R重載 ^W自動 q ?說明",
	"Keyboard shortcuts": "快捷鍵說明", "Quit UI": "離開UI", "ESC close": "ESC 退出", "Navigation": "訊息導覽", "Main screen": "主畫面", "Filter dialog": "篩選視窗", "Preferences dialog": "偏好設定視窗", "Global": "全域", "Help screen": "說明畫面",
	"Select next/previous message": "選取下一／上一則訊息", "List half-page up/down": "訊息列表上／下半頁", "Body page up/down": "訊息本文上／下一頁", "Metadata page up/down": "訊息資料上／下一頁",
	"Open filters": "開啟篩選", "Clear applied filters": "清除已套用篩選", "Open Preferences": "開啟偏好設定", "Refresh messages only": "只刷新訊息", "Toggle one-second auto refresh": "開關每秒自動刷新", "Quit from main screen only": "僅在主畫面離開UI",
	"Choose field": "選擇欄位", "Cycle value": "切換條件／選項", "Clear selected draft field": "清除此欄草稿", "Clear all draft fields": "清除全部草稿", "Apply draft filters": "套用篩選草稿", "Cancel filter draft": "取消篩選草稿",
	"Choose language or theme": "選擇語系或主題", "Save and apply preferences": "儲存並套用偏好設定", "Cancel preferences draft": "取消偏好設定草稿", "Open/close this help": "開啟／關閉本說明", "Reload settings, not messages": "重載設定、不刷新訊息", "Quit UI from any screen": "任何畫面均可離開UI",
	"Scroll help down/up": "說明向下／上捲動", "Help page up/down": "說明上／下一頁", "Help top/bottom": "說明頂端／底端", "Return to previous screen": "回到原畫面",
	"j/k scroll u/d page g/G ends Esc/? close ^C quit": "j/k捲動 u/d翻頁 g/G頂底 Esc/?關閉 ^C離開",

	"STALE": "未更新",
	"↑↓ field ←→ value d clear P clear-all Enter apply Esc cancel": "↑↓選項 ←→條件 d清此項 P全清 Enter套用 Esc取消",
	"From": "寄", "To": "收", "Time": "入庫", "Delivery": "投遞", "Seq": "序", "Key": "鍵", "Updated": "更新", "Evidence": "證據", "Data": "資料", "Body": "本文", "Path": "路徑",
	"Sender": "發件者", "Stored": "收件／入庫", "State": "狀態", "Message": "訊息內容", "Recipient": "收件人", "Delivery time": "投遞時間",
	"Details": "訊息資料", "Message body": "訊息本文", "Sequence": "序號", "Idempotency key": "冪等鍵", "Batch updated": "批次更新", "line": "行", "No messages.": "沒有訊息。", "No matching messages.": "沒有符合篩選的訊息。", "Loading…": "載入中…", "Unable to load history.": "無法載入歷史資料。", "Manual": "手動", "Auto (1s)": "自動（每秒）", "DATA NOT UPDATED": "資料未更新", "Filters": "篩選", "All": "全部", "Minimum 20 × 10 cells.": "至少需 20 × 10 格。",
	"Root": "實例路徑", "ID": "識別碼", "Batch": "批次", "Automatic submission record": "自動提交紀錄", "Manual confirmation record (not original delivery time)": "人工確認紀錄（非原始投遞時間）", "Batch evidence": "批次證據", "No reliable delivery time": "無可靠投遞時間", "Submission is not a read receipt.": "提交不代表已讀、接單或完成。", "Control sequences are hidden; stored body is unchanged.": "控制序列已隱藏；儲存本文未改寫。",
	"Selection no longer matches filters; moved to first row.": "選取訊息已不符合篩選，移至第一列。",
	"↑/↓ field  ←/→ value  Enter apply  Esc cancel":            "↑/↓ 選項  ←/→ 條件  Enter 套用  Esc 取消",
}

func (m *Model) contentHeight() int    { return max(1, m.height-3) }
func (m *Model) tableWidth() int       { return max(1, m.width-2) }
func (m *Model) tableHeight() int      { return max(1, (m.contentHeight()-1)/2) }
func (m *Model) metadataWidth() int    { return max(1, (m.tableWidth()-1)*4/10) }
func (m *Model) detailWidth() int      { return max(1, m.tableWidth()-m.metadataWidth()-1) }
func (m *Model) detailPageHeight() int { return max(1, m.contentHeight()-m.tableHeight()-1) }
func (m *Model) listHalfPage() int     { return max(1, m.visibleMessages()/2) }

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
	label := m.metadataLabel
	text := label("Sender") + ": " + row.Sender + " → " + label("Recipient") + ": " + row.Recipient + "\n" + label("State") + ": " + row.State + "\n" + label("Stored") + ": " + stamp(row.CreatedAt, true) + "\n" + label("Delivery time") + ": " + stamp(delivered(row), true)
	if delivered(row) > 0 {
		text += "\n" + m.text("Automatic submission record")
	}
	if row.BatchID.Valid {
		text += "\n" + label("Batch") + ": " + row.BatchID.String + " (" + row.BatchState.String + ")"
	}
	if row.BatchState.String == "resolved_submitted" && row.BatchUpdatedAt.Valid {
		text += "\n" + m.text("Manual confirmation record (not original delivery time)") + ": " + stamp(row.BatchUpdatedAt.Int64, true)
	} else if delivered(row) == 0 {
		text += "\n" + m.text("No reliable delivery time")
	}
	if row.BatchDetail.Valid && row.BatchDetail.String != "" {
		text += "\n" + label("Batch evidence") + ": " + row.BatchDetail.String
	}
	text += "\n" + label("ID") + ": " + row.ID + "\n" + label("Sequence") + ": " + fmt.Sprint(row.Seq) + "\n" + label("Idempotency key") + ": " + row.IdempotencyKey
	if row.BatchUpdatedAt.Valid {
		text += "\n" + label("Batch updated") + ": " + stamp(row.BatchUpdatedAt.Int64, true)
	}
	text += "\n" + label("Root") + ": " + m.root + "\n" + m.text("Submission is not a read receipt.") + "\n" + m.text("Control sequences are hidden; stored body is unchanged.")
	return strings.Split(ansi.Wrap(clean(text), m.metadataWidth(), " /"), "\n")
}
func (m *Model) clampLayout() {
	m.clampHelp()
	m.selected = max(0, min(m.selected, max(0, len(m.rows)-1)))
	visible := m.visibleMessages()
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
	if m.mobile() {
		return m.cardLines()
	}
	width, height := max(1, m.tableWidth()), m.tableHeight()
	widths := []int{10, 11, 13, max(1, width-64), 10, 13}
	line := func(fields []string) string {
		parts := make([]string, 6)
		for i := range parts {
			parts[i] = cell(fields[i], widths[i])
		}
		return strings.Join(parts, " ")
	}
	result := []string{m.styles.heading.Render(cell("  "+line([]string{m.text("Sender"), m.text("Stored"), m.text("State"), m.text("Message"), m.text("Recipient"), m.text("Delivery time")}), width))}
	for i := m.top; i < len(m.rows) && len(result) < height; i++ {
		row := m.rows[i]
		fields := []string{row.Sender, stamp(row.CreatedAt, false), stateLabel(row.State), summaryText(row.Body), row.Recipient, stamp(delivered(row), false)}
		parts := make([]string, len(fields))
		for col, value := range fields {
			parts[col] = cell(value, widths[col])
			if i != m.selected {
				switch col {
				case 1, 5:
					parts[col] = m.styles.muted.Render(parts[col])
				case 2:
					parts[col] = m.styles.states[row.State].Render(parts[col])
				}
			}
		}
		rendered := trustedCell("  "+strings.Join(parts, " "), width)
		if i == m.selected {
			rendered = m.styles.selected.Render(cell("▌ "+line(fields), width))
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
		result = append(result, m.styles.muted.Render(lipgloss.PlaceHorizontal(width, lipgloss.Center, clean(text))))
	}
	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	return result
}
func (m *Model) View() tea.View {
	width := max(1, m.width)
	if m.width < minimumWidth || m.height < minimumHeight {
		output := cell(m.text("Minimum 20 × 10 cells."), width)
		if p, ok := palettes[m.preferences.Theme]; ok && p.light && !m.noColor {
			output += strings.Repeat("\n"+strings.Repeat(" ", width), max(0, m.height-1))
		}
		v := tea.NewView(m.paintCanvas(output))
		v.AltScreen = true
		return v
	}
	content := m.tableLines()
	detail, metadata := m.detailLines(), m.metadataLines()
	dh, dw, mw := m.detailPageHeight(), m.detailWidth(), m.metadataWidth()
	metadataTitle, bodyTitle := m.lowerTitles(len(metadata), len(detail))
	content = append(content, m.styles.heading.Render(cell(metadataTitle, mw))+m.styles.border.Render("┬")+m.styles.heading.Render(cell(bodyTitle, dw)))
	leftViewport := textViewport(metadata, mw, dh, m.metadataOffset)
	rightViewport := textViewport(detail, dw, dh, m.detailOffset)
	leftLines := strings.Split(leftViewport.View(), "\n")
	rightLines := strings.Split(rightViewport.View(), "\n")
	for i := 0; i < dh; i++ {
		left, right := "", ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		if i < len(rightLines) {
			right = rightLines[i]
		}
		content = append(content, trustedCell(left, mw)+m.styles.border.Render("│")+trustedCell(right, dw))
	}
	if m.popup || m.preferencesOpen {
		popup := []string{m.text("Filters"), "", m.text("Sender") + ": " + m.choice(m.draft.Sender), m.text("Recipient") + ": " + m.choice(m.draft.Recipient), m.text("State") + ": " + m.choice(m.draft.State), "", m.text("↑↓ field ←→ value d clear P clear-all Enter apply Esc cancel")}
		selectedField := m.field + 2
		pw := min(width-6, 72)
		if !m.preferencesOpen && (m.mobile() || len(content) < len(popup)+2) {
			popup = []string{m.text("Filters"), m.metadataLabel("Sender") + ": " + m.choice(m.draft.Sender), m.metadataLabel("Recipient") + ": " + m.choice(m.draft.Recipient), m.metadataLabel("State") + ": " + m.choice(m.draft.State)}
			selectedField = m.field + 1
			pw = min(width-4, 72)
		}
		if m.preferencesOpen {
			popup = m.preferencesLines()
			selectedField = m.preferenceField + 1
			pw = min(width-4, 72)
		}
		for i, line := range popup {
			prefix := "  "
			if i == selectedField {
				prefix = "▌ "
			}
			popup[i] = cell(prefix+line, pw-2)
			if i == selectedField {
				popup[i] = m.styles.selected.Render(popup[i])
			} else if i == 0 {
				popup[i] = m.styles.accent.Render(popup[i])
			}
		}
		panel := m.styles.dialog.Border(lipgloss.RoundedBorder()).BorderForeground(m.styles.border.GetForeground()).Render(strings.Join(popup, "\n"))
		// 用cell圖層處理寬字與樣式，不清空彈窗兩側原本的資料。
		base := lipgloss.NewLayer(strings.Join(content, "\n"))
		overlay := lipgloss.NewLayer(panel).X((width - 2 - pw) / 2).Y((len(content) - len(popup) - 2) / 2).Z(1)
		content = strings.Split(lipgloss.NewCompositor(base, overlay).Render(), "\n")
		// compositor會省略尾端空白，補回固定列寬以清除舊畫面且維持外框位置。
		for i := range content {
			content[i] = trustedCell(content[i], width-2)
		}
	}
	if m.helpOpen {
		content = m.helpOverlay(content)
	}
	title := " " + clean(filepath.Base(m.root)) + " · hrelay ui "
	summary := fmt.Sprintf("%d/%d", min(m.selected+1, len(m.rows)), len(m.rows))
	if m.next > 0 {
		summary += "+"
	}
	if m.configError != "" {
		summary = "! CFG"
	}
	if m.lastError != "" {
		summary = "! " + m.staleText()
		if m.configError != "" {
			summary = "!STALE/CFG"
		}
	}
	summary = ansi.Truncate(summary, max(1, width-10), "…")
	header := m.styles.title.Render(cell(title, width-4-ansi.StringWidth(summary))) + m.styles.title.Render(" "+summary+" ")
	if m.lastError != "" {
		header = m.styles.title.Render(cell(title, width-4-ansi.StringWidth(summary))) + m.styles.failure.Render(" "+summary+" ")
	}
	output := m.styles.border.Render("┌") + header + m.styles.border.Render("┐") + "\n"
	for _, line := range content {
		output += m.styles.border.Render("│") + line + m.styles.border.Render("│") + "\n"
	}
	output += m.styles.border.Render("└"+strings.Repeat("─", width-2)+"┘") + "\n"
	output += m.footerLine()
	v := tea.NewView(m.paintCanvas(output))
	v.AltScreen = true
	return v
}
func (m *Model) choice(value string) string {
	if value == "" {
		return m.text("All")
	}
	return clean(value)
}
