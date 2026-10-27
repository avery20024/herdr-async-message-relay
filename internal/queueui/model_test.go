package queueui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
	"github.com/charmbracelet/x/ansi"
)

type fakeLoader struct {
	calls       []int64
	filters     []Filter
	err         error
	metadataKey string
}

func (f *fakeLoader) Page(ctx context.Context, filter Filter, before int64) (deliver.BrowseResponse, error) {
	f.calls = append(f.calls, before)
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return deliver.BrowseResponse{}, f.err
	}
	start := int64(125)
	if before > 0 {
		start = before - 1
	}
	rows := []sqlc.BrowseMessagesRow{}
	for seq := start; seq > 0 && len(rows) < 50; seq-- {
		rows = append(rows, sqlc.BrowseMessagesRow{ID: fmt.Sprint(seq), Seq: seq, Sender: "dev", Recipient: "gm", State: "queued", CreatedAt: 1792771200000, IdempotencyKey: f.metadataKey, Body: strings.Repeat("中文 message\n", 100)})
	}
	next := int64(0)
	if len(rows) > 0 && rows[len(rows)-1].Seq > 1 {
		next = rows[len(rows)-1].Seq
	}
	return deliver.BrowseResponse{ProjectRoot: "/team", Items: rows, NextBefore: next, Senders: []string{"arch", "dev"}, Recipients: []string{"gm", "old-role"}}, nil
}
func key(name string) tea.KeyPressMsg {
	switch name {
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "ctrl+f":
		return tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
	case "ctrl+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	case "ctrl+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(name[0])}
}

