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
 printf '%s\n' '{"result":{"agents":[{"cwd":"/team/角色/gm","pane_id":"wC:p5","agent_status":"idle","state_change_seq":10}]}}'
 exit 0
fi
[ "$1" = agent ] && [ "$2" = prompt ] && [ "$3" = wC:p5 ] || exit 7
[ "$5" = --wait ] && [ "$14" = --timeout ] && [ "$15" = 7000 ] || exit 8
printf '%s\n' '{"result":{"agent_status":"working"}}'
`
	// POSIX shell 的兩位數參數需使用大括號。
	script = strings.ReplaceAll(strings.ReplaceAll(script, `"$14"`, `"${14}"`), `"$15"`, `"${15}"`)
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := CLIHerdr{Binary: path, Timeout: time.Second}
	agents, err := h.List(context.Background())
	if err != nil || len(agents) != 1 || agents[0].Pane != "wC:p5" {
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

func TestResolveAgentAcceptsSubdirectoryAndForegroundCWD(t *testing.T) {
	root := t.TempDir()
	roleDir := filepath.Join(root, "角色", "gm")
	for _, role := range []string{"gm", "dev"} {
		if err := os.MkdirAll(filepath.Join(root, "角色", role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	seq := int64(4)
	cases := []struct {
		name   string
		agents []Agent
		want   string
	}{
		{"exact", []Agent{{CWD: roleDir, Pane: "wA:p1", Seq: &seq}}, "wA:p1"},
		{"subdirectory", []Agent{{CWD: filepath.Join(roleDir, "工作記錄"), Pane: "wB:p2", Seq: &seq}}, "wB:p2"},
		{"foreground", []Agent{{CWD: "/somewhere", ForegroundCWD: roleDir, Pane: "wC:p3", Seq: &seq}}, "wC:p3"},
		{"exact beats subdirectory", []Agent{
			{CWD: filepath.Join(roleDir, "工作記錄"), Pane: "wB:p2", Seq: &seq},
			{CWD: roleDir, Pane: "wA:p1", Seq: &seq},
		}, "wA:p1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := ResolveAgent(root, "gm", tc.agents)
			if err != nil {
				t.Fatal(err)
			}
			if a.Pane != tc.want {
				t.Fatalf("pane %s want %s", a.Pane, tc.want)
			}
		})
	}
	t.Run("ambiguous and missing", func(t *testing.T) {
		if _, err := ResolveAgent(root, "gm", []Agent{
			{CWD: roleDir, Pane: "wA:p1", Seq: &seq},
			{CWD: roleDir, Pane: "wB:p2", Seq: &seq},
		}); err == nil {
			t.Fatal("two seats accepted")
		}
		if _, err := ResolveAgent(root, "gm", []Agent{{CWD: filepath.Join(root, "角色", "dev"), Pane: "wA:p1", Seq: &seq}}); err == nil {
			t.Fatal("unrelated seat accepted")
		}
		if _, err := ResolveAgent(root, "gm", []Agent{{CWD: roleDir, Pane: "bad", Seq: &seq}}); err == nil {
			t.Fatal("invalid pane accepted")
		}
	})
}

func TestResolveAgentIgnoresOtherRoleSeats(t *testing.T) {
	root := t.TempDir()
	for _, role := range []string{"gm", "dev", "dev-2"} {
		if err := os.MkdirAll(filepath.Join(root, "角色", role, "sub"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	seq := int64(3)
	devDir := filepath.Join(root, "角色", "dev")
	foreign := Agent{CWD: filepath.Join(root, "角色", "gm"), ForegroundCWD: filepath.Join(devDir, "sub"), Pane: "w1:p1", Seq: &seq}
	if _, err := ResolveAgent(root, "dev", []Agent{foreign}); err == nil {
		t.Fatal("another role's foreground claimed this role")
	}
	if _, err := ResolveAgent(root, "dev", []Agent{{CWD: filepath.Join(root, "角色", "dev-2"), Pane: "w1:p2", Seq: &seq}}); err == nil {
		t.Fatal("dev-2 seat eaten by dev")
	}
	devSeat := Agent{CWD: devDir, Pane: "w1:p3", Seq: &seq}
	exactForeground := Agent{CWD: filepath.Join(root, "角色", "gm"), ForegroundCWD: devDir, Pane: "w1:p4", Seq: &seq}
	if a, err := ResolveAgent(root, "dev", []Agent{devSeat, exactForeground}); err != nil || a.Pane != "w1:p3" {
		t.Fatalf("real seat lost to foreign foreground: %+v %v", a, err)
	}
	subSeat := Agent{CWD: filepath.Join(devDir, "sub"), Pane: "w1:p5", Seq: &seq}
	if a, err := ResolveAgent(root, "dev", []Agent{subSeat, exactForeground}); err != nil || a.Pane != "w1:p5" {
		t.Fatalf("subdirectory seat must outrank foreground: %+v %v", a, err)
	}
	if a, err := ResolveAgent(root, "dev", []Agent{{CWD: "/elsewhere", ForegroundCWD: devDir, Pane: "w1:p6", Seq: &seq}}); err != nil || a.Pane != "w1:p6" {
		t.Fatalf("foreground fallback broken: %+v %v", a, err)
	}
}

// 團隊根以符號連結傳入、agent 回報實體路徑時，不得用 foreground 認領別的角色（第二輪 M）。
func TestResolveAgentSymlinkedRootIgnoresOtherRole(t *testing.T) {
	physical := t.TempDir()
	for _, role := range []string{"gm", "dev"} {
		if err := os.MkdirAll(filepath.Join(physical, "角色", role, "sub"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	logical := filepath.Join(t.TempDir(), "team-link")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	seq := int64(5)
	physicalGM := filepath.Join(physical, "角色", "gm")
	physicalDev := filepath.Join(physical, "角色", "dev")
	foreign := Agent{CWD: physicalGM, ForegroundCWD: physicalDev, Pane: "w1:p1", Seq: &seq}
	if _, err := ResolveAgent(logical, "dev", []Agent{foreign}); err == nil {
		t.Fatal("symlinked root let another role's foreground claim this role")
	}
	if _, err := ResolveAgent(logical, "dev", []Agent{{CWD: filepath.Join(logical, "角色", "gm"), ForegroundCWD: filepath.Join(logical, "角色", "dev"), Pane: "w1:p2", Seq: &seq}}); err == nil {
		t.Fatal("logical cross-role path accepted")
	}
	// 實體路徑回報本角色座位時仍要能解析。
	if a, err := ResolveAgent(logical, "gm", []Agent{{CWD: physicalGM, Pane: "w1:p3", Seq: &seq}}); err != nil || a.Pane != "w1:p3" {
		t.Fatalf("physical seat lost: %+v %v", a, err)
	}
	// cwd 恰在角色容器本身時不得被任何角色以 foreground 認領。
	if _, err := ResolveAgent(logical, "dev", []Agent{{CWD: filepath.Join(physical, "角色"), ForegroundCWD: physicalDev, Pane: "w1:p4", Seq: &seq}}); err == nil {
		t.Fatal("roles-directory cwd claimed a role via foreground")
	}
}

// held 查詢需有 message_id 索引，避免大量歷史時每次掃描退化（第二輪 Low）。
func TestBatchMembersMessageIndexExists(t *testing.T) {
	s, _ := fixture(t)
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='batch_members_message'`).Scan(&name); err != nil {
		t.Fatalf("missing batch_members(message_id) index: %v", err)
	}
}
