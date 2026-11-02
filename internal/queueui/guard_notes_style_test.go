package queueui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestGuardPreferenceAndNotesAreHidden(t *testing.T) {
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
				screen := ansi.Strip(m.View().Content)
				for _, text := range []string{
					"Claude interception (global)",
					"Question timing is recorded while off, ready to enable anytime.",
					"If delivery stalls, try turning interception off.",
				} {
					if strings.Contains(screen, m.text(text)) {
						t.Fatalf("hidden guard content visible: lang=%s theme=%s mono=%v text=%q", language, id, mono, text)
					}
				}
				if len(m.preferencesLines()) != 3 {
					t.Fatal("preferences should contain only title, language and theme")
				}
			}
		}
	}
}
