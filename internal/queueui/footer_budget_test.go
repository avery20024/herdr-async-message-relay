package queueui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterTailBudgetAcrossScreensAndWidths(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, screen := range []string{"main", "filters", "preferences", "help"} {
			for _, state := range []string{"normal", "reload", "saved", "stale", "cfg"} {
				for _, auto := range []bool{false, true} {
					for _, width := range []int{20, 24, 30, 39, 40, 60, 67, 99, 100, 110, 120, 130, 168} {
						t.Run(fmt.Sprint(lang, screen, state, auto, width), func(t *testing.T) {
							m, _ := ready(t)
							m.language = lang
							m.width = width
							m.auto = auto
							m.refreshTheme()
							m.popup = screen == "filters"
							m.preferencesOpen = screen == "preferences"
							m.helpOpen = screen == "help"
							tail := "1/50+"
							if m.popup {
								tail = m.text("Filters") + " 1/3"
							}
							if m.preferencesOpen {
								tail = m.text("Preferences") + " 1/3"
							}
							switch state {
							case "reload":
								m.configNotice = m.text("Preferences reloaded.")
								if screen == "main" {
									tail = m.configNotice
								}
							case "saved":
								m.configNotice = m.text("Preferences saved.")
								if screen == "main" {
									tail = m.configNotice
								}
							case "stale":
								m.lastError = "socket offline"
								tail = m.staleText() + ": " + m.lastError
							case "cfg":
								m.configError = "invalid settings"
								tail = "! CFG: " + m.configError
							}
							footer := ansi.Strip(m.footerLine())
							if ansi.StringWidth(footer) != width {
								t.Fatal("width", footer)
							}
							if m.helpOpen {
								content := m.helpContent()
								if strings.TrimSpace(footer) != "" || strings.TrimSpace(ansi.Strip(content[len(content)-1])) != m.text("ESC close") {
									t.Fatal("help must show only muted ESC hint in panel")
								}
								return
							}
							if !strings.Contains(footer, "?") {
								t.Fatal("help hint lost", footer)
							}
							choices := m.helpChoices()
							minimum := ansi.StringWidth(choices[len(choices)-1])
							modeWidth := ansi.StringWidth(m.text("Manual")) + 3
							if auto {
								modeWidth = ansi.StringWidth(m.text("Auto (1s)")) + 3
							}
							if width < 40 {
								modeWidth = 2
							}
							if modeWidth+1+minimum+1+ansi.StringWidth(tail) <= width && !strings.HasSuffix(strings.TrimRight(footer, " "), tail) {
								t.Fatalf("tail could fit but got truncated: want %q footer %q", tail, footer)
							}
							switch screen {
							case "main":
								for _, keys := range []string{"P", "^FPRW", "q"} {
									if width < 40 && !strings.Contains(footer, keys) {
										t.Fatal("main key lost", keys, footer)
									}
								}
							case "filters", "preferences":
								for _, keys := range []string{"↑↓", "←→", "Enter", "Esc"} {
									if !strings.Contains(footer, keys) {
										t.Fatal("dialog key lost", keys, footer)
									}
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestFooterRegressionCountsLabelsAndFeedback(t *testing.T) {
	m, _ := ready(t)
	m.width = 120
	if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, "1/50+") {
		t.Fatal("120-column count regression", footer)
	}
	for _, notice := range []string{"Preferences reloaded.", "Preferences saved."} {
		m.configNotice = notice
		if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, notice) {
			t.Fatal("120-column feedback regression", footer)
		}
	}
	m.configNotice = ""
	m.preferencesOpen = true
	m.width = 40
	if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, "Preferences 1/3") {
		t.Fatal("40-column preferences label regression", footer)
	}
	m.language = i18n.TraditionalChinese
	m.width = 30
	if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, "偏好設定 1/3") {
		t.Fatal("30-column preferences label regression", footer)
	}
	m.preferencesOpen = false
	m.width = 100
	if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, "1/50+") {
		t.Fatal("100-column Traditional Chinese count regression", footer)
	}
}
