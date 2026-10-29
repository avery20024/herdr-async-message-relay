package deliver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentNameOverridesDirectoryButStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	seq := int64(1)
	worktree := filepath.Join(root, "frontend-api-789")
	if err := os.MkdirAll(worktree, 0700); err != nil {
		t.Fatal(err)
	}
	named := Agent{Name: "tmp-fix", ForegroundCWD: worktree, Pane: "w1:p2", Seq: &seq}

	if _, err := ResolveAgent(root, "tmp-fix", []Agent{named}); err != nil {
		t.Fatalf("named seat must be addressable by its name: %v", err)
	}
	if _, err := ResolveAgent(root, "frontend-api-789", []Agent{named}); err == nil {
		t.Fatal("a named seat must not also answer to its directory name")
	}
	unnamed := named
	unnamed.Name = ""
	if _, err := ResolveAgent(root, "frontend-api-789", []Agent{unnamed}); err != nil {
		t.Fatalf("unnamed seats keep directory-based roles: %v", err)
	}

	foreign := named
	foreign.ForegroundCWD = outside
	if _, err := ResolveAgent(root, "tmp-fix", []Agent{foreign}); err == nil {
		t.Fatal("a name must not pull a seat from outside the relay root")
	}
}

func TestInvalidAgentNameFallsBackAndDuplicatesStayAmbiguous(t *testing.T) {
	root := t.TempDir()
	seq := int64(1)
	for _, dir := range []string{"agents/dev", "agents/other"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	bad := Agent{Name: "has space", ForegroundCWD: filepath.Join(root, "agents/dev"), Pane: "w1:p1", Seq: &seq}
	if _, err := ResolveAgent(root, "dev", []Agent{bad}); err != nil {
		t.Fatalf("an invalid name must fall back to the directory role: %v", err)
	}
	renamed := Agent{Name: "dev", ForegroundCWD: filepath.Join(root, "agents/other"), Pane: "w1:p2", Seq: &seq}
	if _, err := ResolveAgent(root, "dev", []Agent{bad, renamed}); err == nil {
		t.Fatal("two seats claiming one role must stay ambiguous")
	}
}

func TestAgentListJSONCarriesName(t *testing.T) {
	var a Agent
	if err := json.Unmarshal([]byte(`{"name":"tmp-fix","pane_id":"w1:p1","foreground_cwd":"/x"}`), &a); err != nil || a.Name != "tmp-fix" {
		t.Fatalf("name not decoded: %+v %v", a, err)
	}
}
