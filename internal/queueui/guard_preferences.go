package queueui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

type guardMsg struct {
	generation uint64
	saving     bool
	settings   deliver.GuardSettings
	err        error
}

func (m *Model) guardRequest(save bool) tea.Cmd {
	client, ok := m.loader.(GuardClient)
	if !ok {
		m.guardError = m.text("Team guard unavailable.")
		return nil
	}
	if m.guardBusy {
		return nil
	}
	m.guardGeneration++
	generation := m.guardGeneration
	enabled := m.guardDraft
	m.guardBusy = true
	m.guardSaving = save
	m.guardError = ""
	// 命令不讀取模型可變欄位，避免與 Update 競爭。
	parent := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		var settings deliver.GuardSettings
		var err error
		if save {
			settings, err = client.SaveGuard(ctx, enabled)
		} else {
			settings, err = client.GuardSettings(ctx)
		}
		return guardMsg{generation: generation, saving: save, settings: settings, err: err}
	}
}
func (m *Model) guardUpdate(msg guardMsg) {
	if msg.generation != m.guardGeneration {
		return
	}
	m.guardBusy = false
	m.guardSaving = false
	if msg.err != nil {
		m.guardReady = false
		m.guardError = clean(msg.err.Error())
		// 寫入回應遺失不等於未寫入；必須重新讀取後才能再編輯。
		if msg.saving {
			m.guardUnconfirmed = true
			m.guardError = m.text("Team save unconfirmed; reload before retrying.") + " " + m.guardError
		}
		return
	}
	m.guardUnconfirmed = false
	m.guardSettings = msg.settings
	m.guardReady = true
	m.guardDraft = msg.settings.Policy.Enabled
	m.guardError = ""
	if msg.saving {
		m.configNotice = m.text("Preferences saved.")
		m.preferencesOpen = false
	}
}
func (m *Model) guardLines() []string {
	label := m.text("Claude interception (global)")
	value := m.text("Loading")
	if !m.guardBusy {
		value = m.text("Unavailable")
		if m.guardReady {
			value = m.text("Off")
			if m.guardDraft {
				value = m.text("On")
			}
		}
	}
	if m.width < 40 {
		label = "G"
	}
	lines := []string{label + ": " + value}
	if m.width >= 60 && m.height >= 18 {
		lines = append(lines, m.text("Question timing is recorded while off, ready to enable anytime."))
		lines = append(lines, m.text("If delivery stalls, try turning interception off."))
	}
	if m.guardSettings.DiscoveryError != "" && m.width >= 60 && m.height >= 18 {
		lines = append(lines, "! "+m.text("Seat discovery unavailable; observations may be incomplete."))
	}
	if m.guardError != "" {
		if m.width < 40 || m.height < 18 {
			lines[0] = "G: ! " + m.text("Reload required")
		} else {
			lines = append(lines, "! "+m.guardError)
		}
	}
	return lines
}
