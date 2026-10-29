package deliver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

// RootMarker 是團隊根目錄的標記檔名稱；內容只記格式版本，不記路徑，複製到他處也不會指向原根目錄。
const RootMarker = ".hrelay"

type rootMarker struct {
	Schema int `json:"schema"`
}

// FindRoot 自 start 起逐層向上尋找標記檔，只認標記檔，不以目錄名稱推測根目錄。
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	for {
		info, e := os.Lstat(filepath.Join(dir, RootMarker))
		if e == nil && info.Mode().IsRegular() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", i18n.NewError("no relay root: pass --root, set DELIVER_ROOT, or run `hrelay init` in the team root")
		}
		dir = parent
	}
}

// InitRoot 在既存目錄建立標記檔；已有有效標記檔時回報 created=false，不覆寫。
func InitRoot(dir string) (root string, created bool, err error) {
	root, err = filepath.Abs(dir)
	if err != nil {
		return
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	info, e := os.Stat(root)
	if e != nil || !info.IsDir() {
		err = i18n.Errorf("root must be an existing directory: %s", root)
		return
	}
	path := filepath.Join(root, RootMarker)
	data, _ := json.Marshal(rootMarker{Schema: 1})
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(e, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		var m rootMarker
		if readErr != nil || json.Unmarshal(existing, &m) != nil || m.Schema != 1 {
			err = i18n.Errorf("%s exists but is not a valid relay marker; not overwritten", path)
		}
		return
	}
	if e != nil {
		err = e
		return
	}
	_, e = f.Write(append(data, '\n'))
	if ce := f.Close(); e == nil {
		e = ce
	}
	return root, e == nil, e
}
