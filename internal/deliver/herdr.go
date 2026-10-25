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

// seatRank 由 herdr agent list 的 cwd／foreground_cwd 判斷座位歸屬，數字越大越精確。
func seatRank(a Agent, logical, physical string) int {
	paths := []string{a.CWD}
	if a.ForegroundCWD != "" {
		paths = append(paths, a.ForegroundCWD)
	}
	rank := 0
	for _, path := range paths {
		if path == "" {
			continue
		}
		for _, base := range []string{logical, physical} {
			if base == "" {
				continue
			}
			if path == base {
				rank = max(rank, 3)
			} else if strings.HasPrefix(path, base+string(filepath.Separator)) {
				rank = max(rank, 2)
			}
		}
	}
	return rank
}

// ResolveAgent 以 herdr agent list 解析角色座位；完全相符優先，其次子目錄與 foreground cwd，仍要求唯一 pane。
func ResolveAgent(root, role string, agents []Agent) (Agent, error) {
	logical := filepath.Join(root, "角色", role)
	physical, err := filepath.EvalSymlinks(logical)
	if err != nil {
		physical = ""
	}
	best := 0
	matches := []Agent{}
	for _, a := range agents {
		rank := seatRank(a, logical, physical)
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
