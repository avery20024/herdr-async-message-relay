package deliver

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
	"github.com/avery20024/herdr-async-message-relay/internal/roles"
)

// SenderContext 只由發送端建立；daemon 的 cwd 不能用來辨識發信席位。
type SenderContext struct {
	CWD         string
	Explicit    string
	Environment string
	Legacy      string
	Pane        string
	Tab         string
	InsideHerdr bool
}

func (s SenderContext) validate() error {
	for _, hint := range []struct{ source, value string }{
		{"--from", s.Explicit}, {"HERDR_RELAY_FROM", s.Environment}, {"HERDR_SEND_FROM", s.Legacy},
	} {
		if hint.value != "" && !roles.ValidName(hint.value) {
			return i18n.Errorf("invalid sender hint %s=%q", hint.source, hint.value)
		}
	}
	return nil
}

func (s SenderContext) check(role string) (string, error) {
	for _, hint := range []struct{ source, value string }{
		{"--from", s.Explicit}, {"HERDR_RELAY_FROM", s.Environment}, {"HERDR_SEND_FROM", s.Legacy},
	} {
		if hint.value != "" && hint.value != role {
			return "", i18n.Errorf("sender identity conflict: caller pwd %q resolves to %q but %s=%q; no message was enqueued", s.CWD, role, hint.source, hint.value)
		}
	}
	return role, nil
}

// IdentifySender 在 Ensure 與 POST 之前，以 caller 目錄及唯讀席位查詢驗證身分。
// 非 Herdr 的顯式人工發送保留離線入口；環境值本身永遠不構成離線證據。
func IdentifySender(ctx context.Context, c Config, caller SenderContext, herdr Herdr) (string, error) {
	if err := caller.validate(); err != nil {
		return "", err
	}
	cwd, err := physicalDirectory(caller.CWD)
	if err != nil {
		return "", i18n.Errorf("cannot resolve sender pwd: %w", err)
	}
	caller.CWD = cwd
	agents, err := herdr.List(ctx)
	if err != nil {
		if ctx.Err() == nil && !caller.inHerdr() && caller.Explicit != "" {
			return caller.check(caller.Explicit)
		}
		return "", i18n.Errorf("cannot verify sender identity from pwd %q: %w; no message was enqueued", cwd, err)
	}
	return ResolveSender(c.Root, caller, agents)
}

func (s SenderContext) inHerdr() bool { return s.InsideHerdr || s.Pane != "" || s.Tab != "" }

func physicalDirectory(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", i18n.Errorf("not a directory: %s", path)
	}
	return path, nil
}

func directoryContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ResolveSender 沿用收件端的通用角色規則，不假設語言目錄或以任意 pwd basename 猜角色。
func ResolveSender(root string, caller SenderContext, agents []Agent) (string, error) {
	if err := caller.validate(); err != nil {
		return "", err
	}
	cwd, err := physicalDirectory(caller.CWD)
	if err != nil {
		return "", i18n.Errorf("cannot resolve sender pwd: %w", err)
	}
	caller.CWD = cwd
	bound := -1
	if caller.Pane != "" || caller.Tab != "" {
		matches := []int{}
		for i, a := range agents {
			if (caller.Pane != "" && a.Pane == caller.Pane) || (caller.Pane == "" && a.TabID == caller.Tab) {
				matches = append(matches, i)
			}
		}
		if len(matches) != 1 {
			return "", i18n.Errorf("sender seat must be unique: pane=%q tab=%q matched %d agents", caller.Pane, caller.Tab, len(matches))
		}
		bound = matches[0]
		if caller.Tab != "" && agents[bound].TabID != caller.Tab {
			return "", i18n.Errorf("sender pane/tab conflict: pane=%q tab=%q", caller.Pane, caller.Tab)
		}
		if _, err := RoleFromAgent(root, agents[bound]); err != nil {
			return "", i18n.Errorf("sender seat is outside relay root or invalid: %w", err)
		}
	}

	// 取最接近 pwd 的已觀測前景目錄，避免父目錄席位覆蓋較精確的角色。
	candidates := []int{}
	depth := -1
	for i, a := range agents {
		if _, err := RoleFromAgent(root, a); err != nil {
			continue
		}
		dir, err := physicalDirectory(a.ForegroundCWD)
		if err != nil || !directoryContains(dir, cwd) {
			continue
		}
		if len(dir) > depth {
			depth, candidates = len(dir), nil
		}
		if len(dir) == depth {
			candidates = append(candidates, i)
		}
	}
	selected := bound
	if len(candidates) > 0 {
		if bound >= 0 {
			found := false
			for _, i := range candidates {
				found = found || i == bound
			}
			if !found {
				return "", i18n.Errorf("sender pwd/seat conflict: pwd=%q pane=%q tab=%q; no message was enqueued", cwd, caller.Pane, caller.Tab)
			}
		} else if len(candidates) == 1 {
			selected = candidates[0]
		} else {
			return "", i18n.Errorf("sender pwd %q is shared by %d agents; a unique pane/tab binding is required", cwd, len(candidates))
		}
	}
	if selected < 0 {
		if !caller.inHerdr() && caller.Explicit != "" {
			return caller.check(caller.Explicit)
		}
		return "", i18n.Errorf("cannot verify sender from pwd %q: no matching live directory or seat; environment alone is not identity", cwd)
	}
	role, err := RoleFromAgent(root, agents[selected])
	if err != nil {
		return "", err
	}
	resolved, err := ResolveAgent(root, role, agents)
	if err != nil {
		return "", i18n.Errorf("sender address is ambiguous or invalid: %w", err)
	}
	if resolved.Pane != agents[selected].Pane {
		return "", i18n.NewError("sender seat changed during identity resolution")
	}
	return caller.check(role)
}
