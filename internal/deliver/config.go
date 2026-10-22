package deliver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/avery20024/herdr-async-message-relay/internal/i18n"
)

var rolePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

type Config struct {
	Root           string
	StateDir       string
	BatchSize      int
	BatchBytes     int
	Poll           time.Duration
	CommandTimeout time.Duration
	Herdr          string
}

func EnvInt(name string, fallback, min, max int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, i18n.Errorf("%s must be %d..%d", name, min, max)
	}
	return n, nil
}

func LoadConfig(root string) (c Config, err error) {
	if root == "" {
		root, err = os.Getwd()
		if err != nil {
			return
		}
	}
	c.Root, err = filepath.Abs(root)
	if err != nil {
		return
	}
	c.Root, err = filepath.EvalSymlinks(c.Root)
	if err != nil {
		return
	}
	info, e := os.Stat(filepath.Join(c.Root, "角色"))
	if e != nil || !info.IsDir() {
		err = i18n.Errorf("root must contain 角色 directory: %s", c.Root)
		return
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			err = e
			return
		}
		base = filepath.Join(home, ".local", "state")
	}
	c.StateDir = os.Getenv("DELIVER_STATE_DIR")
	if c.StateDir == "" {
		// 保留改名前的儲存位置，讓 hrelay 與既有 daemon 共用原佇列與 socket。
		c.StateDir = filepath.Join(base, "herdr-deliver", fmt.Sprintf("%x", sha256.Sum256([]byte(c.Root)))[:16])
	}
	c.StateDir, err = filepath.Abs(c.StateDir)
	if err != nil {
		return
	}
	c.BatchSize, err = EnvInt("DELIVER_BATCH_SIZE", 3, 1, 100)
	if err != nil {
		return
	}
	c.BatchBytes, err = EnvInt("DELIVER_BATCH_BYTES", 12000, 2048, 64000)
	if err != nil {
		return
	}
	poll, e := EnvInt("DELIVER_POLL_MS", 500, 50, 60000)
	if e != nil {
		err = e
		return
	}
	c.Poll = time.Duration(poll) * time.Millisecond
	c.CommandTimeout = 10 * time.Second
	c.Herdr = os.Getenv("HERDR_BIN_PATH")
	if c.Herdr == "" {
		c.Herdr = "herdr"
	}
	return
}

func (c Config) Socket() string { return filepath.Join(c.StateDir, "deliver.sock") }
func (c Config) Validate(r SendRequest) error {
	for _, role := range []string{r.Sender, r.Recipient} {
		if len(role) > 64 || !rolePattern.MatchString(role) {
			return i18n.Errorf("invalid role: %q", role)
		}
		info, err := os.Stat(filepath.Join(c.Root, "角色", role))
		if err != nil || !info.IsDir() {
			return i18n.Errorf("role directory missing: %s", role)
		}
	}
	if len(r.Key) < 1 || len(r.Key) > 200 {
		return i18n.NewError("idempotency_key must be 1..200 bytes")
	}
	if !utf8.ValidString(r.Body) || strings.ContainsRune(r.Body, '\x00') {
		return i18n.NewError("body must be UTF-8 without NUL bytes")
	}
	if len(r.Body) == 0 || len(r.Body) > c.BatchBytes-1024 {
		return i18n.Errorf("body must be 1..%d bytes", c.BatchBytes-1024)
	}
	return nil
}
