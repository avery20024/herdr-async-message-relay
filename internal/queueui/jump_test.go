package queueui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLoadedEndpointShortcuts(t *testing.T) {
	for _, size := range [][2]int{{180, 30}, {74, 16}, {20, 10}} {
		for _, last := range []tea.KeyPressMsg{key("G"), {Code: 'g', Mod: tea.ModShift}, {Code: 'g', Text: "G", Mod: tea.ModShift, ShiftedCode: 'G'}} {
			m, f := ready(t)
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.rows[0].IdempotencyKey = strings.Repeat("long-key", 100)
			m.Update(key("D"))
			m.Update(key("]"))
			calls, next := len(f.calls), m.next
			_, cmd := m.Update(last)
			if cmd != nil || m.selected != 49 || m.top != 50-m.visibleMessages() || m.detailOffset != 0 || m.metadataOffset != 0 || len(f.calls) != calls || m.next != next {
				t.Fatalf("last jump fetched or lost layout at %v: selected=%d top=%d", size, m.selected, m.top)
			}
			_, cmd = m.Update(key("g"))
			if cmd != nil || m.selected != 49 || !m.goTopPending {
				t.Fatal("single g jumped or failed to arm")
			}
			_, cmd = m.Update(key("g"))
			if cmd != nil || m.selected != 0 || m.top != 0 || m.goTopPending || len(f.calls) != calls {
				t.Fatal("gg did not jump to first loaded row without fetching")
			}
		}
	}
}

func TestJumpTargetsCurrentLoadedTail(t *testing.T) {
	m, f := ready(t)
	m.selected = 49
	_, cmd := m.Update(key("j"))
	apply(m, cmd)
	calls := len(f.calls)
	_, cmd = m.Update(key("G"))
	if cmd != nil || m.selected != 99 || len(m.rows) != 100 || len(f.calls) != calls || m.next == 0 {
		t.Fatal("G did not stop at loaded tail")
	}
	// 已載入末筆仍不是完整歷史末筆，不可因重按 G 觸發按需分頁。
	_, cmd = m.Update(key("G"))
	if cmd != nil || len(f.calls) != calls || m.selected != 99 {
		t.Fatal("repeated G fetched history")
	}
}

func TestJumpCancelsPendingPaginationSelection(t *testing.T) {
	for _, sequence := range [][]string{{"G"}, {"g", "g"}} {
		m, _ := ready(t)
		m.selected = 49
		m.clampLayout()
		_, cmd := m.Update(key("j"))
		if cmd == nil || m.pendingFrom == "" {
			t.Fatal("missing pending pagination")
		}
		for _, press := range sequence {
			m.Update(key(press))
		}
		id := m.selectedID()
		apply(m, cmd)
		if m.selectedID() != id || len(m.rows) != 100 {
			t.Fatal("late page response overrode endpoint jump")
		}
	}
}

func TestGoTopSequenceInterrupted(t *testing.T) {
	for _, interrupt := range []tea.Msg{key("k"), key("r"), key("?"), key("ctrl+f"), key("ctrl+p"), key("ctrl+r"), wheel(1, 2, true), tea.MouseClickMsg{X: 1, Y: 2}, tea.MouseReleaseMsg{X: 1, Y: 2}, tea.WindowSizeMsg{Width: 180, Height: 30}} {
		m, _ := ready(t)
		m.preferencesStore = testStore(t)
		m.selected = 20
		m.clampLayout()
		m.Update(key("g"))
		m.Update(interrupt)
		if m.goTopPending {
			t.Fatalf("sequence survived interrupt %T %v", interrupt, interrupt)
		}
		m.popup, m.preferencesOpen, m.helpOpen = false, false, false
		before := m.selected
		m.Update(key("g"))
		if m.selected != before {
			t.Fatal("isolated g jumped after interruption")
		}
		m.cancelFetch()
	}
}

func TestEndpointKeysDoNotLeakThroughDialogs(t *testing.T) {
	for _, dialog := range []string{"filters", "preferences", "help"} {
		m, _ := ready(t)
		m.selected = 20
		m.clampLayout()
		m.Update(tea.WindowSizeMsg{Width: 74, Height: 16})
		m.popup, m.preferencesOpen, m.helpOpen = dialog == "filters", dialog == "preferences", dialog == "help"
		for _, press := range []string{"g", "g", "G"} {
			m.Update(key(press))
		}
		if m.selected != 20 || m.goTopPending {
			t.Fatal("dialog changed underlying list or armed gg")
		}
		if dialog == "help" {
			if m.helpOffset == 0 {
				t.Fatal("existing help G no longer reaches end")
			}
			m.Update(key("g"))
			if m.helpOffset != 0 {
				t.Fatal("existing help g no longer reaches top")
			}
		}
	}
}

func TestEndpointKeysOnEmptyAndSingleRow(t *testing.T) {
	for _, count := range []int{0, 1} {
		m, f := ready(t)
		m.rows, m.next = m.rows[:count], 0
		calls := len(f.calls)
		for _, press := range []string{"G", "g", "g", "G"} {
			_, cmd := m.Update(key(press))
			if cmd != nil || m.selected != 0 || m.top != 0 || len(f.calls) != calls {
				t.Fatal("empty/single row jump fetched or went out of bounds")
			}
		}
	}
}
