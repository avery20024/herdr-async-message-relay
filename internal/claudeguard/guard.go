// Package claudeguard 提供團隊共用的 Claude 問答投遞閘門與持續觀測。
package claudeguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

var rolePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var panePattern = regexp.MustCompile(`^w[0-9A-Za-z]+:p[0-9A-Za-z]+$`)

const maxRecord = 64 * 1024

type Manager struct{ Dir string }
type State struct {
	Session   string           `json:"session_id"`
	Pane      string           `json:"pane_id"`
	Phase     string           `json:"phase"`
	Pending   map[string]bool  `json:"pending"`
	UpdatedAt int64            `json:"updated_at"`
	Started   map[string]int64 `json:"started_at,omitempty"`
	Recent    []Question       `json:"recent_questions,omitempty"`
}

// Question 只保存生命週期證據，不含問題或答案本文；未觀測到結束時不填耗時。
type Question struct {
	Session    string `json:"session_id"`
	Pane       string `json:"pane_id"`
	ToolID     string `json:"tool_use_id"`
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Outcome    string `json:"outcome"`
}
type Policy struct {
	Version int  `json:"version"`
	Enabled bool `json:"enabled"`
}

// 政策只接受明確且唯一的必要欄位，避免缺漏或誤植被解讀為停用。
func (p *Policy) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	start, err := d.Token()
	if err != nil {
		return err
	}
	if start != json.Delim('{') {
		return errors.New("guard policy must be an object")
	}
	var version *int
	var enabled *bool
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return errors.New("invalid or duplicate guard policy field")
		}
		seen[key] = true
		switch key {
		case "version":
			err = d.Decode(&version)
		case "enabled":
			err = d.Decode(&enabled)
		default:
			return fmt.Errorf("unknown guard policy field: %s", key)
		}
		if err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if version == nil || enabled == nil {
		return errors.New("guard policy requires version and enabled")
	}
	if *version != 1 {
		return errors.New("unsupported guard policy version")
	}
	*p = Policy{Version: *version, Enabled: *enabled}
	return nil
}

type Event struct {
	Name    string `json:"hook_event_name"`
	Session string `json:"session_id"`
	Tool    string `json:"tool_name"`
	ToolID  string `json:"tool_use_id"`
	AgentID string `json:"agent_id"`
}

func Decode(r io.Reader) (Event, error) {
	var e Event
	data, err := io.ReadAll(io.LimitReader(r, 1024*1024+1))
	if err != nil {
		return e, err
	}
	if len(data) > 1024*1024 {
		return e, errors.New("hook input exceeds 1 MiB")
	}
	err = json.Unmarshal(data, &e)
	return e, err
}
func (m Manager) directory(role string) (string, error) {
	if !rolePattern.MatchString(role) {
		return "", errors.New("invalid guard role")
	}
	return filepath.Join(m.Dir, role), nil
}

