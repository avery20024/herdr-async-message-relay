package queueui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func wheel(x, y int, down bool) tea.MouseWheelMsg {
	button := tea.MouseWheelUp
	if down {
		button = tea.MouseWheelDown
	}
	return tea.MouseWheelMsg{X: x, Y: y, Button: button}
}

func TestMouseWheelRoutesByPane(t *testing.T) {
	for _, size := range [][2]int{{180, 30}, {74, 16}, {20, 10}} {
		m, f := ready(t)
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.View().MouseMode != tea.MouseModeCellMotion {
			t.Fatal("mouse reporting is not enabled")
		}
		m.rows[0].IdempotencyKey = strings.Repeat("long-key", 100)
		lower := m.tableHeight() + 2
		calls := len(f.calls)
		m.Update(wheel(1, lower, true))
		if m.metadataOffset != 1 || m.detailOffset != 0 || m.selected != 0 {
			t.Fatalf("left pane leaked at %v: metadata=%d body=%d selected=%d", size, m.metadataOffset, m.detailOffset, m.selected)
		}
		m.Update(wheel(m.width-2, lower, true))
		if m.metadataOffset != 1 || m.detailOffset != 1 || m.selected != 0 {
			t.Fatal("right pane leaked")
		}
		m.Update(wheel(1, lower, false))
		m.Update(wheel(m.width-2, lower, false))
		if m.metadataOffset != 0 || m.detailOffset != 0 || len(f.calls) != calls {
			t.Fatal("lower panes fetched or failed to reverse")
		}
		m.Update(wheel(1, 2, true))
		if m.selected != 1 || m.detailOffset != 0 || m.metadataOffset != 0 {
			t.Fatal("list did not select exactly one message")
		}
		m.Update(wheel(1, 2, false))
		if m.selected != 0 {
			t.Fatal("list did not reverse")
		}
	}
}

func TestMouseWheelIgnoresChromeAndDialogs(t *testing.T) {
	m, f := ready(t)
	lower, divider := m.tableHeight()+2, m.metadataWidth()+1
	for _, point := range [][2]int{{0, lower}, {m.width - 1, lower}, {1, 0}, {1, 1}, {1, m.tableHeight() + 1}, {divider, lower}, {1, m.height - 2}, {1, m.height - 1}, {-1, lower}, {m.width, lower}, {1, m.height}} {
		m.Update(wheel(point[0], point[1], true))
	}
	m.Update(tea.MouseWheelMsg{X: 1, Y: lower, Button: tea.MouseWheelLeft})
	m.Update(tea.MouseWheelMsg{X: 1, Y: lower, Button: tea.MouseWheelRight})
	calls := len(f.calls)
	for _, dialog := range []string{"filter", "preferences", "help"} {
		m.popup, m.preferencesOpen, m.helpOpen = dialog == "filter", dialog == "preferences", dialog == "help"
		for _, point := range [][2]int{{1, 2}, {1, lower}, {m.width - 2, lower}} {
			m.Update(wheel(point[0], point[1], true))
		}
	}
	if m.selected != 0 || m.metadataOffset != 0 || m.detailOffset != 0 || len(f.calls) != calls {
		t.Fatal("chrome or dialog leaked navigation")
	}
	m.popup, m.preferencesOpen, m.helpOpen = false, false, false
	m.Update(tea.WindowSizeMsg{Width: 19, Height: 9})
	m.Update(wheel(1, 2, true))
	if m.selected != 0 || m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("undersized view accepted mouse navigation")
	}
}

func TestMouseWheelLazyPageAndEndpoints(t *testing.T) {
	m, _ := ready(t)
	m.selected = 49
	m.clampLayout()
	_, cmd := m.Update(wheel(1, 2, true))
	if cmd == nil {
		t.Fatal("wheel did not load next history page")
	}
	apply(m, cmd)
	if m.selected != 50 || len(m.rows) != 100 {
		t.Fatal("wheel lost pagination selection")
	}
	lower := m.tableHeight() + 2
	m.metadataOffset, m.detailOffset = 100000, 100000
	m.clampLayout()
	meta, body := m.metadataOffset, m.detailOffset
	m.Update(wheel(1, lower, true))
	m.Update(wheel(m.width-2, lower, true))
	if m.metadataOffset != meta || m.detailOffset != body {
		t.Fatal("wheel exceeded lower endpoints")
	}
	m.rows, m.next = nil, 0
	m.Update(wheel(1, lower, true))
	m.Update(wheel(m.width-2, lower, false))
	m.Update(wheel(1, 2, true))
	if m.selected != 0 || m.metadataOffset != 0 || m.detailOffset != 0 {
		t.Fatal("empty queue moved")
	}
}
