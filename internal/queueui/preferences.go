package queueui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

func (m *Model) applyPreferences(p Preferences) {
	m.preferences = p
	m.language = m.baseLanguage
	if p.Language != "auto" {
		m.language = i18n.Language(p.Language)
	}
	m.refreshTheme()
}

func (m *Model) reloadPreferences() bool {
	if m.preferencesStore == nil {
		m.configError = m.text("Preferences storage unavailable.")
		m.configUnreadable = true
		return false
	}
	p, err := m.preferencesStore.Load()
	if err == nil {
		err = p.validate()
	}
	if err != nil {
		m.configError = clean(m.text("Unable to load preferences.") + " " + err.Error())
		m.configUnreadable = true
		return false
	}
	m.applyPreferences(p)
	m.configError = ""
	m.configUnreadable = false
	m.configNotice = m.text("Preferences reloaded.")
	if m.preferencesOpen {
		m.preferencesDraft = p
	}
	return true
}

func (m *Model) openPreferences() tea.Cmd {
	m.reloadPreferences()
	// 開窗讀取是草稿初始化，不把背景載入回饋留在關窗後的計數位置。
	m.configNotice = ""
	m.preferencesDraft = m.preferences
	m.preferenceField = 0
	m.preferencesOpen = true
	return m.guardRequest(false)
}

func (m *Model) savePreferences() tea.Cmd {
	if m.guardBusy || m.guardUnconfirmed {
		return nil
	}
	var err error
	if m.preferencesStore == nil || m.configUnreadable {
		err = errors.New("reload valid preferences before saving")
	} else {
		err = m.preferencesStore.Save(m.preferencesDraft)
	}
	if err != nil {
		m.configError = clean(m.text("Unable to save preferences.") + " " + err.Error())
		return nil
	}
	m.applyPreferences(m.preferencesDraft)
	m.configError = ""
	m.configNotice = m.text("Preferences saved.")
	if m.guardReady && m.guardDraft != m.guardSettings.Policy.Enabled {
		m.configNotice = m.text("Local preferences saved; team save pending.")
		return m.guardRequest(true)
	}
	m.preferencesOpen = false
	return nil
}

func (m *Model) cyclePreference(direction int) {
	if m.preferenceField == 2 {
		if m.guardReady && !m.guardBusy {
			m.guardDraft = !m.guardDraft
		}
		return
	}
	values := themeIDs
	current := &m.preferencesDraft.Theme
	if m.preferenceField == 0 {
		values = []string{"auto", "en", "zh-TW"}
		current = &m.preferencesDraft.Language
	}
	index := 0
	for i, value := range values {
		if value == *current {
			index = i
			break
		}
	}
	*current = values[(index+direction+len(values))%len(values)]
}

func (m *Model) preferencesKey(key string) tea.Cmd {
	if m.guardSaving {
		return nil
	}
	switch key {
	case "esc":
		m.preferencesOpen = false
	case "up", "k":
		m.preferenceField = (m.preferenceField + 2) % 3
	case "down", "j":
		m.preferenceField = (m.preferenceField + 1) % 3
	case "left", "h":
		m.cyclePreference(-1)
	case "right", "l":
		m.cyclePreference(1)
	case "enter":
		return m.savePreferences()
	}
	return nil
}

func (m *Model) preferencesLines() []string {
	language := map[string]string{"auto": m.text("Auto"), "en": "English", "zh-TW": "繁體中文"}[m.preferencesDraft.Language]
	name := m.text("Terminal (default)")
	if p, ok := palettes[m.preferencesDraft.Theme]; ok {
		name = p.name
		if m.width < 40 {
			name = p.short
		}
	} else if m.width < 40 {
		name = "Terminal"
	}
	langLabel, themeLabel := m.text("Language"), m.text("Theme")
	if m.width < 40 {
		langLabel, themeLabel = "L", "T"
	}
	lines := []string{m.text("Preferences"), langLabel + ": " + language, themeLabel + ": " + name}
	lines = append(lines, m.guardLines()...)
	if m.configError != "" {
		if m.height < 12 {
			lines[0] = "! CFG"
		} else {
			lines = append(lines, "! "+m.configError)
		}
	}
	return lines
}
