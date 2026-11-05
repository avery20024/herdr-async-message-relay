package claudeguard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Launch 由實際啟動包裝器寫入；不是輪詢 first-seen 或畫面狀態推估。
type Launch struct {
	ID        string `json:"id"`
	Pane      string `json:"pane_id"`
	StartedAt int64  `json:"started_at"`
}

func (m Manager) readLaunch(dir string) (Launch, error) {
	var l Launch
	err := readJSON(filepath.Join(dir, "launch.json"), &l)
	if err == nil && (l.ID == "" || len(l.ID) > 256 || !panePattern.MatchString(l.Pane) || l.StartedAt <= 0) {
		err = errors.New("invalid harness launch record")
	}
	return l, err
}

func (m Manager) StartRun(ctx context.Context, role, pane string) (Launch, error) {
	dir, err := m.directory(role)
	if err != nil {
		return Launch{}, err
	}
	if !panePattern.MatchString(pane) {
		return Launch{}, errors.New("invalid harness pane")
	}
	if err := prepareDir(m.Dir); err != nil {
		return Launch{}, err
	}
	if err := prepareDir(dir); err != nil {
		return Launch{}, err
	}
	release, err := lock(ctx, dir)
	if err != nil {
		return Launch{}, err
	}
	defer release()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Launch{}, err
	}
	l := Launch{ID: hex.EncodeToString(nonce[:]), Pane: pane, StartedAt: time.Now().UnixMilli()}
	// nonce 是主識別；時鐘回撥或同毫秒重啟仍不沿用舊問答。
	if err := writeJSON(dir, "launch.json", l); err != nil {
		return Launch{}, err
	}
	return l, nil
}

func (m Manager) unavailable(err error) {
	if m.OnUnavailable != nil && !errors.Is(err, os.ErrNotExist) {
		m.OnUnavailable(err)
	}
}
