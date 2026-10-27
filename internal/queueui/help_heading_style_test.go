package queueui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpGroupHeadingsAlwaysBold(t *testing.T) {
	bold := helpScreenCells(lipgloss.NewStyle().Bold(true).Render("X"))[0][0].Style.Attrs
	for _, language := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, themeID := range themeIDs {
			for _, mono := range []bool{false, true} {
				m, _ := ready(t)
				m.language = language
				m.preferences.Theme, m.noColor = themeID, mono
				m.refreshTheme()
				// 小標題的粗體是固定語意，不依賴色票是否原本提供粗體。
				m.styles.heading = m.styles.heading.Bold(false)
				for _, title := range []string{"Browsing", "Filters", "Settings", "Other"} {
					found := false
					for _, line := range m.helpDocument() {
						if strings.TrimSpace(ansi.Strip(line)) != m.text(title) {
							continue
						}
						found = true
						if helpScreenCells(line)[0][0].Style.Attrs&bold == 0 {
							t.Fatalf("heading not bold: %s %s mono=%v %s", language, themeID, mono, title)
						}
					}
					if !found {
						t.Fatalf("missing heading: %s", title)
					}
				}
			}
		}
	}
}
