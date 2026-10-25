package queueui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const preferencesLimit = 4096

type Preferences struct {
	Version  int    `json:"version"`
	Language string `json:"language"`
	Theme    string `json:"theme"`
}

func defaultPreferences() Preferences {
	return Preferences{Version: 1, Language: "auto", Theme: "terminal"}
}
func (p Preferences) validate() error {
	if p.Version != 1 {
		return errors.New("unsupported preferences version")
	}
	if p.Language != "auto" && p.Language != "en" && p.Language != "zh-TW" {
		return errors.New("invalid preferences language")
	}
	if p.Theme != "terminal" {
		if _, ok := palettes[p.Theme]; !ok {
			return errors.New("invalid preferences theme")
		}
	}
	return nil
}

type PreferencesStore interface {
	Load() (Preferences, error)
	Save(Preferences) error
}

type FilePreferencesStore struct{ Path string }

func preferencesPath() (string, error) {
	if path := os.Getenv("HERDR_RELAY_UI_CONFIG"); path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New("HERDR_RELAY_UI_CONFIG must be absolute")
		}
		return filepath.Clean(path), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("user config directory must be absolute")
	}
	return filepath.Join(dir, "hrelay", "ui-preferences.json"), nil
}

func (s FilePreferencesStore) checkPath() error {
	if !filepath.IsAbs(s.Path) {
		return errors.New("preferences path must be absolute")
	}
	// 專屬目錄與檔案不可是符號連結；不chmod其他既有共用設定目錄。
	if info, err := os.Lstat(filepath.Dir(s.Path)); err == nil {
		if !info.IsDir() {
			return errors.New("preferences directory must be a real directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if info, err := os.Lstat(s.Path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("preferences must be a regular file, not a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s FilePreferencesStore) Load() (Preferences, error) {
	fallback := defaultPreferences()
	if err := s.checkPath(); err != nil {
		return fallback, err
	}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fallback, err
	}
	if !info.Mode().IsRegular() {
		return fallback, errors.New("preferences must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, preferencesLimit+1))
	if err != nil {
		return fallback, err
	}
	if len(data) > preferencesLimit {
		return fallback, errors.New("preferences exceed 4 KiB")
	}
	// 重複欄位也拒絕，避免不同讀取器對同一份設定有不同解讀。
	scan := json.NewDecoder(bytes.NewReader(data))
	if tok, err := scan.Token(); err != nil || tok != json.Delim('{') {
		return fallback, errors.New("preferences must be a JSON object")
	}
	seen := map[string]bool{}
	for scan.More() {
		tok, err := scan.Token()
		if err != nil {
			return fallback, err
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return fallback, errors.New("duplicate preferences field")
		}
		seen[key] = true
		var value json.RawMessage
		if err := scan.Decode(&value); err != nil {
			return fallback, err
		}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var p Preferences
	if err := d.Decode(&p); err != nil {
		return fallback, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fallback, errors.New("preferences must contain exactly one object")
	}
	if err := p.validate(); err != nil {
		return fallback, err
	}
	return p, nil
}

func (s FilePreferencesStore) Save(p Preferences) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := s.checkPath(); err != nil {
		return err
	}
	// 不以新版本設定默默覆蓋未知版本或損毀檔案。
	if _, err := s.Load(); err != nil {
		return fmt.Errorf("cannot overwrite unreadable preferences: %w", err)
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := s.checkPath(); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".ui-preferences-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, s.Path)
}
