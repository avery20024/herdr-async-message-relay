package queueui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestGuardPreferenceNotesUseMutedTheme(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, id := range themeIDs {
			for _, mono := range []bool{false, true} {
				m, _ := ready(t)
				m.language = language
				m.preferences.Theme, m.noColor, m.backgroundKnown = id, mono, true
				m.refreshTheme()
				m.Update(tea.WindowSizeMsg{Width: 180, Height: 30})
				m.preferencesOpen = true
				m.preferencesDraft = m.preferences
				m.preferenceField = 2
				screen := m.View().Content
				lines := strings.Split(ansi.Strip(screen), "\n")
				cells := helpScreenCells(screen)
				want := helpScreenCells(m.paintCanvas(m.styles.dialog.Render(m.styles.muted.Render("X"))))[0][0].Style
				for _, note := range m.guardLines()[1:3] {
					needle := string([]rune(note)[:8])
					found := false
					for y, line := range lines {
						offset := strings.Index(line, needle)
						if offset < 0 {
							continue
						}
						found = true
						x := ansi.StringWidth(line[:offset])
						got := cells[y][x].Style
						if !reflect.DeepEqual(got.Fg, want.Fg) || got.Attrs != want.Attrs {
							t.Fatalf("note not muted: lang=%s theme=%s mono=%v note=%q got=%+v want=%+v", language, id, mono, needle, got, want)
						}
					}
					if !found {
						t.Fatalf("missing hint %q", needle)
					}
				}
				label := m.text("Claude interception (global)")
				if !strings.Contains(ansi.Strip(screen), label) || strings.Contains(ansi.Strip(screen), "(team)") || strings.Contains(ansi.Strip(screen), "全團隊") {
					t.Fatalf("global label missing or old label remains: lang=%s", language)
				}
			}
		}
	}
}
