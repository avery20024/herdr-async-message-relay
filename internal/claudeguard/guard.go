// Package claudeguard 提供明確啟用的 Claude 問答投遞閘門；不讀寫正式佇列。
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
	Session   string          `json:"session_id"`
	Pane      string          `json:"pane_id"`
	Phase     string          `json:"phase"`
	Pending   map[string]bool `json:"pending"`
	UpdatedAt int64           `json:"updated_at"`
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
	f, err := os.OpenFile(filepath.Join(dir, "guard.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
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
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxRecord+1))
	if err != nil {
		return err
	}
	if len(b) > maxRecord {
		return errors.New("guard record too large")
	}
	return json.Unmarshal(b, v)
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

// Configure 只改指定角色政策；重複 enable 不清除任何既有阻擋狀態。
func (m Manager) Configure(ctx context.Context, role string, enabled bool) error {
	dir, err := m.directory(role)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return err
	}
	defer release()
	if enabled {
		var previous Policy
		err := readJSON(filepath.Join(dir, "policy.json"), &previous)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err != nil || !previous.Enabled {
			// 停用期間可能漏掉問答事件；重新啟用不能沿用舊 ready。
			if err := writeJSON(dir, "state.json", State{Phase: "unknown", UpdatedAt: time.Now().UnixMilli()}); err != nil {
				return err
			}
		}
	}
	return writeJSON(dir, "policy.json", Policy{Version: 1, Enabled: enabled})
}
func (m Manager) Inspect(ctx context.Context, role string) (Policy, State, error) {
	dir, err := m.directory(role)
	if err != nil {
		return Policy{}, State{}, err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return Policy{}, State{}, err
	}
	defer release()
	var p Policy
	var s State
	if err = readJSON(filepath.Join(dir, "policy.json"), &p); err != nil {
		return p, s, err
	}
	err = readJSON(filepath.Join(dir, "state.json"), &s)
	return p, s, err
}

func (m Manager) Apply(ctx context.Context, role, pane string, e Event) error {
	if !panePattern.MatchString(pane) || e.Session == "" || len(e.Session) > 256 {
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
	// 從未啟用的角色可共用 Hooks；已有目錄卻缺政策不在此放行。
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return err
	}
	defer release()
	var p Policy
	if err = readJSON(filepath.Join(dir, "policy.json"), &p); err != nil {
		return err
	}
	if !p.Enabled {
		return nil
	}
	var s State
	err = readJSON(filepath.Join(dir, "state.json"), &s)
	if e.Name == "SessionStart" {
		// 同一 session 的 resume／compact 不是問答結束證據，保留原有阻擋。
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err != nil || s.Session != e.Session || s.Pane != pane {
			s = State{Session: e.Session, Pane: pane, Phase: "ready", Pending: map[string]bool{}}
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
		// 子代理的 SessionEnd 不代表座位主會話結束。
		if e.AgentID != "" && e.Name == "SessionEnd" {
			return nil
		}
		switch e.Name {
		case "SessionEnd":
			s.Phase = "ended"
		case "PreToolUse":
			if len(s.Pending) >= 100 && !s.Pending[e.ToolID] {
				return errors.New("too many pending questions")
			}
			s.Pending[e.ToolID] = true
		case "PostToolUse":
			// 未觀察到開始的結束事件不可清除其他問題或 unknown 狀態。
			if !s.Pending[e.ToolID] {
				return errors.New("unmatched question completion")
			}
			delete(s.Pending, e.ToolID)
		case "PostToolUseFailure":
			s.Phase = "unknown"
		}
	}
	s.UpdatedAt = time.Now().UnixMilli()
	return writeJSON(dir, "state.json", s)
}

// Acquire 成功時，呼叫方須持鎖直到提交結果已記錄；不是 Claude 原生原子 API。
// 政策目錄不存在的角色保持原有行為；已有目錄卻缺失／損毀政策或狀態則拒絕放行。
func (m Manager) Acquire(ctx context.Context, role, pane, session string) (func(), bool, error) {
	dir, err := m.directory(role)
	if err != nil {
		return nil, false, err
	}
	if _, err = os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return func() {}, false, nil
	} else if err != nil {
		return nil, true, err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return nil, true, err
	}
	var p Policy
	if err = readJSON(filepath.Join(dir, "policy.json"), &p); err != nil {
		release()
		return nil, true, err
	}
	if !p.Enabled {
		return release, false, nil
	}
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
