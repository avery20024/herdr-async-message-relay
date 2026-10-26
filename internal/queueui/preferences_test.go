package queueui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func testStore(t *testing.T) FilePreferencesStore {
	t.Helper()
	return FilePreferencesStore{Path: filepath.Join(t.TempDir(), "hrelay", "ui-preferences.json")}
}

func TestPreferencesNoticeDismissesOnNextOperation(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, operation := range []string{"j", "k", "u", "d", "U", "D", "[", "]", "r", "P", "ctrl+f", "ctrl+w"} {
			t.Run(string(lang)+"/"+operation, func(t *testing.T) {
				m, _ := ready(t)
				defer m.cancelFetch()
				m.baseLanguage = lang
				m.language = lang
				m.preferencesStore = testStore(t)
				m.Update(key("ctrl+p"))
				if m.configNotice != "" {
					t.Fatal("opening Preferences left a reload notice")
				}
				m.Update(key("enter"))
				if m.configNotice != m.text("Preferences saved.") {
					t.Fatal("save feedback missing")
				}
				m.Update(key(operation))
				if m.configNotice != "" {
					t.Fatalf("%s did not dismiss save feedback", operation)
				}
				if !m.popup && !m.preferencesOpen {
					count := fmt.Sprintf("%d/%d+", m.selected+1, len(m.rows))
					if footer := ansi.Strip(m.footerLine()); !strings.Contains(footer, count) {
						t.Fatalf("count did not return after %s: %q", operation, footer)
					}
				}
			})
		}
	}
	m, _ := ready(t)
	defer m.cancelFetch()
	m.preferencesStore = testStore(t)
	m.Update(key("ctrl+r"))
	if m.configNotice != m.text("Preferences reloaded.") {
		t.Fatal("manual reload feedback missing")
	}
	m.auto = true
	m.timer = 17
	if _, cmd := m.Update(tickMsg(17)); cmd == nil || m.configNotice != "" {
		t.Fatal("automatic fetch did not restore count")
	}
}

func TestPreferencesNoticeCannotHidePopupLabels(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, width := range []int{20, 40, 120, 168} {
			for _, prefs := range []bool{false, true} {
				m, f := ready(t)
				m.baseLanguage = lang
				m.language = lang
				m.preferencesStore = testStore(t)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
				if prefs {
					m.Update(key("ctrl+p"))
				} else {
					m.Update(key("ctrl+f"))
				}
				count := len(f.calls)
				m.Update(key("ctrl+r"))
				if m.configNotice == "" || len(f.calls) != count {
					t.Fatal("reload lost feedback or issued HTTP")
				}
				// 一般notice也不能遮住popup欄標；設定與資料錯誤不屬於此類一次性提示。
				m.notice = "OLD SELECTION NOTICE"
				footer := ansi.Strip(m.footerLine())
				if strings.Contains(footer, m.configNotice) || strings.Contains(footer, m.notice) {
					t.Fatal("notice overwrote popup label", footer)
				}
				if width >= 120 {
					want := m.text("Filters") + " 1/3"
					if prefs {
						want = m.text("Preferences") + " 1/3"
					}
					if !strings.Contains(footer, want) {
						t.Fatal("missing popup position", want, footer)
					}
				}
				m.Update(key("esc"))
				if m.configNotice != "" {
					t.Fatal("Esc left a popup reload notification")
				}
			}
		}
	}
}

