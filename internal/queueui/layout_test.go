package queueui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFixedSplitAndSeparatedContent(t *testing.T) {
	m, _ := ready(t)
	m.rows[0].Body = "BODY-ONLY\n" + strings.Repeat("line\n", 60)
	m.rows[0].IdempotencyKey = "KEY-ONLY"
	for _, size := range [][2]int{{170, 44}, {136, 44}, {74, 16}, {220, 60}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.tableWidth() != size[0]-2 || m.tableHeight()+1+m.detailPageHeight() != size[1]-4 {
			t.Fatalf("not vertical halves: %v", size)
		}
		if delta := m.detailPageHeight() - m.tableHeight(); delta < 0 || delta > 1 {
			t.Fatalf("unequal halves: %v", size)
		}
		available := size[0] - 3 // 扣掉外框與中縫，再分配40%／60%。
		if m.metadataWidth() != available*4/10 || m.detailWidth() != available-available*4/10 {
			t.Fatalf("bottom columns are not 40/60: %v left=%d right=%d", size, m.metadataWidth(), m.detailWidth())
		}
		meta, body := strings.Join(m.metadataLines(), "\n"), strings.Join(m.detailLines(), "\n")
		if strings.Contains(meta, "BODY-ONLY") || !strings.Contains(meta, "KEY-ONLY") || strings.Contains(body, "KEY-ONLY") || !strings.Contains(body, "BODY-ONLY") {
			t.Fatal("body mixed with metadata")
		}
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		if len(lines) != size[1] {
			t.Fatalf("height=%d want=%d", len(lines), size[1])
		}
		for _, line := range lines {
			if ansi.StringWidth(line) != size[0] {
				t.Fatalf("row width mismatch %v: %q", size, line)
			}
		}
		divider := m.tableHeight() + 1
		if !strings.Contains(lines[divider], "Message body") || strings.Contains(strings.Join(lines[1:divider], "\n"), "KEY-ONLY") {
			t.Fatal("data not below full-width table")
		}
	}
}

func TestListHalfPageAndBodyPageAreIndependent(t *testing.T) {
	m, _ := ready(t)
	half := m.listHalfPage()
	m.Update(key("D"))
	if m.detailOffset != m.detailPageHeight() || m.selected != 0 || m.top != 0 {
		t.Fatal("D changed list or did not page body")
	}
	m.Update(key("U"))
	if m.detailOffset != 0 {
		t.Fatal("U did not return body to top")
	}
	m.Update(key("d"))
	if m.selected != half || m.top != half || m.detailOffset != 0 {
		t.Fatalf("d not half-page: selected=%d top=%d", m.selected, m.top)
	}
	m.Update(key("u"))
	if m.selected != 0 || m.top != 0 {
		t.Fatal("u not reverse half-page")
	}
	m.Update(key("j"))
	if m.selected != 1 {
		t.Fatal("j no longer selects one row")
	}
	// Kitty等增強鍵盤協定可能將Shift與文字分開回報。
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "D", Mod: tea.ModShift, ShiftedCode: 'D'})
	if m.selected != 1 || m.detailOffset != m.detailPageHeight() {
		t.Fatal("Shift+D did not page body")
	}
	id, top := m.selectedID(), m.top
	for i := 0; i < 100; i++ {
		m.Update(key("D"))
	}
	if m.selectedID() != id || m.top != top || m.detailOffset != len(m.detailLines())-m.detailPageHeight() {
		t.Fatal("body endpoint changed list")
	}
	m.Update(key("k"))
	if m.selected != 0 || m.detailOffset != 0 {
		t.Fatal("new selection did not reset body")
	}
	m.Update(key("ctrl+p"))
	for _, name := range []string{"u", "d", "U", "D"} {
		m.Update(key(name))
	}
	if m.selected != 0 || m.detailOffset != 0 {
		t.Fatal("popup leaked navigation")
	}
}

func TestHalfPageAcrossLazyBoundaryAndError(t *testing.T) {
	m, f := ready(t)
	for i := 0; i < 48; i++ {
		m.Update(key("j"))
	}
	oldIndex, oldTop := m.selected, m.top
	_, cmd := m.Update(key("d"))
	if cmd == nil {
		t.Fatal("half-page did not load history")
	}
	m.Update(cmd())
	if m.selected != oldIndex+m.listHalfPage() || m.top != oldTop+m.listHalfPage() {
		t.Fatal("lazy page lost half-page distance")
	}
	// 返回最末筆後，半頁仍不得越界。
	for m.next > 0 {
		m.selected = len(m.rows) - 1
		_, cmd = m.Update(key("d"))
		if cmd == nil {
			t.Fatal("missing tail load")
		}
		m.Update(cmd())
	}
	m.selected = len(m.rows) - 1
	m.Update(key("d"))
	if m.selected != len(m.rows)-1 {
		t.Fatal("selection beyond tail")
	}
	m2, _ := ready(t)
	m2.selected = 49
	m2.clampLayout()
	before := m2.selectedID()
	f.err = errors.New("offline")
	// 使用同一故障loader，失敗必須留在原選取，不先跳到尚未取得的列。
	m2.loader = f
	_, cmd = m2.Update(key("d"))
	m2.Update(cmd())
	if m2.selectedID() != before || m2.lastError != "offline" {
		t.Fatal("failed page lost selection")
	}
}

