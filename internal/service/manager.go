// Package service 透過作業系統的使用者服務管理器啟動 relay，不自行背景 fork。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

type Record struct {
	Schema      int               `json:"schema"`
	Config      deliver.Config    `json:"config"`
	Environment map[string]string `json:"environment"`
	AutoStart   bool              `json:"auto_start"`
	Managed     bool              `json:"managed"`
	Binary      string            `json:"binary,omitempty"`
}

type Status struct {
	ID        string `json:"id,omitempty"`
	Error     string `json:"error,omitempty"`
	Root      string `json:"project_root"`
	StateDir  string `json:"state_dir"`
	AutoStart bool   `json:"auto_start"`
	Managed   bool   `json:"managed"`
	Ready     bool   `json:"ready"`
}

type Manager struct {
	Base, Home, Platform, Executable string
	UID                              int
	Environment                      []string
	run                              func(context.Context, string, ...string) ([]byte, error)
	probe                            func(context.Context, deliver.Config) (bool, error)
	wait                             time.Duration
}

func New() (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := os.Getenv("HERDR_RELAY_SERVICE_HOME")
	if base == "" {
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			state = filepath.Join(home, ".local", "state")
		}
		base = filepath.Join(state, "herdr-async-message-relay", "services")
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Manager{Base: base, Home: home, Platform: runtime.GOOS, Executable: executable,
		UID: os.Getuid(), Environment: os.Environ(), run: run, probe: probe, wait: 8 * time.Second}, nil
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, i18n.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func probe(ctx context.Context, c deliver.Config) (bool, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.Socket())
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/v1/status", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		// 只有確定沒有 listener 才允許啟動；逾時／權限錯誤不代表可以接管。
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return false, nil
		}
		return false, i18n.Errorf("cannot safely determine relay health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, i18n.Errorf("existing relay returned HTTP %d; refusing takeover", resp.StatusCode)
	}
	var body struct {
		Root string `json:"project_root"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 2*1024*1024)).Decode(&body); err != nil {
		return false, i18n.Errorf("invalid existing relay response: %w", err)
	}
	if body.Root != c.Root {
		return false, i18n.NewError("existing relay belongs to a different project; refusing takeover")
	}
	return true, nil
}

func key(c deliver.Config) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(c.Root+"\x00"+c.StateDir)))[:24]
}
func (m *Manager) dir(c deliver.Config) string { return filepath.Join(m.Base, key(c)) }
func (m *Manager) recordPath(c deliver.Config) string {
	return filepath.Join(m.dir(c), "service.json")
}

func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return i18n.Errorf("service directory must not be a symlink: %s", path)
	}
	return os.Chmod(path, 0700)
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".hrelay-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (m *Manager) lock(ctx context.Context, c deliver.Config) (func(), error) {
	if len(c.Socket()) > 100 {
		return nil, i18n.NewError("Unix socket path too long; set a shorter DELIVER_STATE_DIR")
	}
	if err := privateDir(m.Base); err != nil {
		return nil, err
	}
	if err := privateDir(m.dir(c)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(m.dir(c), "service.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (m *Manager) load(c deliver.Config) (Record, error) {
	var r Record
	body, err := os.ReadFile(m.recordPath(c))
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(body, &r); err != nil {
		return r, err
	}
	if err := validateRecord(r); err != nil {
		return r, err
	}
	if r.Config.Root != c.Root || r.Config.StateDir != c.StateDir {
		return r, i18n.NewError("service record has an incompatible schema or project binding")
	}
	return r, nil
}
func validateRecord(r Record) error {
	if r.Schema != 1 || !filepath.IsAbs(r.Config.Root) || !filepath.IsAbs(r.Config.StateDir) ||
		!filepath.IsAbs(r.Config.Herdr) || !filepath.IsAbs(r.Environment["HERDR_SOCKET_PATH"]) ||
		r.Config.BatchSize < 1 || r.Config.BatchSize > 100 || r.Config.BatchBytes < 2048 || r.Config.BatchBytes > 64000 ||
		r.Config.Poll < 50*time.Millisecond || r.Config.Poll > time.Minute {
		return i18n.NewError("invalid service schema, settings or Herdr context binding")
	}
	return nil
}

func (m *Manager) save(r Record) error {
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(m.recordPath(r.Config), append(body, '\n'), 0600)
}

func (m *Manager) newRecord(c deliver.Config) (Record, error) {
	env := make(map[string]string)
	for _, item := range m.Environment {
		name, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		switch name {
		case "PATH", "HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "HERDR_SOCKET_PATH", "HERDR_CONFIG_PATH":
			env[name] = value
		}
	}
	if !filepath.IsAbs(env["HERDR_SOCKET_PATH"]) {
		return Record{}, i18n.NewError("first service setup requires an absolute HERDR_SOCKET_PATH; run from a Herdr pane or set its socket explicitly")
	}
	env["HOME"] = m.Home
	herdr := c.Herdr
	if !filepath.IsAbs(herdr) {
		var err error
		herdr, err = exec.LookPath(herdr)
		if err != nil {
			return Record{}, i18n.Errorf("cannot locate Herdr for supervised service: %w", err)
		}
	}
	herdr, err := filepath.Abs(herdr)
	if err != nil {
		return Record{}, err
	}
	c.Herdr = herdr
	env["HERDR_BIN_PATH"] = herdr
	env["DELIVER_STATE_DIR"] = c.StateDir
	env["DELIVER_BATCH_SIZE"] = strconv.Itoa(c.BatchSize)
	env["DELIVER_BATCH_BYTES"] = strconv.Itoa(c.BatchBytes)
	env["DELIVER_POLL_MS"] = strconv.FormatInt(c.Poll.Milliseconds(), 10)
	return Record{Schema: 1, Config: c, Environment: env, AutoStart: true}, nil
}

func (m *Manager) checkContext(r Record) error {
	for _, item := range m.Environment {
		if name, value, ok := strings.Cut(item, "="); ok && name == "HERDR_SOCKET_PATH" && value != "" {
			if stored := r.Environment[name]; value != stored {
				return i18n.NewError("project is registered to a different Herdr socket; refusing takeover")
			}
		}
	}
	return nil
}

// Ensure 不會中止或更新健康的 daemon；resume 只供明確的 service start 使用。
func (m *Manager) Ensure(ctx context.Context, c deliver.Config, resume bool) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if info, err := os.Stat(c.Root); err != nil || !info.IsDir() {
		return i18n.Errorf("registered project root must still be a directory: %s", c.Root)
	}
	unlock, err := m.lock(ctx, c)
	if err != nil {
		return err
	}
	defer unlock()
	return m.ensureLocked(ctx, c, resume)
}

func (m *Manager) ensureLocked(ctx context.Context, c deliver.Config, resume bool) error {
	if info, err := os.Stat(c.Root); err != nil || !info.IsDir() {
		return i18n.Errorf("registered project root must still be a directory: %s", c.Root)
	}
	r, err := m.load(c)
	if errors.Is(err, os.ErrNotExist) {
		other, e := m.sameRootOther(c)
		if e != nil {
			return e
		}
		if other != nil {
			return i18n.Errorf("this root is already registered with state directory %s (id %s); use it, or run hrelay rm %s first", other.Config.StateDir, m.ID(other.Config), m.ID(other.Config))
		}
		r, err = m.newRecord(c)
	}
	if err != nil {
		return err
	}
	if err = m.checkContext(r); err != nil {
		return err
	}
	if !r.AutoStart && !resume {
		return i18n.NewError("relay service is paused; explicitly run service start to resume")
	}
	ready, err := m.probe(ctx, c)
	if err != nil {
		return err
	}
	r.AutoStart = true
	if ready {
		// 明確恢復已管理服務時，修復先前停止失敗可能留下的登入停用狀態；不重啟程序。
		if resume && r.Managed {
			if err := m.enable(ctx, r); err != nil {
				return err
			}
		}
		// 舊版／前景 daemon 只登記供日後恢復，不搶停，也不寫入服務管理器。
		return m.save(r)
	}
	if resume {
		// 明確 stop/start 才套用新的 worker 設定；一般 send／restore 不改動既有設定。
		r.Config.BatchSize, r.Config.BatchBytes, r.Config.Poll = c.BatchSize, c.BatchBytes, c.Poll
		r.Environment["DELIVER_BATCH_SIZE"] = strconv.Itoa(c.BatchSize)
		r.Environment["DELIVER_BATCH_BYTES"] = strconv.Itoa(c.BatchBytes)
		r.Environment["DELIVER_POLL_MS"] = strconv.FormatInt(c.Poll.Milliseconds(), 10)
	}
	if m.Platform != "darwin" && m.Platform != "linux" {
		return i18n.NewError("automatic service management supports macOS launchd or Linux systemd --user only")
	}
	binary, err := os.ReadFile(m.Executable)
	if err != nil {
		return err
	}
	binaryPath := filepath.Join(m.dir(c), fmt.Sprintf("hrelay-%x", sha256.Sum256(binary)))
	if err = atomicWrite(binaryPath, binary, 0700); err != nil {
		return err
	}
	r.Binary, r.Managed = binaryPath, true
	if err = m.save(r); err != nil {
		return err
	}
	if err = m.start(ctx, r); err != nil {
		return i18n.Errorf("cannot start supervised relay: %w", err)
	}
	deadline := time.NewTimer(m.wait)
	defer deadline.Stop()
	for {
		ready, err = m.probe(ctx, r.Config)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return i18n.NewError("service did not become ready; inspect launchd log or systemd user journal, no message was enqueued")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (m *Manager) Stop(ctx context.Context, c deliver.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	unlock, err := m.lock(ctx, c)
	if err != nil {
		return err
	}
	defer unlock()
	if err := m.stopLocked(ctx, c); err != nil {
		return err
	}
	return m.waitStopped(ctx, c)
}

func (m *Manager) stopLocked(ctx context.Context, c deliver.Config) error {
	r, err := m.load(c)
	if err != nil {
		return err
	}
	if err = m.checkContext(r); err != nil {
		return err
	}
	if !r.Managed {
		return i18n.NewError("existing relay is not managed by this plugin; no process was stopped")
	}
	r.AutoStart = false
	if err = m.save(r); err != nil {
		return err
	}
	return m.stop(ctx, r)
}

func (m *Manager) Status(ctx context.Context, c deliver.Config) (Status, error) {
	r, err := m.load(c)
	if err != nil {
		return Status{}, err
	}
	if err = m.checkContext(r); err != nil {
		return Status{}, err
	}
	ready, err := m.probe(ctx, c)
	return Status{ID: m.ID(c), Root: c.Root, StateDir: c.StateDir, AutoStart: r.AutoStart, Managed: r.Managed, Ready: ready}, err
}

func (m *Manager) records() ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(m.Base, "*", "service.json"))
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r Record
		if err = json.Unmarshal(body, &r); err != nil {
			return nil, i18n.Errorf("invalid registry %s: %w", path, err)
		}
		if validateRecord(r) != nil || path != m.recordPath(r.Config) {
			return nil, i18n.Errorf("invalid project binding in %s", path)
		}
		records = append(records, r)
	}
	return records, nil
}

func (m *Manager) Restore(ctx context.Context) error {
	records, err := m.records()
	if err != nil {
		return err
	}
	var failures []error
	for _, r := range records {
		if !r.AutoStart || m.checkContext(r) != nil {
			continue
		}
		if err := m.Ensure(ctx, r.Config, false); err != nil {
			failures = append(failures, i18n.Errorf("%s: %w", r.Config.Root, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) List(ctx context.Context) ([]Status, error) {
	records, err := m.records()
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(records))
	for _, r := range records {
		if m.checkContext(r) != nil {
			continue
		}
		status, err := m.Status(ctx, r.Config)
		if err != nil {
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}