func TestPreferencesNoticeDismissalKeepsConfigErrors(t *testing.T) {
	for _, operation := range []string{"j", "r", "ctrl+f", "ctrl+p", "ctrl+w"} {
		m, _ := ready(t)
		defer m.cancelFetch()
		s := testStore(t)
		m.preferencesStore = s
		if err := s.Save(defaultPreferences()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.Path, []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
		m.Update(key("ctrl+r"))
		if m.configError == "" {
			t.Fatal("corrupt preferences were not reported")
		}
		m.Update(key(operation))
		if m.configError == "" || !strings.Contains(ansi.Strip(m.View().Content), "CFG") {
			t.Fatalf("%s dismissed config error", operation)
		}
	}
}

func TestPreferencesMissingAtomicPrivateAndValidation(t *testing.T) {
	s := testStore(t)
	if p, err := s.Load(); err != nil || p != defaultPreferences() {
		t.Fatal(p, err)
	}
	if _, err := os.Stat(s.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("load created preferences")
	}
	p := Preferences{Version: 1, Language: "zh-TW", Theme: "catppuccin-latte"}
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Load(); err != nil || got != p {
		t.Fatal(got, err)
	}
	file, _ := os.Stat(s.Path)
	dir, _ := os.Stat(filepath.Dir(s.Path))
	if file.Mode().Perm() != 0600 || dir.Mode().Perm() != 0700 {
		t.Fatal("preferences permissions not private")
	}
	for _, data := range []string{"", "null", "[]", `{"version":2,"language":"en","theme":"dracula"}`, `{"version":1,"language":"fr","theme":"dracula"}`, `{"version":1,"language":"en","theme":"nope"}`, `{"version":1,"language":"en","theme":"dracula","extra":1}`, `{"version":1,"language":"en","theme":"dracula","theme":"vesper"}`, `{"version":1,"language":"en","theme":"dracula"} {}`, strings.Repeat(" ", preferencesLimit+1)} {
		if err := os.WriteFile(s.Path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(); err == nil {
			t.Fatalf("accepted invalid preferences %q", data)
		}
		if err := s.Save(p); err == nil {
			t.Fatal("overwrote unreadable preferences")
		}
		got, _ := os.ReadFile(s.Path)
		if string(got) != data {
			t.Fatal("failed save modified original")
		}
	}
}

func TestPreferencesRejectSymlinkAndRelativePaths(t *testing.T) {
	target := testStore(t)
	if err := target.Save(defaultPreferences()); err != nil {
		t.Fatal(err)
	}
	link := FilePreferencesStore{Path: filepath.Join(t.TempDir(), "linked.json")}
	if err := os.Symlink(target.Path, link.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := link.Load(); err == nil {
		t.Fatal("read symlink preferences")
	}
	if err := link.Save(defaultPreferences()); err == nil {
		t.Fatal("saved over symlink")
	}
	dirLink := filepath.Join(t.TempDir(), "linked-dir")
	if err := os.Symlink(filepath.Dir(target.Path), dirLink); err != nil {
		t.Fatal(err)
	}
	if _, err := (FilePreferencesStore{Path: filepath.Join(dirLink, "prefs.json")}).Load(); err == nil {
		t.Fatal("accepted symlink preferences directory")
	}
	t.Setenv("HERDR_RELAY_UI_CONFIG", "relative.json")
	if _, err := preferencesPath(); err == nil {
		t.Fatal("accepted relative config override")
	}
	t.Setenv("HERDR_RELAY_UI_CONFIG", target.Path)
	if got, err := preferencesPath(); err != nil || got != target.Path {
		t.Fatal(got, err)
	}
}

func TestPreferencesConcurrentReadersNeverSeePartialSave(t *testing.T) {
	s := testStore(t)
	if err := s.Save(defaultPreferences()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				p := Preferences{Version: 1, Language: "en", Theme: themeIDs[(n+i)%len(themeIDs)]}
				if err := s.Save(p); err != nil {
					t.Error(err)
				}
				if _, err := s.Load(); err != nil {
					t.Error("partial preferences read", err)
				}
			}
		}(n)
	}
	wg.Wait()
	entries, err := os.ReadDir(filepath.Dir(s.Path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary preferences leaked", entries, err)
	}
}

func TestPreferencesCrossRootManualReloadAndDraftIsolation(t *testing.T) {
	a, f := ready(t)
	b := NewModel(t.Context(), &fakeLoader{}, "/another-team", i18n.English)
	store := testStore(t)
	a.preferencesStore = store
	b.preferencesStore = store
	a.reloadPreferences()
	b.reloadPreferences()
	calls := len(f.calls)
	id := a.selectedID()
	a.Update(key("ctrl+p"))
	if !a.preferencesOpen || a.popup {
		t.Fatal("Ctrl+P did not open preferences")
	}
	a.Update(key("right")) // auto -> en
	a.Update(key("down"))
	a.Update(key("right")) // terminal -> Latte
	if a.preferences.Theme != "terminal" {
		t.Fatal("draft changed theme before save")
	}
	a.Update(key("esc"))
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Esc wrote preferences")
	}
	a.Update(key("ctrl+p"))
	a.Update(key("down"))
	a.Update(key("right"))
	a.Update(key("enter"))
	if a.preferencesOpen || a.preferences.Theme != "catppuccin-latte" || len(f.calls) != calls || a.selectedID() != id {
		t.Fatal("save changed message state or did not apply")
	}
	if b.preferences.Theme != "terminal" {
		t.Fatal("other instance proactively changed")
	}
	if _, cmd := b.Update(key("ctrl+r")); cmd != nil || b.preferences.Theme != "catppuccin-latte" {
		t.Fatal("Ctrl+R failed config-only reload")
	}
	next := Preferences{Version: 1, Language: "zh-TW", Theme: "vesper"}
	if err := store.Save(next); err != nil {
		t.Fatal(err)
	}
	a.Update(key("ctrl+f"))
	a.Update(key("right"))
	draft := a.draft
	a.Update(key("ctrl+r"))
	if a.language != i18n.TraditionalChinese || a.preferences.Theme != "vesper" || a.draft != draft || !a.popup || len(f.calls) != calls {
		t.Fatal("config reload affected filter draft or HTTP")
	}
	a.Update(key("esc"))
	a.Update(key("ctrl+p"))
	a.Update(key("right"))
	a.Update(key("ctrl+r"))
	if a.preferencesDraft != next {
		t.Fatal("reload inside preferences did not discard unsaved draft")
	}
	a.Update(key("q"))
	if !a.preferencesOpen {
		t.Fatal("q escaped preferences")
	}
	a.Update(key("esc"))
}

func TestPreferencesReadAndSaveFailureRetainCurrentState(t *testing.T) {
	m, f := ready(t)
	s := testStore(t)
	m.preferencesStore = s
	good := Preferences{Version: 1, Language: "zh-TW", Theme: "dracula"}
	if err := s.Save(good); err != nil {
		t.Fatal(err)
	}
	m.reloadPreferences()
	if err := os.WriteFile(s.Path, []byte("bad\x1b[31m"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := len(f.calls)
	m.Update(key("ctrl+r"))
	if m.preferences != good || m.language != i18n.TraditionalChinese || m.configError == "" || strings.Contains(m.configError, "\x1b") {
		t.Fatal("bad reload applied or hid error")
	}
	m.Update(key("ctrl+p"))
	m.Update(key("right"))
	m.Update(key("enter"))
	if !m.preferencesOpen || m.preferences != good || len(f.calls) != calls {
		t.Fatal("failed save applied or lost draft")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "CFG") {
		t.Fatal("configuration error is hidden")
	}
}

func TestNamedThemesCanvasProfilesAndSizes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	m, _ := ready(t)
	for _, id := range themeIDs {
		for _, mono := range []bool{false, true} {
			m.noColor = mono
			m.applyPreferences(Preferences{Version: 1, Language: "auto", Theme: id})
			for _, size := range [][2]int{{20, 10}, {24, 12}, {40, 20}, {99, 16}, {100, 16}, {168, 43}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for _, dialog := range []int{0, 1, 2} {
					m.popup = dialog == 1
					m.preferencesOpen = dialog == 2
					m.preferencesDraft = m.preferences
					output := m.View().Content
					lines := strings.Split(ansi.Strip(output), "\n")
					if len(lines) != size[1] {
						t.Fatalf("%s height %v", id, size)
					}
					for _, line := range lines {
						if ansi.StringWidth(line) != size[0] {
							t.Fatalf("%s width %v: %q", id, size, line)
						}
					}
					parsed := uv.NewStyledString(output).Lines(ansi.GraphemeWidth)
					if id != "terminal" && !mono {
						for _, line := range parsed {
							for _, cell := range line {
								if cell.IsZero() {
									continue
								}
								if cell.Style.Fg == nil {
									t.Fatalf("%s missing primary foreground", id)
								}
								if id == "catppuccin-latte" && cell.Style.Bg == nil {
									t.Fatal("Latte has transparent cell")
								}
							}
						}
						if id != "catppuccin-latte" {
							// 本文中間必須仍然透明，面板／模式／選取則可有配對背景。
							row := parsed[len(parsed)-3]
							if row[len(row)-3].Style.Bg != nil {
								t.Fatal("dark theme painted general body background")
							}
						}
					}
					if mono && (strings.Contains(output, "38;") || strings.Contains(output, "48;")) {
						t.Fatal("NO_COLOR emitted palette")
					}
				}
			}
		}
	}
}

func TestPaletteSwitchDoesNotChangeTerminalDefaults(t *testing.T) {
	m, _ := ready(t)
	for _, id := range themeIDs {
		m.applyPreferences(Preferences{Version: 1, Language: "auto", Theme: id})
		v := m.View()
		if v.BackgroundColor != nil || v.ForegroundColor != nil || strings.Contains(v.Content, "\x1b]") {
			t.Fatal("theme changed terminal defaults", id)
		}
	}
	for _, height := range []int{1, 9} {
		m.applyPreferences(Preferences{Version: 1, Language: "auto", Theme: "catppuccin-latte"})
		m.Update(tea.WindowSizeMsg{Width: 19, Height: height})
		if n := len(strings.Split(ansi.Strip(m.View().Content), "\n")); n != height {
			t.Fatal(fmt.Sprint("Latte minimum view is not filled ", n))
		}
	}
}
