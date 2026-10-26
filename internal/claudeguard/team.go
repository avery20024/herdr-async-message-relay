package claudeguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func prepareDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return checkDir(dir)
}
func checkDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("guard directory must not be a symlink")
	}
	return nil
}

// TeamPolicy 缺少設定時預設開啟；損毀不是停用。舊每角色政策不再控制投遞。
func (m Manager) TeamPolicy() (Policy, error) {
	p := Policy{Version: 1, Enabled: true}
	err := readJSON(filepath.Join(m.Dir, "team-policy.json"), &p)
	if errors.Is(err, os.ErrNotExist) {
		return Policy{Version: 1, Enabled: true}, nil
	}
	return p, err
}
func (m Manager) teamLock(ctx context.Context, shared bool) (func(), error) {
	if err := prepareDir(m.Dir); err != nil {
		return nil, err
	}
	return fileLock(ctx, m.Dir, "team.lock", shared)
}
func (m Manager) ConfigureTeam(ctx context.Context, enabled bool) error {
	release, err := m.teamLock(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	// 不覆蓋損毀的既有政策，避免把讀取錯誤當作明確選擇。
	if _, err := m.TeamPolicy(); err != nil {
		return err
	}
	return writeJSON(m.Dir, "team-policy.json", Policy{Version: 1, Enabled: enabled})
}

func (s *State) finishQuestion(id, outcome string, ended bool) {
	q := Question{Session: s.Session, Pane: s.Pane, ToolID: id, StartedAt: s.Started[id], Outcome: outcome}
	if ended {
		q.EndedAt = time.Now().UnixMilli()
		if q.StartedAt > 0 && q.EndedAt >= q.StartedAt {
			q.DurationMS = q.EndedAt - q.StartedAt
		}
	}
	s.Recent = append(s.Recent, q)
	// 有界近期歷史；不是無限成長的審計日誌。
	if len(s.Recent) > 20 {
		s.Recent = s.Recent[len(s.Recent)-20:]
	}
}

type Observation struct {
	Role         string `json:"role"`
	Status       string `json:"status"`
	UpdatedAt    int64  `json:"updated_at,omitempty"`
	PendingCount int    `json:"pending_count"`
}

// Observation 不建立角色狀態；已觀測只代表收到事件，不是 Hooks 的心跳保證。
func (m Manager) Observation(ctx context.Context, role string) Observation {
	o := Observation{Role: role, Status: "uninitialized"}
	dir, err := m.directory(role)
	if err != nil {
		o.Status = "error"
		return o
	}
	if err := checkDir(dir); errors.Is(err, os.ErrNotExist) {
		return o
	} else if err != nil {
		o.Status = "error"
		return o
	}
	release, err := lock(ctx, dir)
	if err != nil {
		o.Status = "error"
		return o
	}
	defer release()
	var s State
	if err := readJSON(filepath.Join(dir, "state.json"), &s); err != nil {
		o.Status = "error"
		return o
	}
	o.UpdatedAt = s.UpdatedAt
	o.PendingCount = len(s.Pending)
	switch {
	case s.Session == "" || s.Pane == "":
		o.Status = "uninitialized"
	case s.Phase == "unknown":
		o.Status = "unknown"
	case s.Phase == "ended":
		o.Status = "ended"
	case s.Phase != "ready":
		o.Status = "error"
	case len(s.Pending) > 0:
		o.Status = "pending"
	default:
		o.Status = "observed"
	}
	return o
}
