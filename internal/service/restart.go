package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

// Restart 將停用、關閉確認及恢復放在同一生命週期鎖，避免發送端搶先喚醒。
func (m *Manager) Restart(ctx context.Context, c deliver.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	unlock, err := m.lock(ctx, c)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := m.load(c)
	if errors.Is(err, os.ErrNotExist) || err == nil && !r.Managed {
		ready, probeErr := m.probe(ctx, c)
		if probeErr != nil {
			return probeErr
		}
		if ready {
			return errors.New("existing relay is not managed by this plugin; no process was restarted")
		}
		return m.ensureLocked(ctx, c, true)
	}
	if err != nil {
		return err
	}
	if err := m.stopLocked(ctx, c); err != nil {
		return err
	}
	if err := m.waitStopped(ctx, c); err != nil {
		return err
	}
	return m.ensureLocked(ctx, c, true)
}

// listener 消失不足以證明 worker／SQLite 已關閉，還須確認舊 daemon 的檔案鎖釋放。
func daemonLockFree(c deliver.Config) (bool, error) {
	lock, err := os.OpenFile(filepath.Join(c.StateDir, "daemon.lock"), os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("daemon lock must be a regular file")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return false, nil
		}
		return false, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return true, nil
}

func (m *Manager) waitStopped(ctx context.Context, c deliver.Config) error {
	deadline := time.NewTimer(m.wait)
	defer deadline.Stop()
	for {
		ready, probeErr := m.probe(ctx, c)
		if !ready && probeErr == nil {
			free, err := daemonLockFree(c)
			if err != nil {
				return err
			}
			if free {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("relay shutdown not confirmed; service remains paused, no replacement started (health error: %v)", probeErr)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
