package queueui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestHelpPreservesUnderlyingScreenAndDrafts(t *testing.T) {
	for _, screen := range []string{"main", "filters", "preferences"} {
		t.Run(screen, func(t *testing.T) {
			m, f := ready(t)
			m.preferencesStore = testStore(t)
			m.selected = 4
			m.top = 2
			m.detailOffset = 2
			m.metadataOffset = 1
			m.rows[4].IdempotencyKey = strings.Repeat("long", 200)
			m.clampLayout()
			if screen == "filters" {
				m.Update(key("ctrl+f"))
				m.Update(key("right"))
				m.Update(key("j"))
			}
			if screen == "preferences" {
				m.Update(key("ctrl+p"))
				m.Update(key("right"))
				m.Update(key("j"))
			}
			id, draft, prefs, field, prefField := m.selectedID(), m.draft, m.preferencesDraft, m.field, m.preferenceField
			body, metadata, auto := m.detailOffset, m.metadataOffset, m.auto
			count := len(f.calls)
			m.Update(key("?"))
			if !m.helpOpen {
				t.Fatal("? did not open help")
			}
			for _, action := range []string{"j", "d", "G", "g", "ctrl+f", "ctrl+p", "ctrl+w", "r", "P", "enter", "q"} {
				_, cmd := m.Update(key(action))
				if cmd != nil {
					t.Fatalf("%s leaked a command through help", action)
				}
			}
			if m.selectedID() != id || m.detailOffset != body || m.metadataOffset != metadata || m.draft != draft || m.preferencesDraft != prefs || m.field != field || m.preferenceField != prefField || m.auto != auto || len(f.calls) != count {
				t.Fatal("help changed underlying state")
			}
			m.Update(key("esc"))
			if m.helpOpen || m.popup != (screen == "filters") || m.preferencesOpen != (screen == "preferences") {
				t.Fatal("Esc did not return to original screen")
			}
			m.Update(key("?"))
			m.Update(key("?"))
			if m.helpOpen {
				t.Fatal("? did not close help")
			}
		})
	}
	for _, press := range []tea.KeyPressMsg{{Code: '/', Mod: tea.ModShift}, {Code: '?', Mod: tea.ModShift}} {
		m, _ := ready(t)
		m.Update(press)
		if !m.helpOpen {
			t.Fatal("shift question alias did not open help", press.String())
		}
	}
}

func TestHelpScrollReachabilityResizeAndGlobalKeys(t *testing.T) {
	m, f := ready(t)
	m.preferencesStore = testStore(t)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	m.Update(key("?"))
	var visible strings.Builder
	seenRows := map[string]bool{}
	for i := 0; i < len(m.helpDocument()); i++ {
		content := m.helpContent()
		for _, line := range content[2 : len(content)-1] {
			seenRows[ansi.Strip(line)] = true
		}
		visible.WriteString(ansi.Strip(strings.Join(content, "\n")))
		m.Update(key("j"))
	}
	visibleText := strings.Join(strings.Fields(visible.String()), " ")
	for _, entry := range shortcutEntries {
		if !strings.Contains(visibleText, entry[0]) {
			t.Fatal("unreachable key", entry[0])
		}
	}
	// 逐列核描述可達；小視窗描述可能跨頁，不能要求整句一次出現在同一幀。
	for _, line := range m.helpDocument() {
		if !seenRows[ansi.Strip(line)] {
			t.Fatal("unreachable document row", ansi.Strip(line))
		}
	}
	if m.helpOffset != len(m.helpDocument())-m.helpPageHeight() {
		t.Fatal("end clamp")
	}
	m.Update(key("home"))
	if m.helpOffset != 0 {
		t.Fatal("home")
	}
	for _, keys := range [][2]string{{"down", "up"}, {"d", "u"}, {"pgdown", "pgup"}, {"G", "g"}, {"end", "home"}} {
		m.Update(key(keys[0]))
		if m.helpOffset == 0 {
			t.Fatal("no scroll", keys[0])
		}
		m.Update(key(keys[1]))
		if m.helpOffset != 0 {
			t.Fatal("no return", keys[1])
		}
	}
	m.Update(key("G"))
	m.Update(tea.WindowSizeMsg{Width: 180, Height: 60})
	if m.helpOffset != 0 {
		t.Fatal("resize did not clamp short document")
	}
	count := len(f.calls)
	m.Update(key("ctrl+r"))
	if len(f.calls) != count || !m.helpOpen || m.configError != "" {
		t.Fatal("help reload issued HTTP or closed help")
	}
	m.auto = true
	m.timer = 8
	_, cmd := m.Update(tickMsg(8))
	if cmd == nil || !m.busy || !m.helpOpen {
		t.Fatal("help paused background auto")
	}
	m.cancelFetch()
	_, cmd = m.Update(key("ctrl+c"))
	if cmd == nil {
		t.Fatal("Ctrl+C blocked by help")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not Quit")
	}
	m, _ = ready(t)
	m.Update(key("?"))
	m.Update(tea.WindowSizeMsg{Width: 19, Height: 9})
	_, cmd = m.Update(key("q"))
	if cmd == nil {
		t.Fatal("minimum notice trapped help")
	}
}

