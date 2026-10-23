package queueui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

type pageMsg struct {
	token  uint64
	append bool
	page   deliver.BrowseResponse
	err    error
}
type tickMsg uint64

type Model struct {
	ctx                          context.Context
	loader                       Loader
	language                     i18n.Language
	root                         string
	width, height                int
	rows                         []sqlc.BrowseMessagesRow
	senders, recipients          []string
	next                         int64
	selected, top, detailOffset  int
	metadataOffset               int
	pendingSelection, pendingTop int
	pendingFrom                  string
	filter, draft                Filter
	popup                        bool
	field                        int
	auto                         bool
	timer                        uint64
	token                        uint64
	busy, loaded                 bool
	cancel                       context.CancelFunc
	lastError                    string
	notice                       string
}

func NewModel(ctx context.Context, loader Loader, root string, language i18n.Language) *Model {
	return &Model{ctx: ctx, loader: loader, root: root, language: language}
}
func (m *Model) Init() tea.Cmd { return m.fetch(false) }

func (m *Model) selectedID() string {
	if len(m.rows) == 0 {
		return ""
	}
	return m.rows[m.selected].ID
}
func (m *Model) fetch(appendPage bool) tea.Cmd {
	if m.busy {
		return nil
	}
	m.token++
	token := m.token
	m.busy = true
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	m.cancel = cancel
	filter := m.filter
	before := int64(0)
	target := int64(0)
	if appendPage {
		before = m.next
	} else if len(m.rows) > 0 {
		target = m.rows[m.selected].Seq
	}
	loader := m.loader
	needed := max(1, m.pendingSelection-len(m.rows)+1)
	return func() tea.Msg {
		defer cancel()
		result, err := loader.Page(ctx, filter, before)
		// 刷新時以原選取 seq 為讀取深度，不能只取最新一頁後誤換訊息。
		for err == nil && result.NextBefore > 0 && ((appendPage && len(result.Items) < needed) || (!appendPage && target > 0 && result.NextBefore > target)) {
			page, e := loader.Page(ctx, filter, result.NextBefore)
			if e != nil {
				err = e
				break
			}
			if page.NextBefore != 0 && page.NextBefore >= result.NextBefore {
				err = fmt.Errorf("history cursor did not advance")
				break
			}
			result.Items = append(result.Items, page.Items...)
			result.NextBefore = page.NextBefore
		}
		return pageMsg{token: token, append: appendPage, page: result, err: err}
	}
}
func (m *Model) tick() tea.Cmd {
	generation := m.timer
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg(generation) })
}
func (m *Model) cancelFetch() {
	if m.cancel != nil {
		m.cancel()
	}
	m.token++
	m.busy = false
	m.pendingFrom = ""
}

// 半頁翻動同時移動視窗及選取，維持選取的螢幕位置；跨資料頁則延後到載入完成。
func (m *Model) moveSelection(delta int, halfPage bool) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	target := max(0, m.selected+delta)
	top := m.top
	if halfPage {
		top = max(0, top+delta)
	}
	if target >= len(m.rows) && m.next > 0 && !m.busy {
		m.pendingSelection, m.pendingTop, m.pendingFrom = target, top, m.selectedID()
		return m.fetch(true)
	}
	target = min(target, len(m.rows)-1)
	if target != m.selected {
		m.detailOffset, m.metadataOffset = 0, 0
		m.notice = ""
		m.pendingFrom = ""
	}
	m.selected, m.top = target, top
	return nil
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	defer m.clampLayout()
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if !m.auto || uint64(msg) != m.timer {
			return m, nil
		}
		return m, tea.Batch(m.fetch(false), m.tick())
	case pageMsg:
		if msg.token != m.token {
			return m, nil
		}
		m.busy = false
		if msg.err != nil {
			m.pendingFrom = ""
			m.lastError = clean(msg.err.Error())
			return m, nil
		}
		id := m.selectedID()
		oldLen := len(m.rows)
		if msg.append {
			m.rows = append(m.rows, msg.page.Items...)
			if len(m.rows) > oldLen && id == m.pendingFrom {
				m.selected = min(m.pendingSelection, len(m.rows)-1)
				m.top = m.pendingTop
				m.detailOffset, m.metadataOffset = 0, 0
				m.notice = ""
			}
			m.pendingFrom = ""
		} else {
			m.rows = msg.page.Items
			m.selected = 0
			found := false
			for i, row := range m.rows {
				if row.ID == id {
					m.selected = i
					found = true
					break
				}
			}
			if id != "" && !found {
				m.detailOffset, m.metadataOffset = 0, 0
				m.notice = m.text("Selection no longer matches filters; moved to first row.")
			}
		}
		m.next = msg.page.NextBefore
		m.senders = msg.page.Senders
		m.recipients = msg.page.Recipients
		m.loaded = true
		m.lastError = ""
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" || (!m.popup && key == "q") {
			m.cancelFetch()
			return m, tea.Quit
		}
		if m.popup {
			switch key {
			case "esc":
				m.popup = false
			case "up", "k":
				m.field = (m.field + 2) % 3
			case "down", "j":
				m.field = (m.field + 1) % 3
			case "left", "h":
				m.cycle(-1)
			case "right", "l":
				m.cycle(1)
			case "enter":
				m.popup = false
				if m.filter != m.draft {
					m.cancelFetch()
					m.filter = m.draft
					m.rows = nil
					m.next = 0
					m.selected = 0
					m.top = 0
					m.detailOffset, m.metadataOffset = 0, 0
					m.loaded = false
					m.notice = ""
					return m, m.fetch(false)
				}
			}
			return m, nil
		}
		switch key {
		case "ctrl+p":
			m.popup = true
			m.draft = m.filter
			m.field = 0
		case "ctrl+w":
			m.auto = !m.auto
			m.timer++
			if m.auto {
				return m, m.tick()
			}
		case "r":
			return m, m.fetch(false)
		case "up", "k":
			return m, m.moveSelection(-1, false)
		case "down", "j":
			return m, m.moveSelection(1, false)
		case "u":
			return m, m.moveSelection(-m.listHalfPage(), true)
		case "d":
			return m, m.moveSelection(m.listHalfPage(), true)
		case "[":
			m.metadataOffset = max(0, m.metadataOffset-m.detailPageHeight())
		case "]":
			m.metadataOffset = min(max(0, len(m.metadataLines())-m.detailPageHeight()), m.metadataOffset+m.detailPageHeight())
		case "U", "shift+u":
			m.detailOffset = max(0, m.detailOffset-m.detailPageHeight())
		case "D", "shift+d":
			m.detailOffset = min(max(0, len(m.detailLines())-m.detailPageHeight()), m.detailOffset+m.detailPageHeight())
		}
	}
	return m, nil
}

func choices(current string, values []string) []string {
	result := []string{""}
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	if current != "" {
		found := false
		for _, v := range result {
			if v == current {
				found = true
			}
		}
		if !found {
			result = append(result, current)
		}
	}
	return result
}
func (m *Model) cycle(direction int) {
	value := &m.draft.Sender
	values := m.senders
	switch m.field {
	case 1:
		value = &m.draft.Recipient
		values = m.recipients
	case 2:
		value = &m.draft.State
		values = []string{"queued", "dispatching", "submitted", "uncertain"}
	}
	list := choices(*value, values)
	index := 0
	for i, v := range list {
		if v == *value {
			index = i
			break
		}
	}
	*value = list[(index+direction+len(list))%len(list)]
}
