package deliver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

const BackendEnv = "HRELAY_BACKEND"

func (c Config) IsCore() bool { return c.Backend == "core" }
func (c Config) BackendName() string {
	if c.IsCore() {
		return "core"
	}
	return "herdr"
}

func DetectBackend(getenv func(string) string) (Config, error) {
	var c Config
	err := c.loadBackend(getenv)
	return c, err
}

// loadBackend 只在呼叫／啟動時判斷；受管理 worker 以保存的明確後端恢復。
func (c *Config) loadBackend(getenv func(string) string) error {
	choice := getenv(BackendEnv)
	if choice == "" {
		choice = "auto"
	}
	id, terminalSocket, socket := getenv("HC_TERMINAL_ID"), getenv("HC_TERMINAL_SOCKET"), getenv("HC_SOCKET")
	if choice == "auto" {
		choice = "herdr"
		if id != "" || terminalSocket != "" || socket != "" {
			if !coreAddressPattern.MatchString(id) || !filepath.IsAbs(terminalSocket) {
				return i18n.NewError("incomplete core terminal context; require HC_TERMINAL_ID and absolute HC_TERMINAL_SOCKET")
			}
			choice = "core"
		}
	}
	if choice == "herdr" {
		return nil
	}
	if choice != "core" {
		return i18n.NewError("HRELAY_BACKEND must be auto, herdr or core")
	}
	if id != "" || terminalSocket != "" {
		if !coreAddressPattern.MatchString(id) || !filepath.IsAbs(terminalSocket) {
			return i18n.NewError("incomplete core terminal context")
		}
		if socket != "" && filepath.Clean(socket) != filepath.Clean(terminalSocket) {
			return i18n.NewError("HC_SOCKET differs from HC_TERMINAL_SOCKET; refusing cross-core routing")
		}
		socket = terminalSocket
	}
	if !filepath.IsAbs(socket) {
		return i18n.NewError("core backend requires an absolute HC_SOCKET or complete core terminal context")
	}
	c.Backend, c.CoreSocket = "core", filepath.Clean(socket)
	c.Core = getenv("HC_BIN_PATH")
	if c.Core == "" {
		c.Core = "hc"
	}
	return nil
}

type stateBinding struct {
	Schema  int    `json:"schema"`
	Root    string `json:"project_root"`
	Backend string `json:"backend"`
	Socket  string `json:"core_socket"`
}

func (c Config) binding() stateBinding { return stateBinding{1, c.Root, c.BackendName(), c.CoreSocket} }

// CheckStateBinding 是唯讀防線：明訂 state 也不能將舊 DB 當作 core 或換 socket 接管。
func (c Config) CheckStateBinding() error {
	path := filepath.Join(c.StateDir, "backend.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if c.IsCore() {
			if _, dbErr := os.Lstat(filepath.Join(c.StateDir, "queue.db")); !errors.Is(dbErr, os.ErrNotExist) {
				return i18n.NewError("core state is unbound but contains an existing database; use a separate state directory")
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return i18n.NewError("backend binding must be a regular file")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var bound stateBinding
	if err := json.Unmarshal(body, &bound); err != nil {
		return i18n.Errorf("invalid backend binding: %w", err)
	}
	if bound != c.binding() {
		return i18n.NewError("state directory is bound to a different backend, core socket or project")
	}
	return nil
}

// bindState 由持有 daemon.lock 的 Serve 呼叫，先落盤綁定再開 SQLite。
func (c Config) bindState() error {
	if err := c.CheckStateBinding(); err != nil {
		return err
	}
	if !c.IsCore() {
		return nil
	}
	path := filepath.Join(c.StateDir, "backend.json")
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	body, err := json.Marshal(c.binding())
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(body, '\n'))
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (c Config) DeliveryBackend() Herdr {
	if c.IsCore() {
		return CLICore{Binary: c.Core, Socket: c.CoreSocket, Root: c.Root, Timeout: c.CommandTimeout}
	}
	return CLIHerdr{Binary: c.Herdr, Timeout: c.CommandTimeout}
}

// ValidateStatusBinding 相容舊 Herdr 缺 backend 的狀態，但 core 必須明確綁定。
func (c Config) ValidateStatusBinding(body []byte) error {
	var status struct {
		Root    string `json:"project_root"`
		Backend string `json:"backend"`
		Socket  string `json:"core_socket"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return err
	}
	if status.Backend == "" {
		status.Backend = "herdr"
	}
	if status.Root != c.Root || status.Backend != c.BackendName() || (c.IsCore() && status.Socket != c.CoreSocket) {
		return i18n.NewError("existing relay has a different project, backend or core socket; refusing takeover")
	}
	return nil
}