// 派發當次批次（請求、spinner啟動與背景查詢）；回傳的timer由專項測試驗證，不在此無限循環。
func apply(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			apply(m, child)
		}
		return
	}
	m.Update(msg)
}
func ready(t *testing.T) (*Model, *fakeLoader) {
	t.Helper()
	f := &fakeLoader{}
	m := NewModel(t.Context(), f, "/team", i18n.English)
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 30})
	// 專項測試驗證 Init 的預設 timer；其餘案例只執行初次查詢，避免每例等一秒。
	apply(m, m.fetch(false))
	return m, f
}
func TestSelectionPagingRefreshAndErrors(t *testing.T) {
	m, f := ready(t)
	if !m.auto || m.selected != 0 || len(m.rows) != 50 {
		t.Fatal("bad defaults")
	}
	for i := 0; i < 49; i++ {
		m.Update(key("j"))
	}
	_, cmd := m.Update(key("j"))
	if cmd == nil {
		t.Fatal("no lazy load")
	}
	apply(m, cmd)
	if m.selectedID() != "75" || len(m.rows) != 100 {
		t.Fatalf("pagination %s %d", m.selectedID(), len(m.rows))
	}
	m.Update(key("D"))
	offset := m.detailOffset
	_, cmd = m.Update(key("r"))
	apply(m, cmd)
	if m.selectedID() != "75" || m.detailOffset != offset || len(f.calls) != 4 {
		t.Fatalf("refresh lost selection: %s %d calls=%v", m.selectedID(), m.detailOffset, f.calls)
	}
	f.err = errors.New("disconnected\x1b[2J")
	_, cmd = m.Update(key("r"))
	apply(m, cmd)
	if m.selectedID() != "75" || !strings.Contains(m.View().Content, "DATA NOT UPDATED") || strings.Contains(m.lastError, "\x1b") {
		t.Fatal("failure lost data or leaked control")
	}
	f.err = nil
	_, cmd = m.Update(key("r"))
	apply(m, cmd)
	if m.lastError != "" {
		t.Fatal("retry did not recover")
	}
}
func TestFilterDraftCancellationAndStaleResponse(t *testing.T) {
	m, f := ready(t)
	m.Update(key("ctrl+f"))
	m.Update(key("right"))
	if m.draft.Sender != "arch" || m.filter.Sender != "" {
		t.Fatal("draft altered active filter")
	}
	m.Update(key("esc"))
	if m.popup || m.filter.Sender != "" {
		t.Fatal("cancel applied draft")
	}
	_, old := m.Update(key("r"))
	oldToken := m.token
	m.Update(key("ctrl+f"))
	m.Update(key("right"))
	m.Update(key("down"))
	m.Update(key("right"))
	m.Update(key("down"))
	m.Update(key("right"))
	_, next := m.Update(key("enter"))
	if next == nil || m.filter != (Filter{Sender: "arch", Recipient: "gm", State: "queued"}) {
		t.Fatal("AND draft not applied")
	}
	// 已取消的舊回應不能覆蓋新篩選；即使請求稍後才完成也須拒絕。
	_ = old
	m.Update(pageMsg{token: oldToken, page: deliver.BrowseResponse{Items: []sqlc.BrowseMessagesRow{{ID: "stale"}}}})
	if len(m.rows) != 0 {
		t.Fatal("stale response replaced new filter")
	}
	apply(m, next)
	if f.filters[len(f.filters)-1] != m.filter {
		t.Fatal("query used old filter")
	}
}
func TestAutoTimerAndQuit(t *testing.T) {
	m, _ := ready(t)
	if _, cmd := m.Update(key("esc")); cmd != nil {
		t.Fatal("Esc quit main screen")
	}
	old := m.timer
	m.Update(key("ctrl+w"))
	if m.auto {
		t.Fatal("auto did not stop")
	}
	if _, cmd := m.Update(tickMsg(old)); cmd != nil {
		t.Fatal("initial tick kept refreshing after stop")
	}
	_, cmd := m.Update(key("ctrl+w"))
	if !m.auto || cmd == nil {
		t.Fatal("auto toggle missing timer")
	}
	old = m.timer
	m.Update(key("ctrl+w"))
	if _, cmd := m.Update(tickMsg(old)); cmd != nil {
		t.Fatal("old tick kept refreshing")
	}
	m.Update(key("ctrl+f"))
	if _, cmd := m.Update(key("q")); cmd != nil || !m.popup {
		t.Fatal("popup keys leaked")
	}
	_, cmd = m.Update(key("ctrl+c"))
	if cmd == nil {
		t.Fatal("Ctrl+C did not quit")
	}
}
func TestLayoutBoundsAndSanitization(t *testing.T) {
	m, _ := ready(t)
	m.rows[0].Body = "\x1b[2J\x1b]52;c;clipboard\x07中文🙂\n\tvisible\r\b\u202eevil"
	m.rows[0].Sender = "\x1b[31mdev\n攻擊"
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		m.language = lang
		for _, size := range [][2]int{{180, 30}, {160, 16}, {159, 24}, {100, 30}, {74, 16}, {73, 10}, {1, 1}} {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for _, popup := range []bool{false, true} {
				m.popup = popup
				view := m.View()
				if !view.AltScreen {
					t.Fatal("not alternate screen")
				}
				lines := strings.Split(view.Content, "\n")
				if len(lines) > size[1] {
					t.Fatalf("height overflow %v: %d", size, len(lines))
				}
				for _, line := range lines {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("width overflow %v: %d %q", size, ansi.StringWidth(line), line)
					}
				}
				plain := ansi.Strip(view.Content)
				if strings.Contains(plain, "clipboard") || strings.Contains(plain, "\u202e") || strings.Contains(plain, "\r") || strings.Contains(view.Content, "\x1b]52") {
					t.Fatal("terminal sequence escaped")
				}
			}
		}
	}
	if clean("\x1b[31mtext\x1b[0m\n🙂") != "text\n🙂" {
		t.Fatal("visible body damaged")
	}
}
func TestTimeEvidenceDoesNotInventDelivery(t *testing.T) {
	row := sqlc.BrowseMessagesRow{State: "submitted", BatchState: sql.NullString{String: "resolved_submitted", Valid: true}, BatchUpdatedAt: sql.NullInt64{Int64: 1000, Valid: true}}
	if delivered(row) != 0 {
		t.Fatal("manual confirmation treated as delivery time")
	}
	row.BatchState.String = "submitted"
	if delivered(row) != 1000 {
		t.Fatal("automatic record missing")
	}
	if stamp(1000, true) != "1970-01-01 08:00:01 +08:00" || stamp(0, false) != "—" {
		t.Fatal("incorrect timezone or missing time")
	}
}