// StyledString.Lines回傳grapheme，不是終端欄位；補寬字佔用格才可比xy座標。
func helpScreenCells(text string) []uv.Line {
	lines := uv.NewStyledString(text).Lines(ansi.GraphemeWidth)
	for y, line := range lines {
		var cells uv.Line
		for _, c := range line {
			cells = append(cells, c)
			for i := 1; i < c.Width; i++ {
				cells = append(cells, uv.Cell{})
			}
		}
		lines[y] = cells
	}
	return lines
}

func TestHelpPanelHasOneBackground(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, themeID := range themeIDs {
			for _, mono := range []bool{false, true} {
				for _, size := range [][2]int{{20, 10}, {40, 20}, {106, 35}, {168, 43}} {
					t.Run(fmt.Sprint(lang, themeID, mono, size), func(t *testing.T) {
						m, _ := ready(t)
						m.baseLanguage = lang
						m.noColor = mono
						m.applyPreferences(Preferences{Version: 1, Language: "auto", Theme: themeID})
						m.width = size[0]
						m.height = size[1]
						m.clampLayout()
						before := helpScreenCells(m.View().Content)
						m.Update(key("?"))
						for _, position := range []string{"g", "G"} {
							m.Update(key(position))
							panel := m.helpPanel()
							if mono && (strings.Contains(panel, "38;") || strings.Contains(panel, "48;")) {
								t.Fatal("NO_COLOR panel contains color")
							}
							after := helpScreenCells(m.View().Content)
							left := 1 + (m.tableWidth()-m.helpOuterWidth())/2
							top := 1 + (m.contentHeight()-m.helpOuterHeight())/2
							fgColors := map[[4]uint32]bool{}
							bold := false
							for y := top; y < top+m.helpOuterHeight(); y++ {
								for x := left; x < left+m.helpOuterWidth(); x++ {
									c := after[y][x]
									if c.IsZero() {
										continue
									}
									if mono {
										if c.Style.Bg != nil {
											t.Fatal("NO_COLOR background")
										}
									} else {
										bg := m.styles.dialog.GetBackground()
										if c.Style.Bg == nil {
											t.Fatalf("transparent dialog cell %d,%d", x, y)
										}
										r, g, b, a := c.Style.Bg.RGBA()
										wr, wg, wb, wa := bg.RGBA()
										if [4]uint32{r, g, b, a} != [4]uint32{wr, wg, wb, wa} {
											t.Fatalf("patch background %d,%d", x, y)
										}
									}
									if c.Style.Fg != nil {
										r, g, b, a := c.Style.Fg.RGBA()
										fgColors[[4]uint32{r, g, b, a}] = true
									}
									// 標題與鍵名粗體不能被統一底色移除。
									bold = bold || c.Style.Attrs != 0
								}
							}
							if !bold {
								t.Fatal("text attributes lost")
							}
							if !mono && len(fgColors) < 2 {
								t.Fatal("foreground roles lost")
							}
							for y := 0; y < len(before)-1; y++ {
								for x := range before[y] {
									// 寬字跨邊界時compositor可能清掉相鄰半格，非底色外溢。
									if y >= top && y < top+m.helpOuterHeight() && x >= left-1 && x <= left+m.helpOuterWidth() {
										continue
									}
									if !reflect.DeepEqual(before[y][x], after[y][x]) {
										t.Fatalf("changed underlying cell outside modal %d,%d", x, y)
									}
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestHelpTitleNormalColorAndGap(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, id := range themeIDs {
			for _, mono := range []bool{false, true} {
				m, _ := ready(t)
				m.baseLanguage = lang
				m.noColor = mono
				m.applyPreferences(Preferences{Version: 1, Language: "auto", Theme: id})
				m.width = 120
				m.height = 40
				m.Update(key("?"))
				lines := m.helpContent()
				if strings.TrimSpace(ansi.Strip(lines[0])) != m.text("Keyboard shortcuts") {
					t.Fatal("title changed")
				}
				if strings.TrimSpace(ansi.Strip(lines[1])) != "" {
					t.Fatal("missing blank row below title")
				}
				for _, c := range uv.NewStyledString(lines[0]).Lines(ansi.GraphemeWidth)[0] {
					if strings.TrimSpace(c.Content) == "" {
						continue
					}
					if c.Style.Attrs == 0 {
						t.Fatal("title lost bold")
					}
					if !mono {
						want := m.styles.dialog.GetForeground()
						if c.Style.Fg == nil || want == nil {
							t.Fatal("missing theme normal title foreground", id)
						}
						r, g, b, a := c.Style.Fg.RGBA()
						wr, wg, wb, wa := want.RGBA()
						if [4]uint32{r, g, b, a} != [4]uint32{wr, wg, wb, wa} {
							t.Fatal("title does not use theme normal foreground", id)
						}
					} else if c.Style.Fg != nil {
						t.Fatal("NO_COLOR title has foreground")
					}
				}
				if m.helpOuterWidth() != 62 || m.helpOuterHeight() != 27 || len(lines) != 25 {
					t.Fatal("title gap changed fixed dimensions")
				}
				if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) != m.text("ESC close") {
					t.Fatal("exit hint no longer at bottom")
				}
			}
		}
	}
}

func TestHelpCenteredOverlayPrototype(t *testing.T) {
	for _, size := range [][2]int{{168, 43}, {120, 40}, {68, 32}, {100, 30}, {40, 20}, {20, 10}} {
		m, _ := ready(t)
		m.width = size[0]
		m.height = size[1]
		m.clampLayout()
		m.Update(key("?"))
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		xWant := 1 + (m.tableWidth()-m.helpOuterWidth())/2
		yWant := 1 + (m.contentHeight()-m.helpOuterHeight())/2
		if !strings.Contains(lines[yWant], "╭") {
			t.Fatal("missing centered top border", size, yWant, lines[yWant])
		}
		x := ansi.StringWidth(strings.SplitN(lines[yWant], "╭", 2)[0])
		if x != xWant {
			t.Fatal("not centered", size, x, xWant)
		}
		segment := strings.SplitN(lines[yWant], "╭", 2)[1]
		end := strings.Index(segment, "╮")
		if end < 0 || ansi.StringWidth(segment[:end])+2 != m.helpOuterWidth() {
			t.Fatal("modal outer width", size)
		}
		if size[0] >= 68 && m.helpOuterWidth() != 62 {
			t.Fatal("dialog must cap at 62 columns")
		}
		if size[1] >= 32 && m.helpOuterHeight() != 27 {
			t.Fatal("dialog must keep 27 rows even with short content")
		}
		content := m.helpContent()
		if strings.TrimSpace(ansi.Strip(content[len(content)-1])) != m.text("ESC close") {
			t.Fatal("hint must be immediately above bottom border")
		}
		if size[1] >= 32 {
			if len(content) != 25 || len(m.helpDocument()) >= m.helpPageHeight() {
				t.Fatal("missing fixed-height blank viewport")
			}
			if strings.TrimSpace(ansi.Strip(content[len(content)-2])) != "" {
				t.Fatal("short content must leave blank space above bottom hint")
			}
		}
		if size[0] >= 120 && !strings.Contains(strings.Join(lines[:yWant], "\n"), "Sender") {
			t.Fatal("underlying main screen was replaced")
		}
		if len(m.helpContent())+2 != m.helpOuterHeight() {
			t.Fatal("panel height", size)
		}
	}
}

func TestHelpAllThemesLanguagesAndSizes(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.TraditionalChinese} {
		for _, themeID := range themeIDs {
			for _, mono := range []bool{false, true} {
				for _, size := range [][2]int{{20, 10}, {24, 12}, {39, 16}, {40, 20}, {67, 22}, {99, 30}, {100, 30}, {168, 43}} {
					t.Run(fmt.Sprint(lang, themeID, mono, size), func(t *testing.T) {
						m, _ := ready(t)
						m.baseLanguage = lang
						m.noColor = mono
						m.applyPreferences(Preferences{Version: 1, Language: "auto", Theme: themeID})
						m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
						m.Update(key("?"))
						for _, position := range []string{"g", "G"} {
							m.Update(key(position))
							view := m.View().Content
							plain := ansi.Strip(view)
							lines := strings.Split(plain, "\n")
							if len(lines) != size[1] {
								t.Fatal("height", len(lines))
							}
							for _, line := range lines {
								if ansi.StringWidth(line) != size[0] {
									t.Fatal("width", line)
								}
							}
							content := m.helpContent()
							if got := strings.TrimSpace(ansi.Strip(content[len(content)-1])); got != m.text("ESC close") {
								t.Fatal("close hint must be only ESC", got)
							}
							if strings.TrimSpace(ansi.Strip(m.footerLine())) != "" {
								t.Fatal("duplicate help footer")
							}
							if mono && strings.Contains(view, "38;") {
								t.Fatal("NO_COLOR foreground")
							}
							if themeID == "catppuccin-latte" && !mono {
								for y, line := range uv.NewStyledString(view).Lines(ansi.GraphemeWidth) {
									for x, c := range line {
										if !c.IsZero() && c.Style.Bg == nil {
											t.Fatalf("Latte hole %d,%d", x, y)
										}
									}
								}
							}
						}
					})
				}
			}
		}
	}
	m, _ := ready(t)
	m.Update(key("?"))
	doc := ansi.Strip(strings.Join(m.helpDocument(), "\n"))
	if len(shortcutEntries) != 14 || len(m.helpDocument()) != 21 {
		t.Fatal("grouped list must have fourteen shortcuts, four headings and three separators")
	}
	seen := map[string]bool{}
	for _, entry := range shortcutEntries {
		if seen[entry[0]] || !strings.Contains(doc, entry[0]) {
			t.Fatal("duplicate or missing key", entry[0])
		}
		seen[entry[0]] = true
	}
	for _, old := range []string{"Navigation", "Main screen", "Filter dialog", "Preferences dialog", "Global", "Help screen", "Choose field", "Scroll help"} {
		if strings.Contains(doc, old) {
			t.Fatal("redundant group/action", old)
		}
	}
	m.language = i18n.TraditionalChinese
	for _, entry := range shortcutEntries {
		if m.text(entry[1]) == entry[1] {
			t.Fatal("untranslated description", entry[1])
		}
	}
}
