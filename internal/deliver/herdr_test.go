package deliver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIHerdrWithFakeBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr")
	script := `#!/bin/sh
if [ "$1 $2" = "agent list" ]; then
 printf '%s\n' '{"result":{"agents":[{"foreground_cwd":"/team/agents/gm","pane_id":"wC:p5","agent_status":"idle","state_change_seq":10}]}}'
 exit 0
fi
[ "$1" = agent ] && [ "$2" = prompt ] && [ "$3" = wC:p5 ] || exit 7
[ "$5" = --wait ] && [ "${14}" = --timeout ] && [ "${15}" = 7000 ] || exit 8
printf '%s\n' '{"result":{"agent_status":"working"}}'
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := CLIHerdr{Binary: path, Timeout: time.Second}
	agents, err := h.List(context.Background())
	if err != nil || len(agents) != 1 || agents[0].Pane != "wC:p5" || agents[0].ForegroundCWD != "/team/agents/gm" {
		t.Fatalf("%+v %v", agents, err)
	}
	if err = h.Prompt(context.Background(), "wC:p5", "body\n'quoted'"); err != nil {
		t.Fatal(err)
	}
}

func TestCLIRejectsMalformedAndErrorResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "herdr")
	for _, response := range []string{`help text`, `{}`, `{"error":{"code":"agent_blocked"}}`, `{"result":null}`} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+response+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		h := CLIHerdr{Binary: path, Timeout: time.Second}
		if _, err := h.List(context.Background()); err == nil {
			t.Fatal(response)
		}
		if err := h.Prompt(context.Background(), "wC:p5", "body"); err == nil {
			t.Fatal(response)
		}
	}
}

func TestResolveAgentRequiresValidIdentity(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "gm")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	seq := int64(1)
	negative := int64(-1)
	for _, a := range []Agent{
		{ForegroundCWD: dir, Pane: "bad", Seq: &seq},
		{ForegroundCWD: dir, Pane: "w1:p1"},
		{ForegroundCWD: dir, Pane: "w1:p1", Seq: &negative},
	} {
		if _, err := ResolveAgent(root, "gm", []Agent{a}); err == nil {
			t.Fatalf("invalid identity accepted: %+v", a)
		}
	}
	good := Agent{ForegroundCWD: dir, Pane: "w1:p1", Seq: &seq}
	if _, err := ResolveAgent(root, "gm", []Agent{good, good}); err == nil || !strings.Contains(err.Error(), "matched 2") {
		t.Fatal("duplicate pane must not hide a role collision", err)
	}
	if _, err := ResolveAgent(root, "gm", nil); err == nil {
		t.Fatal("missing seat accepted")
	}
}

// held 查詢需有 message_id 索引，避免大量歷史時每次掃描退化。
func TestBatchMembersMessageIndexExists(t *testing.T) {
	s, _ := fixture(t)
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='batch_members_message'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
}
