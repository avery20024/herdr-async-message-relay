package claudeguard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// HistoryEvent 是已驗證且已觀測的 Hook 事件，不保證後續 state.json 已完成更新。
// 兩檔無跨檔交易；崩潰後須核對事件與狀態，不依日誌自動解除攔截。
type HistoryEvent struct {
	At       int64     `json:"at"`
	Name     string    `json:"event"`
	Session  string    `json:"session_id"`
	Pane     string    `json:"pane_id"`
	ToolID   string    `json:"tool_use_id,omitempty"`
	AgentID  string    `json:"agent_id,omitempty"`
	Phase    string    `json:"phase"`
	Question *Question `json:"question,omitempty"`
}

func appendEvent(dir string, e Event, pane string, s State) error {
	record := HistoryEvent{At: s.UpdatedAt, Name: e.Name, Session: e.Session, Pane: pane, ToolID: e.ToolID, AgentID: e.AgentID, Phase: s.Phase}
	if e.Name == "PreToolUse" {
		record.Question = &Question{Session: e.Session, Pane: pane, ToolID: e.ToolID, StartedAt: s.Started[e.ToolID], Outcome: "pending"}
	} else if (e.Name == "PostToolUse" || e.Name == "PostToolUseFailure") && len(s.Recent) > 0 {
		q := s.Recent[len(s.Recent)-1]
		record.Question = &q
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("guard history must be a regular file")
	}
	n, err := f.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return errors.New("short guard history write")
	}
	return f.Sync()
}