// 同一角色的同步 hook 與提交持有同一把鎖；等待有界，不使用 shell 背景程序。
func lock(ctx context.Context, dir string) (func(), error) {
	return fileLock(ctx, dir, "guard.lock", false)
}
func fileLock(ctx context.Context, dir, name string, shared bool) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("guard lock must be a regular file")
	}
	mode := syscall.LOCK_EX
	if shared {
		mode = syscall.LOCK_SH
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func readJSON(path string, v any) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("guard record must be a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxRecord+1))
	if err != nil {
		return err
	}
	if len(b) > maxRecord {
		return errors.New("guard record too large")
	}
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	if s, ok := v.(*State); ok {
		return s.validate()
	}
	return nil
}
func writeJSON(dir, name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > maxRecord {
		return errors.New("guard record too large")
	}
	f, err := os.CreateTemp(dir, ".guard-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Configure 保留舊呼叫介面，但政策已改為團隊共用，不再重設持續觀測狀態。
func (m Manager) Configure(ctx context.Context, role string, enabled bool) error {
	if _, err := m.directory(role); err != nil {
		return err
	}
	return m.ConfigureTeam(ctx, enabled)
}
func (m Manager) Inspect(ctx context.Context, role string) (Policy, State, error) {
	dir, err := m.directory(role)
	if err != nil {
		return Policy{}, State{}, err
	}
	p, err := m.TeamPolicy()
	if err != nil {
		return p, State{}, err
	}
	if err := checkDir(dir); err != nil {
		return p, State{}, err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return p, State{}, err
	}
	defer release()
	var s State
	err = readJSON(filepath.Join(dir, "state.json"), &s)
	return p, s, err
}

func (m Manager) Apply(ctx context.Context, role, pane string, e Event) error {
	if !panePattern.MatchString(pane) || e.Session == "" || len(e.Session) > 256 || len(e.AgentID) > 256 {
		return errors.New("hook requires valid pane and session")
	}
	if e.AgentID != "" && (e.Name == "SessionStart" || e.Name == "SessionEnd") {
		return nil
	}
	switch e.Name {
	case "SessionStart", "SessionEnd":
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		if e.Tool != "AskUserQuestion" || e.ToolID == "" || len(e.ToolID) > 256 {
			return errors.New("expected AskUserQuestion tool_use_id")
		}
	default:
		return errors.New("unsupported Claude hook event")
	}
	dir, err := m.directory(role)
	if err != nil {
		return err
	}
	// 接線的角色一律記錄，團隊攔截開關不影響事件觀測。
	if err := prepareDir(m.Dir); err != nil {
		return err
	}
	if err := prepareDir(dir); err != nil {
		return err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return err
	}
	defer release()
	var s State
	err = readJSON(filepath.Join(dir, "state.json"), &s)
	if e.Name == "SessionStart" {
		// 同一 session 的 resume／compact 不是問答結束證據，保留原有阻擋。
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err != nil || s.Session != e.Session || s.Pane != pane {
			for id := range s.Pending {
				s.finishQuestion(id, "interrupted", false)
			}
			s = State{Session: e.Session, Pane: pane, Phase: "ready", Pending: map[string]bool{}, Started: map[string]int64{}, Recent: s.Recent}
		}
	} else {
		if err != nil {
			return err
		}
		if s.Session != e.Session || s.Pane != pane {
			return errors.New("stale hook session or pane")
		}
		if s.Pending == nil {
			s.Pending = map[string]bool{}
		}
		if s.Started == nil {
			s.Started = map[string]int64{}
		}
		// 子代理的 SessionEnd 不代表座位主會話結束。
		if e.AgentID != "" && e.Name == "SessionEnd" {
			return nil
		}
		switch e.Name {
		case "SessionEnd":
			for id := range s.Pending {
				s.finishQuestion(id, "interrupted", false)
			}
			s.Phase = "ended"
		case "PreToolUse":
			if len(s.Pending) >= 100 && !s.Pending[e.ToolID] {
				return errors.New("too many pending questions")
			}
			if !s.Pending[e.ToolID] {
				s.Started[e.ToolID] = time.Now().UnixMilli()
			}
			s.Pending[e.ToolID] = true
		case "PostToolUse":
			// 未觀察到開始的結束事件不可清除其他問題或 unknown 狀態。
			if !s.Pending[e.ToolID] {
				return errors.New("unmatched question completion")
			}
			s.finishQuestion(e.ToolID, "completed", true)
			delete(s.Pending, e.ToolID)
			delete(s.Started, e.ToolID)
		case "PostToolUseFailure":
			if !s.Pending[e.ToolID] {
				return errors.New("unmatched question failure")
			}
			s.finishQuestion(e.ToolID, "failed", true)
			s.Phase = "unknown"
		}
	}
	s.UpdatedAt = time.Now().UnixMilli()
	if err := appendEvent(dir, e, pane, s); err != nil {
		return err
	}
	return writeJSON(dir, "state.json", s)
}

// Acquire 成功時，呼叫方須持鎖直到提交結果已記錄；不是 Claude 原生原子 API。
// 團隊預設攔截；未接線或未初始化的 Claude 不宣稱已受保護且不放行。
func (m Manager) Acquire(ctx context.Context, role, pane, session string) (func(), bool, error) {
	dir, err := m.directory(role)
	if err != nil {
		return nil, false, err
	}
	teamRelease, err := m.teamLock(ctx, true)
	if err != nil {
		return nil, true, err
	}
	p, err := m.TeamPolicy()
	if err != nil {
		teamRelease()
		return nil, true, err
	}
	if !p.Enabled {
		return teamRelease, false, nil
	}
	if err := checkDir(dir); err != nil {
		teamRelease()
		return nil, true, fmt.Errorf("Claude hooks not initialized: %w", err)
	}
	roleRelease, err := lock(ctx, dir)
	if err != nil {
		teamRelease()
		return nil, true, err
	}
	release := func() { roleRelease(); teamRelease() }
	var s State
	if err = readJSON(filepath.Join(dir, "state.json"), &s); err != nil {
		release()
		return nil, true, err
	}
	if session == "" || s.Session != session || s.Pane != pane || s.Phase != "ready" || len(s.Pending) != 0 {
		release()
		return nil, true, fmt.Errorf("Claude guard deferred: phase=%s pending=%d; session and pane must match", s.Phase, len(s.Pending))
	}
	return release, true, nil
}
