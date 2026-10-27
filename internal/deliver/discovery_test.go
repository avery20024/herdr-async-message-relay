package deliver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForegroundDiscoveryWithoutLayout(t *testing.T) {
	root := t.TempDir()
	seq := int64(1)
	if err := os.MkdirAll(root+"-other/agents/reviewer", 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root + "-other") })
	if _, err := RoleFromDirectory(root, root); err == nil {
		t.Fatal("root itself accepted as a role")
	}
	for _, path := range []string{"agents/reviewer", "開発/担当A", "other/reviewer", "workers/dev_2"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	agent := func(path string) Agent {
		return Agent{CWD: "/unrelated", ForegroundCWD: filepath.Join(root, path), Pane: "w1:p1", Seq: &seq}
	}
	for _, tc := range []struct{ path, role string }{{"agents/reviewer", "reviewer"}, {"開発/担当A", "担当A"}, {"workers/dev_2", "dev_2"}} {
		if _, err := ResolveAgent(root, tc.role, []Agent{agent(tc.path)}); err != nil {
			t.Errorf("%s: %v", tc.role, err)
		}
	}
	if _, err := ResolveAgent(root, "reviewer", []Agent{agent("agents/reviewer"), agent("other/reviewer")}); err == nil {
		t.Fatal("duplicate role accepted")
	}
	for _, bad := range []Agent{
		{CWD: filepath.Join(root, "agents/reviewer"), Pane: "w1:p1", Seq: &seq},
		{ForegroundCWD: root, Pane: "w1:p1", Seq: &seq},
		{ForegroundCWD: filepath.Join(root, "missing/reviewer"), Pane: "w1:p1", Seq: &seq},
		{ForegroundCWD: "agents/reviewer", Pane: "w1:p1", Seq: &seq},
		{ForegroundCWD: root + "-other/agents/reviewer", Pane: "w1:p1", Seq: &seq},
	} {
		if _, err := ResolveAgent(root, "reviewer", []Agent{bad}); err == nil {
			t.Fatalf("invalid foreground accepted: %+v", bad)
		}
	}
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "reviewer"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAgent(root, "reviewer", []Agent{agent("escape/reviewer")}); err == nil {
		t.Fatal("symlink escape accepted")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAgent(alias, "reviewer", []Agent{agent("agents/reviewer")}); err != nil {
		t.Fatal(err)
	}
	// 身分以目前 foreground 為準，不沿用 shell 的舊 cwd。
	a := agent("agents/reviewer")
	a.CWD = filepath.Join(root, "workers/dev_2")
	if _, err := ResolveAgent(root, "reviewer", []Agent{a}); err != nil {
		t.Fatal(err)
	}
}

func TestRootAndSendWithoutRoleDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DELIVER_STATE_DIR", t.TempDir())
	c, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reviewer", "dev_2", "担当A", "Réviseur"} {
		if err := c.Validate(SendRequest{Sender: name, Recipient: "offline", Key: "key", Body: "test"}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"", "../gm", "a/b", "a\\b", "-option", "a\nrole", "."} {
		if err := c.Validate(SendRequest{Sender: name, Recipient: "offline", Key: "key", Body: "test"}); err == nil {
			t.Errorf("invalid role %q accepted", name)
		}
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(file); err == nil {
		t.Fatal("file accepted as root")
	}
}
