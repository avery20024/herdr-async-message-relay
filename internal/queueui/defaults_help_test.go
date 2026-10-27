package queueui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

func TestAutoRefreshStartsByDefault(t *testing.T) {
	m := NewModel(t.Context(), &fakeLoader{}, "/team", i18n.English)
	if !m.auto {
		t.Fatal("auto refresh must default on")
	}
	commands, ok := m.Init()().(tea.BatchMsg)
	if !ok || len(commands) != 3 {
		t.Fatal("Init must schedule initial fetch, background query and auto timer")
	}
	message := commands[2]()
	tick, ok := message.(tickMsg)
	if !ok || uint64(tick) != m.timer {
		t.Fatal("initial refresh timer missing")
	}
	m.cancelFetch()
}

func TestGuardHintsAndFourHelpGroups(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		m := NewModel(t.Context(), &fakeLoader{}, "/team", language)
		m.width, m.height = 180, 30
		hints := m.guardLines()
		if len(hints) != 3 {
			t.Fatalf("want label and two hints, got %v", hints)
		}
		if language == i18n.TraditionalChinese {
			if hints[1] != "關閉攔截仍會持續記錄問答時間，以利隨時開啓" || hints[2] != "若發現訊息送達有阻塞，可試著關閉設攔截功能" {
				t.Fatal(hints)
			}
		}
		document := ansi.Strip(strings.Join(m.helpDocument(), "\n"))
		last := -1
		for _, group := range []string{"Browsing", "Filters", "Settings", "Other"} {
			heading := m.text(group)
			offset := strings.Index(document, heading)
			if offset <= last {
				t.Fatalf("missing or unordered heading %q", heading)
			}
			last = offset
		}
		if strings.Contains(strings.Join(hints, "\n"), "uninitialized") || strings.Contains(strings.Join(hints, "\n"), "未初始化") {
			t.Fatal("removed status hints remain")
		}
	}
}
