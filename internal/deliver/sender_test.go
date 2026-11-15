package deliver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func senderSeat(t *testing.T, root, dir, role, pane, tab string) Agent {
	t.Helper()
	path := filepath.Join(root, dir)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	seq := int64(1)
	return Agent{ForegroundCWD: path, Name: role, TabLabel: role, Pane: pane, TabID: tab, Seq: &seq}
}

func TestResolveSenderDirectoryAndBinding(t *testing.T) {
	root := t.TempDir()
	reviewer := senderSeat(t, root, "staff/reviewer", "reviewer", "wT:p1", "wT:t1")
	gm := senderSeat(t, root, "staff/gm", "gm", "wT:p2", "wT:t2")
	d1 := senderSeat(t, root, "shared", "dispatch-one", "wT:p3", "wT:t3")
	d2 := senderSeat(t, root, "shared", "dispatch-two", "wT:p4", "wT:t4")
	agents := []Agent{reviewer, gm, d1, d2}
	sub := filepath.Join(reviewer.ForegroundCWD, "logs")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	cases := []struct {
		name            string
		caller          SenderContext
		want, errorPart string
	}{
		{"pwd without environment", SenderContext{CWD: reviewer.ForegroundCWD}, "reviewer", ""},
		{"subdirectory", SenderContext{CWD: sub}, "reviewer", ""},
		{"matching hints", SenderContext{CWD: sub, Explicit: "reviewer", Environment: "reviewer", Legacy: "reviewer"}, "reviewer", ""},
		{"wrong environment", SenderContext{CWD: sub, Environment: "gm"}, "", "identity conflict"},
		{"wrong explicit", SenderContext{CWD: sub, Explicit: "gm"}, "", "identity conflict"},
		{"wrong legacy wrapper hint", SenderContext{CWD: sub, Legacy: "gm"}, "", "identity conflict"},
		{"wrong inherited pane", SenderContext{CWD: sub, Pane: gm.Pane, Tab: gm.TabID, Environment: "gm"}, "", "pwd/seat conflict"},
		{"pane tab disagreement", SenderContext{CWD: sub, Pane: reviewer.Pane, Tab: gm.TabID}, "", "pane/tab conflict"},
		{"unknown pane no cwd fallback", SenderContext{CWD: sub, Pane: "wT:p9"}, "", "seat must be unique"},
		{"shared dispatch binding", SenderContext{CWD: d1.ForegroundCWD, Pane: d2.Pane, Tab: d2.TabID, Environment: "dispatch-two"}, "dispatch-two", ""},
		{"tab only shared", SenderContext{CWD: d1.ForegroundCWD, Tab: d1.TabID}, "dispatch-one", ""},
		{"shared env not enough", SenderContext{CWD: d1.ForegroundCWD, Environment: "dispatch-one"}, "", "shared"},
		{"shared explicit not enough", SenderContext{CWD: d1.ForegroundCWD, Explicit: "dispatch-one"}, "", "shared"},
		{"shared wrong role", SenderContext{CWD: d1.ForegroundCWD, Pane: d1.Pane, Environment: "dispatch-two"}, "", "identity conflict"},
		{"external worktree seat", SenderContext{CWD: worktree, Pane: reviewer.Pane, Environment: "reviewer"}, "reviewer", ""},
		{"worktree never basename", SenderContext{CWD: worktree, Environment: "reviewer"}, "", "no matching live"},
		{"root never basename", SenderContext{CWD: root, InsideHerdr: true, Environment: "reviewer"}, "", "no matching live"},
		{"outside explicit manual", SenderContext{CWD: worktree, Explicit: "operator"}, "operator", ""},
		{"manual hint conflict", SenderContext{CWD: worktree, Explicit: "operator", Environment: "gm"}, "", "identity conflict"},
		{"herdr marker forbids manual fallback", SenderContext{CWD: worktree, Explicit: "operator", InsideHerdr: true}, "", "no matching live"},
		{"invalid hint", SenderContext{CWD: sub, Environment: "../gm"}, "", "invalid sender hint"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSender(root, tt.caller, agents)
			if tt.errorPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errorPart) {
					t.Fatalf("got %q, %v; want %s", got, err, tt.errorPart)
				}
			} else if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestResolveSenderPhysicalRootAndAmbiguity(t *testing.T) {
	root := t.TempDir()
	a := senderSeat(t, root, "people/審核", "reviewer-2", "wT:p1", "wT:t1")
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(a.ForegroundCWD, alias); err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveSender(root, SenderContext{CWD: alias}, []Agent{a}); err != nil || got != "reviewer-2" {
		t.Fatal(got, err)
	}
	b := senderSeat(t, root, "other", "reviewer-2", "wT:p2", "wT:t2")
	if _, err := ResolveSender(root, SenderContext{CWD: alias, Pane: a.Pane}, []Agent{a, b}); err == nil {
		t.Fatal("duplicate complete address accepted")
	}
	b.TabLabel, b.Name, b.Pane, b.TabID = "other", "other", a.Pane, a.TabID
	if _, err := ResolveSender(root, SenderContext{CWD: alias, Pane: a.Pane}, []Agent{a, b}); err == nil {
		t.Fatal("duplicate pane accepted")
	}
	if _, err := ResolveSender(root, SenderContext{CWD: alias, Tab: a.TabID}, []Agent{a, b}); err == nil {
		t.Fatal("duplicate tab accepted")
	}
	outside := t.TempDir()
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	a.ForegroundCWD = escape
	if _, err := ResolveSender(root, SenderContext{CWD: outside, Pane: a.Pane}, []Agent{a}); err == nil || !strings.Contains(err.Error(), "outside relay root") {
		t.Fatal(err)
	}
	if _, err := ResolveSender(root, SenderContext{CWD: filepath.Join(root, "missing")}, nil); err == nil {
		t.Fatal("missing cwd accepted")
	}
}

type senderLookup struct {
	agents []Agent
	err    error
	calls  int
}

func (s *senderLookup) List(context.Context) ([]Agent, error) { s.calls++; return s.agents, s.err }
func (*senderLookup) Prompt(context.Context, string, string) error {
	panic("sender lookup must never prompt")
}

func TestResolveSenderSpecificDirectoryAndMetadata(t *testing.T) {
	root := t.TempDir()
	parent := senderSeat(t, root, "people", "parent-role", "wT:p1", "wT:t1")
	child := senderSeat(t, root, "people/reviewer", "child-role", "wT:p2", "wT:t2")
	if got, err := ResolveSender(root, SenderContext{CWD: child.ForegroundCWD}, []Agent{parent, child}); err != nil || got != "child-role" {
		t.Fatal("parent directory shadowed specific seat", got, err)
	}
	// 有效 tab 名稱覆蓋 basename／agent 名稱，不把低順位名稱當別名。
	child.Name = "reviewer"
	if _, err := ResolveSender(root, SenderContext{CWD: child.ForegroundCWD, Environment: "reviewer"}, []Agent{child}); err == nil {
		t.Fatal("overridden name accepted as sender alias")
	}
	child.TabLabel = ""
	child.TabID = ""
	if got, err := ResolveSender(root, SenderContext{CWD: child.ForegroundCWD}, []Agent{child}); err != nil || got != "reviewer" {
		t.Fatal("legacy agent-name fallback broken", got, err)
	}
	child.Seq = nil
	if _, err := ResolveSender(root, SenderContext{CWD: child.ForegroundCWD}, []Agent{child}); err == nil {
		t.Fatal("missing seat metadata accepted")
	}
}

func TestIdentifySenderOfflineBoundaries(t *testing.T) {
	root := t.TempDir()
	c := Config{Root: root}
	for _, caller := range []SenderContext{
		{CWD: root, Environment: "gm"},
		{CWD: root, Explicit: "gm", Pane: "wT:p1"},
		{CWD: root, Explicit: "gm", Tab: "wT:t1"},
		{CWD: root, Explicit: "gm", InsideHerdr: true},
	} {
		h := &senderLookup{err: errors.New("lookup unavailable")}
		if _, err := IdentifySender(context.Background(), c, caller, h); err == nil {
			t.Fatal("unverified environment or Herdr sender accepted", caller)
		}
	}
	h := &senderLookup{err: errors.New("lookup unavailable")}
	if got, err := IdentifySender(context.Background(), c, SenderContext{CWD: root, Explicit: "operator"}, h); err != nil || got != "operator" {
		t.Fatal(got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := IdentifySender(ctx, c, SenderContext{CWD: root, Explicit: "operator"}, h); err == nil {
		t.Fatal("canceled discovery fell back to manual send")
	}
	h = &senderLookup{}
	if _, err := IdentifySender(context.Background(), c, SenderContext{CWD: root, Explicit: "bad/role"}, h); err == nil || h.calls != 0 {
		t.Fatal("invalid hint queried Herdr", err, h.calls)
	}
}
