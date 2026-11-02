package queueui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/charmbracelet/x/ansi"
)

type guardLoader struct {
	fakeLoader
	settings     deliver.GuardSettings
	err          error
	loads, saves int
}

func (g *guardLoader) GuardSettings(context.Context) (deliver.GuardSettings, error) {
	g.loads++
	return g.settings, g.err
}
func (g *guardLoader) SaveGuard(_ context.Context, b bool) (deliver.GuardSettings, error) {
	g.saves++
	g.settings.Policy.Enabled = b
	return g.settings, g.err
}
func guardModel(t *testing.T, g *guardLoader) *Model {
	t.Helper()
	m := NewModel(t.Context(), g, "/team", i18n.English)
	m.preferencesStore = testStore(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func TestPreferencesDoNotAccessHiddenGuard(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		g := &guardLoader{settings: deliver.GuardSettings{ProjectRoot: "/team", Policy: claudeguard.Policy{Version: 1, Enabled: enabled}}, err: errors.New("guard unavailable")}
		m := guardModel(t, g)
		// 即使殘留攔截狀態，語言／主題也不可被隱藏設定阻塞或觸發政策儲存。
		m.guardReady, m.guardDraft = true, !enabled
		m.guardBusy, m.guardSaving, m.guardUnconfirmed = true, true, true
		for _, k := range []string{"ctrl+p", "down", "right", "ctrl+r", "enter", "ctrl+p", "esc"} {
			_, cmd := m.Update(key(k))
			apply(m, cmd)
		}
		if m.preferencesOpen || g.loads != 0 || g.saves != 0 || g.settings.Policy.Enabled != enabled {
			t.Fatalf("hidden guard accessed: loads=%d saves=%d policy=%v", g.loads, g.saves, g.settings.Policy.Enabled)
		}
		if len(g.calls) != 0 {
			t.Fatal("preferences fetched history")
		}
	}
}

func TestPreferencesNavigationHasOnlyLanguageAndTheme(t *testing.T) {
	m, _ := ready(t)
	m.preferencesStore = testStore(t)
	m.openPreferences()
	for _, k := range []string{"down", "j", "up", "k"} {
		before := m.preferenceField
		m.Update(key(k))
		if m.preferenceField != 1-before {
			t.Fatalf("%s selected hidden field: %d", k, m.preferenceField)
		}
	}
	m.preferencesKey("right")
	if m.preferencesDraft.Language != "en" {
		t.Fatal("language no longer editable")
	}
	m.preferencesKey("down")
	m.preferencesKey("right")
	if m.preferencesDraft.Theme != "catppuccin-latte" {
		t.Fatal("theme no longer editable")
	}
	m.preferencesKey("enter")
	if m.preferencesOpen || m.preferences.Language != "en" || m.preferences.Theme != "catppuccin-latte" {
		t.Fatal("local preferences not saved")
	}
}

func TestLocalErrorsFitMinimumTerminal(t *testing.T) {
	for _, width := range []int{20, 39, 40, 60, 100} {
		for _, height := range []int{10, 11, 18, 30} {
			m, _ := ready(t)
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m.preferencesOpen = true
			m.preferencesDraft = m.preferences
			m.configError = "local preferences unreadable"
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if len(lines) != height {
				t.Fatalf("%dx%d rendered %d rows", width, height, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != width {
					t.Fatalf("%dx%d overflow: %q", width, height, line)
				}
			}
		}
	}
}