func TestTallTerminalHalfPageLoadsMoreThanOneDataPage(t *testing.T) {
	m, _ := ready(t)
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 1000})
	_, cmd := m.Update(key("d"))
	if cmd == nil {
		t.Fatal("no half-page fetch")
	}
	m.Update(cmd())
	if len(m.rows) != 125 || m.selected != 124 || m.next != 0 {
		t.Fatalf("large half-page stopped at one data page: rows=%d selected=%d next=%d", len(m.rows), m.selected, m.next)
	}
}

func TestPendingPageDoesNotOverrideNewSelection(t *testing.T) {
	m, _ := ready(t)
	m.selected = 49
	m.clampLayout()
	_, cmd := m.Update(key("d"))
	m.Update(key("k"))
	id := m.selectedID()
	m.Update(cmd())
	if m.selectedID() != id {
		t.Fatal("late page overwrote more recent selection")
	}
}

func TestMetadataAllLinesReachableAndIndependent(t *testing.T) {
	m, f := ready(t)
	m.Update(tea.WindowSizeMsg{Width: 74, Height: 16})
	f.metadataKey = strings.Repeat("long-key", 100)
	m.rows[0].IdempotencyKey = f.metadataKey
	m.Update(key("D"))
	bodyOffset, id, top := m.detailOffset, m.selectedID(), m.top
	seen := map[string]bool{}
	for {
		screen := strings.Split(ansi.Strip(m.View().Content), "\n")
		start := m.tableHeight() + 2
		for _, line := range screen[start : start+m.detailPageHeight()] {
			seen[strings.TrimSpace(ansi.Cut(line, 1, m.metadataWidth()+1))] = true
		}
		old := m.metadataOffset
		m.Update(key("]"))
		if old == m.metadataOffset {
			break
		}
	}
	for _, line := range m.metadataLines() {
		if !seen[strings.TrimSpace(line)] {
			t.Fatalf("unreachable metadata: %q", line)
		}
	}
	if m.detailOffset != bodyOffset || m.selectedID() != id || m.top != top {
		t.Fatal("metadata key changed other pane")
	}
	offset := m.metadataOffset
	_, cmd := m.Update(key("r"))
	m.Update(cmd())
	if m.metadataOffset != offset || m.detailOffset != bodyOffset {
		t.Fatal("refresh lost independent offsets")
	}
	m.Update(key("U"))
	if m.metadataOffset != offset {
		t.Fatal("body key moved metadata")
	}
	m.Update(key("ctrl+p"))
	m.Update(key("["))
	m.Update(key("]"))
	if m.metadataOffset != offset {
		t.Fatal("popup leaked metadata keys")
	}
	m.Update(key("esc"))
	for i := 0; i < 100; i++ {
		m.Update(key("["))
	}
	if m.metadataOffset != 0 {
		t.Fatal("metadata top clamp failed")
	}
	m.Update(key("]"))
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 200})
	if m.metadataOffset != 0 {
		t.Fatal("resize did not clamp metadata")
	}
	m.Update(tea.WindowSizeMsg{Width: 74, Height: 16})
	m.Update(key("]"))
	m.Update(key("D"))
	m.Update(key("j"))
	if m.metadataOffset != 0 || m.detailOffset != 0 {
		t.Fatal("new selection did not reset both panes")
	}
	m.Update(key("]"))
	m.Update(key("ctrl+p"))
	m.Update(key("right"))
	m.Update(key("enter"))
	defer m.cancelFetch()
	if m.metadataOffset != 0 || m.detailOffset != 0 {
		t.Fatal("new filters did not reset offsets")
	}
}

func TestNarrowHelpAndMetadataIndicator(t *testing.T) {
	m, _ := ready(t)
	m.Update(tea.WindowSizeMsg{Width: 74, Height: 16})
	view := ansi.Strip(m.View().Content)
	lines := strings.Split(view, "\n")
	for _, s := range []string{"j/k", "u/d", "U/D", "[]", "^P", "^W", "q"} {
		if !strings.Contains(lines[len(lines)-1], s) {
			t.Fatalf("missing compact help %s", s)
		}
	}
	if !strings.Contains(view, "line 1/") || strings.Contains(view, "More metadata") {
		t.Fatal("line indicator missing or metadata row replaced")
	}
	m.rows = nil
	m.Update(key("]"))
	m.Update(key("["))
	if m.metadataOffset != 0 {
		t.Fatal("empty metadata scrolled")
	}
}
