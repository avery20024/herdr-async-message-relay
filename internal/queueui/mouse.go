package queueui

import tea "charm.land/bubbletea/v2"

// 使用與 View 相同的零起算座標；不把外框、標題或彈窗事件轉成背景導覽。
func (m *Model) mouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.popup || m.preferencesOpen || m.helpOpen || m.width < minimumWidth || m.height < minimumHeight {
		return nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	if msg.X < 1 || msg.X >= m.width-1 || msg.Y < 1 || msg.Y >= m.height-2 {
		return nil
	}
	if msg.Y <= m.tableHeight() {
		// 桌面欄名不捲動；窄版沒有欄名列，每兩行是一筆訊息。
		if !m.mobile() && msg.Y == 1 {
			return nil
		}
		return m.moveSelection(delta, false)
	}
	if msg.Y == m.tableHeight()+1 {
		return nil
	}
	divider := 1 + m.metadataWidth()
	switch {
	case msg.X < divider:
		m.metadataOffset += delta
	case msg.X > divider:
		m.detailOffset += delta
	}
	return nil
}
