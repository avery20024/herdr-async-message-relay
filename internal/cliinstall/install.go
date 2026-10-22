// Package cliinstall 安裝原生 CLI 副本，不依賴 managed checkout 路徑或 shell shim。
package cliinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

type Result struct {
	Path   string `json:"path"`
	OnPath bool   `json:"on_path"`
}

type ownership struct {
	Schema int    `json:"schema"`
	SHA256 string `json:"sha256"`
}

func digest(body []byte) string {
	value := sha256.Sum256(body)
	return hex.EncodeToString(value[:])
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".hrelay-stage-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = file.Chmod(mode); err != nil {
		return err
	}
	if _, err = file.Write(body); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, i18n.Errorf("refusing non-regular CLI installation file: %s", path)
	}
	return os.ReadFile(path)
}

// Install 只更新由本安裝器記錄且未被另行修改的檔案；其他來源不自動覆蓋。
func Install(ctx context.Context, source, binDir string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if !filepath.IsAbs(binDir) || strings.ContainsFunc(binDir, func(r rune) bool { return r < 32 || r == 127 }) {
		return Result{}, i18n.NewError("CLI bin directory must be an absolute path without control characters")
	}
	if err := os.MkdirAll(binDir, 0700); err != nil {
		return Result{}, err
	}
	binDir, err := filepath.EvalSymlinks(binDir)
	if err != nil {
		return Result{}, err
	}
	lock, err := os.OpenFile(filepath.Join(binDir, ".hrelay-install.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	for {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return Result{}, err
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	body, err := readRegular(source)
	if err != nil {
		return Result{}, err
	}
	if len(body) == 0 {
		return Result{}, i18n.NewError("cannot install an empty CLI binary")
	}
	target := filepath.Join(binDir, "hrelay")
	marker := filepath.Join(binDir, ".hrelay-install.json")
	previous, targetErr := readRegular(target)
	if targetErr != nil && !errors.Is(targetErr, os.ErrNotExist) {
		return Result{}, targetErr
	}
	owned, markerErr := readRegular(marker)
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return Result{}, markerErr
	}
	if markerErr == nil {
		var owner ownership
		if json.Unmarshal(owned, &owner) != nil || owner.Schema != 1 || len(owner.SHA256) != 64 {
			return Result{}, i18n.NewError("invalid CLI ownership record; inspect the installation before retrying")
		}
		if targetErr == nil && digest(previous) != owner.SHA256 {
			return Result{}, i18n.NewError("existing hrelay differs from its ownership record; refusing to overwrite")
		}
	} else if targetErr == nil {
		return Result{}, i18n.Errorf("existing hrelay is not owned by this installer: %s; choose another --bin-dir", target)
	}
	if err := atomicWrite(target, body, 0755); err != nil {
		return Result{}, err
	}
	record, err := json.Marshal(ownership{Schema: 1, SHA256: digest(body)})
	if err != nil {
		return Result{}, err
	}
	if err := atomicWrite(marker, append(record, '\n'), 0600); err != nil {
		return Result{}, i18n.Errorf("CLI copied but ownership record could not be saved; inspect before retrying: %w", err)
	}
	result := Result{Path: target}
	if found, err := exec.LookPath("hrelay"); err == nil {
		abs, err := filepath.Abs(found)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(abs)
			result.OnPath = err == nil && resolved == target
		}
	}
	return result, nil
}
