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
	settings deliver.GuardSettings
	err      error
	saves    int
}

func (g *guardLoader) GuardSettings(context.Context) (deliver.GuardSettings, error) {
	return g.settings, g.err
}
func (g *guardLoader) SaveGuard(_ context.Context, b bool) (deliver.GuardSettings, error) {
	g.saves++
	// 模擬已寫入但回應丟失，不能盲目再寄送。
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
func TestTeamGuardCtrlPSharedAndEscDraftOnly(t *testing.T) {
	g := &guardLoader{settings: deliver.GuardSettings{ProjectRoot: "/team", Policy: claudeguard.Policy{Version: 1, Enabled: true}, Observations: []claudeguard.Observation{}}}
	a, b := guardModel(t, g), guardModel(t, g)
	_, cmd := a.Update(key("ctrl+p"))
	apply(a, cmd)
	if !a.guardReady || !a.guardDraft {
		t.Fatal("default not loaded")
	}
	a.Update(key("down"))
	a.Update(key("down"))
	a.Update(key("left"))
	if a.guardDraft || !g.settings.Policy.Enabled {
		t.Fatal("draft wrote policy")
	}
	a.Update(key("esc"))
	if g.saves != 0 {
		t.Fatal("Esc wrote policy")
	}
	_, cmd = a.Update(key("ctrl+p"))
	apply(a, cmd)
	a.Update(key("down"))
	a.Update(key("down"))
	a.Update(key("left"))
	_, cmd = a.Update(key("enter"))
	if !a.guardBusy {
		t.Fatal("not saving")
	}
	apply(a, cmd)
	if a.preferencesOpen || g.settings.Policy.Enabled || g.saves != 1 {
		t.Fatal("save failed")
	}
	_, cmd = b.Update(key("ctrl+p"))
	apply(b, cmd)
	if b.guardDraft {
		t.Fatal("not team shared")
	}
	if len(g.calls) != 0 {
		t.Fatal("settings fetched history")
	}
}
func TestTeamGuardUnconfirmedSaveRequiresReload(t *testing.T) {
	g := &guardLoader{settings: deliver.GuardSettings{ProjectRoot: "/team", Policy: claudeguard.Policy{Version: 1, Enabled: true}, Observations: []claudeguard.Observation{}}}
	m := guardModel(t, g)
	_, cmd := m.Update(key("ctrl+p"))
	apply(m, cmd)
	m.preferenceField = 2
	m.cyclePreference(1)
	g.err = errors.New("connection lost\x1b[2J")
	_, cmd = m.Update(key("enter"))
	apply(m, cmd)
	if m.guardReady || !m.preferencesOpen || !strings.Contains(m.guardError, "unconfirmed") || strings.Contains(m.guardError, "\x1b") {
		t.Fatal(m.guardError)
	}
	m.cyclePreference(1)
	_, retryCmd := m.Update(key("enter"))
	if retryCmd != nil || !m.preferencesOpen {
		t.Fatal("unconfirmed save claimed success without reload")
	}
	if g.saves != 1 {
		t.Fatal("blind retry")
	}
	g.err = nil
	_, cmd = m.Update(key("ctrl+r"))
	apply(m, cmd)
	if !m.guardReady || m.guardDraft {
		t.Fatal("reload failed to observe committed save")
	}
	// 舊載入結果不能覆蓋新的團隊草稿。
	m.guardUpdate(guardMsg{generation: m.guardGeneration - 1, settings: deliver.GuardSettings{Policy: claudeguard.Policy{Enabled: true}}})
	if m.guardDraft {
		t.Fatal("stale result overwrote policy")
	}
}
func TestGuardAndLocalErrorsFitMinimumTerminal(t *testing.T) {
	for _, width := range []int{20, 39, 40, 60, 100} {
		for _, height := range []int{10, 11, 18, 30} {
			m, _ := ready(t)
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m.preferencesOpen = true
			m.preferencesDraft = m.preferences
			m.configError = "local preferences unreadable"
			m.guardError = "team save unconfirmed"
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

func TestUnavailableGuardDoesNotWriteDefaults(t *testing.T) {
	m, _ := ready(t)
	m.preferencesStore = testStore(t)
	_, cmd := m.Update(key("ctrl+p"))
	apply(m, cmd)
	if m.guardReady {
		t.Fatal("missing API claimed ready")
	}
	m.preferenceField = 2
	m.cyclePreference(1)
	_, cmd = m.Update(key("enter"))
	apply(m, cmd)
	if m.preferencesOpen {
		t.Fatal("unavailable team API blocked local preferences")
	}
}
