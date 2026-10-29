package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

// ID 是實例的短識別碼（根目錄與狀態目錄雜湊的前 12 碼），供 ps／stop／start／rm 使用。
func (m *Manager) ID(c deliver.Config) string { return key(c)[:12] }

func (m *Manager) statusOf(ctx context.Context, r Record) Status {
	ready, err := m.probe(ctx, r.Config)
	s := Status{ID: m.ID(r.Config), Root: r.Config.Root, StateDir: r.Config.StateDir, AutoStart: r.AutoStart, Managed: r.Managed, Ready: ready}
	if err != nil {
		s.Error = err.Error()
	}
	return s
}

// Instances 列出全部已登記實例；不依目前 Herdr 環境過濾，個別健康檢查失敗只記在該列。
func (m *Manager) Instances(ctx context.Context) ([]Status, error) {
	records, err := m.records()
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(records))
	for _, r := range records {
		out = append(out, m.statusOf(ctx, r))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Root < out[j].Root || out[i].Root == out[j].Root && out[i].ID < out[j].ID
	})
	return out, nil
}

func (m *Manager) find(id string) (Record, error) {
	if len(id) < 4 {
		return Record{}, i18n.NewError("instance id must be at least 4 characters; see hrelay ps")
	}
	records, err := m.records()
	if err != nil {
		return Record{}, err
	}
	var matches []Record
	for _, r := range records {
		if strings.HasPrefix(key(r.Config), strings.ToLower(id)) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return Record{}, i18n.Errorf("no relay instance matches %q; see hrelay ps", id)
	case 1:
		return matches[0], nil
	}
	return Record{}, i18n.Errorf("instance id %q is ambiguous; use more characters", id)
}

// sameRootOther 找出同一根目錄、不同狀態目錄的既有登記，避免同專案出現第二個佇列。
func (m *Manager) sameRootOther(c deliver.Config) (*Record, error) {
	records, err := m.records()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Config.Root == c.Root && r.Config.StateDir != c.StateDir {
			return &r, nil
		}
	}
	return nil, nil
}

func (m *Manager) StopByID(ctx context.Context, id string) (string, error) {
	r, err := m.find(id)
	if err != nil {
		return "", err
	}
	return m.ID(r.Config), m.Stop(ctx, r.Config)
}

func (m *Manager) StartByID(ctx context.Context, id string) (string, error) {
	r, err := m.find(id)
	if err != nil {
		return "", err
	}
	return m.ID(r.Config), m.Ensure(ctx, r.Config, true)
}

// RemoveByID 停止並移除登記與服務單元；佇列資料預設保留，purge 只刪佇列檔案本身。
func (m *Manager) RemoveByID(ctx context.Context, id string, purge bool) (string, error) {
	r, err := m.find(id)
	if err != nil {
		return "", err
	}
	c := r.Config
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	unlock, err := m.lock(ctx, c)
	if err != nil {
		return "", err
	}
	defer unlock()
	ready, err := m.probe(ctx, c)
	if err != nil {
		return "", err
	}
	if r.Managed {
		// 管理員明確指名實例時，不受目前 Herdr socket 環境限制。
		r.AutoStart = false
		if err = m.save(r); err != nil {
			return "", err
		}
		if err = m.stop(ctx, r); err != nil {
			return "", err
		}
	} else if ready {
		return "", i18n.NewError("existing relay is not managed by this plugin; no process was stopped")
	}
	if err = m.waitStopped(ctx, c); err != nil {
		return "", err
	}
	if r.Managed {
		path, _, renderErr := m.render(r)
		if renderErr != nil {
			return "", renderErr
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if m.Platform == "linux" {
			if _, err = m.run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
				return "", err
			}
		}
	}
	if purge {
		for _, name := range []string{"queue.db", "queue.db-wal", "queue.db-shm", "deliver.sock", "daemon.lock"} {
			if err = os.Remove(filepath.Join(c.StateDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		_ = os.Remove(c.StateDir) // 仍有其他檔案（例如問答攔截設定）時保留目錄
	}
	if dir := m.dir(c); filepath.Dir(dir) == m.Base {
		if err = os.RemoveAll(dir); err != nil {
			return "", err
		}
	}
	return m.ID(c), nil
}
