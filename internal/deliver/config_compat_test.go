package deliver

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
)

func TestRenamePreservesStateDirectoryAndSocket(t *testing.T) {
	_, original := fixture(t)
	base := t.TempDir()
	t.Setenv("XDG_STATE_HOME", base)
	t.Setenv("DELIVER_STATE_DIR", "")
	t.Setenv("DELIVER_BATCH_SIZE", "")
	t.Setenv("DELIVER_BATCH_BYTES", "")
	t.Setenv("DELIVER_POLL_MS", "")
	c, err := LoadConfig(original.Root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "herdr-deliver", fmt.Sprintf("%x", sha256.Sum256([]byte(c.Root)))[:16])
	if c.StateDir != want || c.Socket() != filepath.Join(want, "deliver.sock") {
		t.Fatalf("rename changed persistent address: state=%s socket=%s", c.StateDir, c.Socket())
	}
	if c.BatchSize != 3 {
		t.Fatalf("batch default changed: %d", c.BatchSize)
	}
}
