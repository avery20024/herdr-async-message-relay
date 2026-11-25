package deliver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var coreAddressPattern = regexp.MustCompile(`^t[0-9a-z]+$`)

type CLICore struct {
	Binary, Socket, Root string
	Timeout              time.Duration
}

type coreTerminal struct {
	Address    string `json:"address"`
	Name       string `json:"name"`
	State      string `json:"state"`
	CWD        string `json:"cwd"`
	Foreground *struct {
		CWD string `json:"cwd"`
		PID int64  `json:"pid"`
	} `json:"foreground"`
	Status *struct {
		App   string `json:"app"`
		State string `json:"state"`
	} `json:"status"`
	Turn    *int64 `json:"turn"`
	Created int64  `json:"created_ms"`
	PID     int64  `json:"pid"`
}

func (h CLICore) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	// hc 的一般查詢可能自行啟動 core；已停止／不可達時先拒絕，不把 relay 當 core 啟動器。
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", h.Socket)
	if err != nil {
		return nil, fmt.Errorf("core socket unavailable: %w", err)
	}
	_ = connection.Close()
	cmd := exec.CommandContext(ctx, h.Binary, args...)
	cmd.WaitDelay = time.Second
	// 不讓發送席位 ID／其他 session 干擾常駐 worker 的 caller 與 socket。
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HC_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HC_SOCKET="+h.Socket)
	var out, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hc command failed: %w: %s", err, stderr.String())
	}
	if out.Len() >= 1024*1024 {
		return nil, errors.New("hc output too large")
	}
	return out.Bytes(), nil
}

func (h CLICore) get(ctx context.Context, address string) (Agent, error) {
	if !coreAddressPattern.MatchString(address) {
		return Agent{}, errors.New("invalid local core terminal address")
	}
	body, err := h.run(ctx, "get", "--json", "--", address)
	if err != nil {
		return Agent{}, err
	}
	var t coreTerminal
	if err := json.Unmarshal(body, &t); err != nil {
		return Agent{}, err
	}
	if t.Address != address {
		return Agent{}, errors.New("hc get address mismatch")
	}
	a := Agent{Name: t.Name, CWD: t.CWD, Pane: address, Backend: "core", Seq: t.Turn}
	if t.State != "live" {
		return a, nil
	}
	if t.Foreground != nil {
		a.ForegroundCWD = t.Foreground.CWD
	} else {
		// 前景行程暫時無法取得時（如 Claude Code 重啟過渡期），退回終端機自身 CWD。
		// RoleFromAgent 仍會核對此路徑在 root 內，不放寬安全邊界。
		a.ForegroundCWD = t.CWD
	}
	if t.Status != nil && t.Status.App != "" && t.Foreground != nil && t.Foreground.PID > 0 && t.Created > 0 && t.PID > 0 {
		a.Status = t.Status.State
		a.Session = AgentSession{Agent: t.Status.App, Kind: "core-process", Value: fmt.Sprintf("%d:%d:%d", t.Created, t.PID, t.Foreground.PID)}
	}
	return a, nil
}

func (h CLICore) List(ctx context.Context) ([]Agent, error) {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	body, err := h.run(ctx, "ls", "--json")
	if err != nil {
		return nil, err
	}
	var list struct {
		Terminals []struct {
			Address string `json:"address"`
			State   string `json:"state"`
		} `json:"terminals"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	if list.Terminals == nil {
		return nil, errors.New("hc ls missing terminals")
	}
	out := []Agent{}
	seen := map[string]bool{}
	for _, t := range list.Terminals {
		// 遠端機器可在該機器執行 relay；此處不把 machine/tN 混入本機路由。
		if strings.Contains(t.Address, "/") {
			continue
		}
		if !coreAddressPattern.MatchString(t.Address) || seen[t.Address] {
			return nil, errors.New("hc ls invalid or duplicate local address")
		}
		seen[t.Address] = true
		if t.State != "live" {
			continue
		}
		a, err := h.get(ctx, t.Address)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// Prompt 僅供介面相容；worker 使用帶角色與世代快照的 PromptAgent。
func (h CLICore) Prompt(context.Context, string, string) error {
	return errors.New("core submission requires a role and agent snapshot")
}

// SendDirect 直接送出訊息，不做 turn/session 狀態驗證；用於 uncertain 訊息的離線重送。
// 收件端可能已收到原批次，呼叫方須自行揭露重送警示。
func (h CLICore) SendDirect(ctx context.Context, pane, body string) error {
	if !coreAddressPattern.MatchString(pane) {
		return errors.New("invalid core terminal address for direct send")
	}
	_, err := h.run(ctx, "send", "--json", "--", pane, body)
	return err
}

func (h CLICore) PromptAgent(ctx context.Context, role string, baseline Agent, body string) error {
	current, err := h.get(ctx, baseline.Pane)
	if err != nil {
		return err
	}
	name, roleErr := RoleFromAgent(h.Root, current)
	if roleErr != nil || name != role || current.Session != baseline.Session || current.Seq == nil || baseline.Seq == nil || *current.Seq != *baseline.Seq || (current.Status != "idle" && current.Status != "done") {
		return errors.New("core recipient identity or readiness changed before submission")
	}
	args := []string{"send", "--json"}
	if current.Status == "idle" {
		args = append(args, "--if", "idle")
	}
	args = append(args, "--", current.Pane, body)
	data, err := h.run(ctx, args...)
	if err != nil {
		return err
	}
	// hc 0.1.21 的 turn/status 是貼入前快照，不是送出後回合；turn 與 baseline 吻合表示
	// 貼入了對的座位與回合。done 模式下貼入後目標可能立即進入 working，允許此競態；
	// idle 模式（使用 --if idle）下前快照必須仍是 idle，其他組合保守交人工核對。
	var response struct {
		Turn    *int64          `json:"turn"`
		Status  string          `json:"status"`
		Started *bool           `json:"started"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("invalid hc submission response: %w", err)
	}
	validStatus := response.Status == current.Status || (current.Status == "done" && response.Status == "working")
	if response.Turn == nil || *response.Turn != *baseline.Seq || !validStatus || response.Started == nil || (len(response.Error) > 0 && string(response.Error) != "null") {
		return errors.New("hc submission response missing confirmation")
	}
	return nil
}
