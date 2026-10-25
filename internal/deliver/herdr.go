package deliver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"strings"
)

type AgentSession struct {
	Agent string `json:"agent"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Agent struct {
	Session       AgentSession `json:"agent_session"`
	CWD           string       `json:"cwd"`
	ForegroundCWD string       `json:"foreground_cwd"`
	Pane          string       `json:"pane_id"`
	Status        string       `json:"agent_status"`
	Seq           *int64       `json:"state_change_seq"`
}

type Herdr interface {
	List(context.Context) ([]Agent, error)
	Prompt(context.Context, string, string) error
}

type CLIHerdr struct {
	Binary  string
	Timeout time.Duration
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 1024*1024 {
		keep := min(n, 1024*1024-b.Len())
		_, _ = b.Buffer.Write(p[:keep])
	}
	return n, nil
}

func (h CLIHerdr) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.Binary, args...)
	cmd.WaitDelay = time.Second
	var out, stderr limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("herdr command failed: %w: %s", err, stderr.String())
	}
	if out.Len() >= 1024*1024 {
		return nil, errors.New("herdr output too large")
	}
	return out.Bytes(), nil
}

func (h CLIHerdr) List(ctx context.Context) ([]Agent, error) {
	data, err := h.run(ctx, "agent", "list")
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Agents []Agent `json:"agents"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, errors.New("herdr list returned error")
	}
	if envelope.Result.Agents == nil {
		return nil, errors.New("invalid herdr agent list")
	}
	return envelope.Result.Agents, nil
}

func (h CLIHerdr) Prompt(ctx context.Context, pane, body string) error {
	data, err := h.run(ctx, "agent", "prompt", pane, body, "--wait", "--until", "working", "--until", "blocked", "--until", "idle", "--until", "done", "--timeout", "7000")
	if err != nil {
		return err
	}
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("invalid submission response: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return fmt.Errorf("submission refused: %s", envelope.Error)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return errors.New("submission response missing result")
	}
	return nil
}

var panePattern = regexp.MustCompile(`^w[0-9A-Za-z]+:p[0-9A-Za-z]+$`)

// canonicalPath 解析符號連結；路徑不存在時退回清理後的字串，避免比較時混用邏輯與實體路徑。
func canonicalPath(p string) string {
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	// 末端可能還不存在（例如剛建立的子目錄）；解析最近的既有祖先後再接回剩餘片段。
	parts := []string{}
	for dir := filepath.Clean(p); ; {
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Clean(p)
		}
		parts = append([]string{filepath.Base(dir)}, parts...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(append([]string{resolved}, parts...)...)
		}
	}
}

func uniquePaths(paths ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range paths {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// cwdBelongsToRole 判斷 cwd 是否落在角色容器底下的本角色；容器本身或其他角色都不算。
func cwdBelongsToRole(cwd string, rolesDirs []string, role string) bool {
	for _, rolesDir := range rolesDirs {
		rel, err := filepath.Rel(rolesDir, cwd)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rel == "." {
			return false
		}
		return strings.SplitN(rel, string(filepath.Separator), 2)[0] == role
	}
	return true
}

// seatRank 由 herdr agent list 的 cwd／foreground_cwd 判斷座位歸屬：
// cwd 完全相符（3）> cwd 在角色目錄下（2）> foreground_cwd 完全相符（1）。
// cwd 已落在其他角色目錄時一律排除，避免以前景程序認領別人的訊息。
func seatRank(a Agent, role string, rolesDirs, bases []string) int {
	cwd := canonicalPath(a.CWD)
	foreground := canonicalPath(a.ForegroundCWD)
	if !cwdBelongsToRole(cwd, rolesDirs, role) {
		return 0
	}
	rank := 0
	for _, base := range bases {
		switch {
		case cwd == base:
			rank = max(rank, 3)
		case strings.HasPrefix(cwd, base+string(filepath.Separator)):
			rank = max(rank, 2)
		}
	}
	for _, base := range bases {
		if foreground == base {
			rank = max(rank, 1)
		}
	}
	return rank
}

// ResolveAgent 以 herdr agent list 解析角色座位；較精確的比對優先，仍要求唯一。
func ResolveAgent(root, role string, agents []Agent) (Agent, error) {
	roleDir := filepath.Join(canonicalPath(root), "角色", role)
	bases := uniquePaths(roleDir, canonicalPath(roleDir))
	rolesDir := filepath.Dir(roleDir)
	rolesDirs := uniquePaths(rolesDir, canonicalPath(rolesDir))
	best := 0
	matches := []Agent{}
	for _, a := range agents {
		rank := seatRank(a, role, rolesDirs, bases)
		if rank == 0 {
			continue
		}
		if rank > best {
			best = rank
			matches = matches[:0]
		}
		if rank == best {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		return Agent{}, fmt.Errorf("role %s has no seat in herdr agent list", role)
	}
	if len(matches) > 1 {
		return Agent{}, fmt.Errorf("role %s matched %d seats", role, len(matches))
	}
	a := matches[0]
	if !panePattern.MatchString(a.Pane) || a.Seq == nil || *a.Seq < 0 {
		return Agent{}, errors.New("invalid pane or missing state_change_seq")
	}
	return a, nil
}
