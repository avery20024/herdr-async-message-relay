package queueui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
	"github.com/charmbracelet/x/ansi"
)

type Retrier interface {
	Retry(context.Context, string, string) error
}
type retryMsg struct{ err error }

func (c *Client) Retry(ctx context.Context, messageID, batchID string) error {
	body, err := json.Marshal(map[string]any{"project_root": c.root, "message_id": messageID, "action": "retry", "confirm": true, "note": "UI operator inspected message " + messageID + " and explicitly confirmed retry of its uncertain batch; duplicate delivery is possible"})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/batches/"+url.PathEscape(batchID)+"/resolve", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil {
		return err
	}
	if len(data) > 4096 {
		return errors.New("retry response too large")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("retry HTTP %d: %s", resp.StatusCode, clean(string(data)))
	}
	var result struct {
		Resolved bool `json:"resolved"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	if !result.Resolved {
		return errors.New("retry was not confirmed")
	}
	return nil
}

func (m *Model) blockFor(row sqlc.BrowseMessagesRow) *deliver.QueueBlock {
	if row.State != "queued" {
		return nil
	}
	for i := range m.queueBlocks {
		if m.queueBlocks[i].Recipient == row.Recipient {
			return &m.queueBlocks[i]
		}
	}
	return nil
}
func (m *Model) rowState(row sqlc.BrowseMessagesRow) string {
	s := stateLabel(row.State)
	if m.blockFor(row) != nil {
		s += " !"
	}
	return s
}
func (m *Model) openRetry() {
	if m.retryBusy || m.retryUnconfirmed {
		return
	}
	if !m.retrySupported {
		m.notice = m.text("Worker does not support safe UI retry; upgrade separately.")
		return
	}
	if _, ok := m.loader.(Retrier); !ok {
		m.notice = m.text("Worker does not support safe UI retry; upgrade separately.")
		return
	}
	if len(m.rows) == 0 {
		return
	}
	row := m.rows[m.selected]
	if row.State != "uncertain" || !row.BatchID.Valid || row.BatchState.String != "uncertain" {
		m.notice = m.text("Select an uncertain message to retry.")
		return
	}
	m.retryTarget = row
	m.retryOffset = 0
	m.retryOpen = true
}
func (m *Model) retryKey(key string) tea.Cmd {
	if m.retryBusy {
		return nil
	}
	switch key {
	case "esc":
		m.retryOpen = false
	case "j", "down":
		m.retryOffset++
	case "k", "up":
		m.retryOffset = max(0, m.retryOffset-1)
	case "u", "pgup":
		m.retryOffset = max(0, m.retryOffset-3)
	case "d", "pgdown":
		m.retryOffset += 3
	case "y":
		retrier, ok := m.loader.(Retrier)
		if !ok {
			return nil
		}
		target := m.retryTarget
		m.cancelFetch()
		m.retryBusy = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
			defer cancel()
			return retryMsg{err: retrier.Retry(ctx, target.ID, target.BatchID.String)}
		}
	}
	m.retryOffset = min(m.retryOffset, max(0, len(m.retryDocument())-m.retryPageHeight()))
	return nil
}
func (m *Model) retryWidth() int { return max(1, min(68, m.tableWidth())-2) }
func (m *Model) retryHints() []string {
	hint := m.text("y retry / Esc cancel / j,k scroll")
	if m.retryBusy {
		hint = m.text("Retry pending; do not repeat.")
	}
	return strings.Split(ansi.Hardwrap(hint, m.retryWidth(), true), "\n")
}
func (m *Model) retryPageHeight() int {
	return max(1, min(m.contentHeight()-2-len(m.retryHints()), 14))
}
func (m *Model) retryDocument() []string {
	lines := []string{m.text("Retry uncertain batch?"), "", m.text("May already be delivered. Retrying can duplicate delivery."), m.text("All uncertain messages in this batch will be requeued, not sent directly."), "", m.text("Recipient") + ": " + clean(m.retryTarget.Recipient), m.text("ID") + ": " + clean(m.retryTarget.ID), m.text("Batch") + ": " + clean(m.retryTarget.BatchID.String), m.text("Batch evidence") + ": " + clean(m.retryTarget.BatchDetail.String)}
	return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), m.retryWidth(), true), "\n")
}
func (m *Model) retryOverlay(content []string) []string {
	width := m.retryWidth()
	wrapped := m.retryDocument()
	height := m.retryPageHeight()
	offset := min(m.retryOffset, max(0, len(wrapped)-height))
	popup := []string{}
	for _, line := range wrapped[offset:min(len(wrapped), offset+height)] {
		popup = append(popup, cell(line, width))
	}
	for _, line := range m.retryHints() {
		popup = append(popup, m.styles.accent.Render(cell(line, width)))
	}
	// 先扣除固定操作提示高度，避免最小畫面截掉永遠捲不到的警示。
	panel := m.styles.dialog.Border(lipgloss.RoundedBorder()).BorderForeground(m.styles.border.GetForeground()).Render(strings.Join(popup, "\n"))
	overlay := lipgloss.NewLayer(panel).X(max(0, (m.tableWidth()-width-2)/2)).Y(max(0, (len(content)-len(popup)-2)/2)).Z(1)
	result := strings.Split(lipgloss.NewCompositor(lipgloss.NewLayer(strings.Join(content, "\n")), overlay).Render(), "\n")
	for i := range result {
		result[i] = trustedCell(result[i], m.tableWidth())
	}
	return result
}
