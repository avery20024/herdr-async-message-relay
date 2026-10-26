package queueui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestMetadataNotesPinnedAndAllFieldsReachable(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, size := range [][2]int{{180, 30}, {74, 16}, {20, 10}} {
			m, _ := ready(t)
			m.language = lang
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.rows[0].IdempotencyKey = strings.Repeat("long中文-key/", 25)
			seen := map[string]bool{}
			for {
				lines := strings.Split(ansi.Strip(m.View().Content), "\n")
				if len(lines) != size[1] {
					t.Fatalf("wrong height at %v", size)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) != size[0] {
						t.Fatalf("wrong width at %v: %q", size, line)
					}
				}
				start := m.tableHeight() + 2
				for _, line := range lines[start : start+m.metadataPageHeight()] {
					seen[strings.TrimSpace(ansi.Cut(line, 1, m.metadataWidth()+1))] = true
				}
				for i, note := range m.metadataNotes() {
					got := ansi.Cut(lines[start+m.metadataPageHeight()+i], 1, m.metadataWidth()+1)
					if got != cell(note, m.metadataWidth()) {
						t.Fatalf("note moved or differs at %v: %q", size, got)
					}
				}
				old := m.metadataOffset
				m.Update(key("]"))
				if m.metadataOffset == old {
					break
				}
			}
			for _, line := range m.metadataLines() {
				if !seen[strings.TrimSpace(line)] {
					t.Fatalf("unreachable metadata at %v lang=%s: %q", size, lang, line)
				}
			}
		}
	}
}

func TestMetadataValuesStyledAfterSanitization(t *testing.T) {
	for _, id := range themeIDs {
		for _, mono := range []bool{false, true} {
			m, _ := ready(t)
			m.preferences.Theme, m.noColor, m.backgroundKnown = id, mono, true
			m.refreshTheme()
			m.rows[0].Sender = "\x1b[31mSENDER-VALUE\x1b[0m\x1b]0;BAD-TITLE\a"
			plain := m.metadataLines()
			styled := m.metadataContent(true)
			if len(plain) != len(styled) {
				t.Fatal("styling changed wrapping")
			}
			for i := range plain {
				if ansi.Strip(styled[i]) != plain[i] || strings.Contains(styled[i], "BAD-TITLE") {
					t.Fatal("styling changed text or leaked terminal control sequences")
				}
			}
			gotField := helpScreenCells(styled[0])[0]
			wantField := helpScreenCells("Sender: " + m.styles.accent.Render("SENDER-VALUE"))[0]
			if !reflect.DeepEqual(gotField, wantField) {
				t.Fatalf("key styled or value not accented: %q", styled[0])
			}
			// 各主題與窄版需維持相同文字分行，且捲入值的中段不能把粗體帶到外框。
			m.rows[0].IdempotencyKey = strings.Repeat("long中文-key/", 20)
			for _, size := range [][2]int{{20, 10}, {74, 16}, {180, 30}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				plainLines, styledLines := m.metadataLines(), m.metadataContent(true)
				if len(plainLines) != len(styledLines) {
					t.Fatalf("line counts differ for %s at %v", id, size)
				}
				for i := range plainLines {
					if plainLines[i] != ansi.Strip(styledLines[i]) {
						t.Fatalf("wrap differs for %s at %v line=%d", id, size, i)
					}
				}
				m.metadataOffset = len(plainLines) / 2
				m.clampLayout()
				screen := helpScreenCells(m.View().Content)
				if screen[m.height-2][0].Style.Attrs != 0 {
					t.Fatalf("metadata emphasis leaked into frame for %s at %v", id, size)
				}
			}
			m.metadataOffset = 0
			view := helpScreenCells(m.View().Content)
			for i, note := range m.metadataNotes() {
				want := helpScreenCells(m.paintCanvas(m.styles.muted.Render(cell(note, m.metadataWidth()))))[0]
				got := view[m.tableHeight()+2+m.metadataPageHeight()+i][1 : 1+m.metadataWidth()]
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("note does not share muted style for %s mono=%v", id, mono)
				}
			}
		}
	}
}
