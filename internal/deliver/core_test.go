package deliver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type coreFixture struct {
	h        CLICore
	dir      string
	terminal map[string]any
}

func coreSetup(t *testing.T) *coreFixture {
	t.Helper()
	dir := t.TempDir()
	socketDir, err := os.MkdirTemp("/tmp", "hc-fake-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(socketDir, "core.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = os.RemoveAll(socketDir)
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done; _ = os.RemoveAll(socketDir) })
	foreground := filepath.Join(dir, "agents", "shared")
	if err := os.MkdirAll(foreground, 0700); err != nil {
		t.Fatal(err)
	}
	f := &coreFixture{dir: dir, h: CLICore{Binary: filepath.Join(dir, "hc"), Socket: socket, Root: dir, Timeout: 5 * time.Second}, terminal: map[string]any{
		"address": "t2", "name": "gm", "state": "live", "cwd": dir,
		"foreground": map[string]any{"cwd": foreground, "pid": 77}, "status": map[string]any{"app": "pi", "state": "idle"}, "turn": int64(4), "pid": 42, "created_ms": 100,
	}}
	f.write(t, "ls", map[string]any{"terminals": []any{map[string]any{"address": "t2", "state": "live"}}})
	f.write(t, "get", f.terminal)
	f.write(t, "send", map[string]any{"turn": 4, "status": "idle", "started": true})
	script := `#!/bin/sh
set -eu
[ "$HC_SOCKET" = ` + shellQuote(socket) + ` ]
[ -z "${HC_TERMINAL_ID:-}" ]
[ -z "${HC_TERMINAL_SOCKET:-}" ]
printf '%s\000' "$@" >> ` + shellQuote(filepath.Join(dir, "calls")) + `
case "$1" in
ls) cat ` + shellQuote(filepath.Join(dir, "ls.json")) + ` ;;
get) cat ` + shellQuote(filepath.Join(dir, "get.json")) + ` ;;
send) cat ` + shellQuote(filepath.Join(dir, "send.json")) + ` ;;
*) exit 99 ;;
esac
`
	if err := os.WriteFile(f.h.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return f
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func (f *coreFixture) write(t *testing.T, command string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.dir, command+".json")
	if err := os.WriteFile(path+".tmp", body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
}
func (f *coreFixture) calls(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func (f *coreFixture) agent(t *testing.T) Agent {
	t.Helper()
	agents, err := f.h.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a, err := ResolveAgent(f.h.Root, "gm", agents)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCoreDiscoveryAndVerbatimSubmission(t *testing.T) {
	f := coreSetup(t)
	a := f.agent(t)
	if a.Backend != "core" || a.Pane != "t2" || a.Seq == nil || *a.Seq != 4 {
		t.Fatalf("%+v", a)
	}
	body := "exact\n'quoted' $(never)\n"
	if err := f.h.PromptAgent(context.Background(), "gm", a, body); err != nil {
		t.Fatal(err)
	}
	calls := f.calls(t)
	if !strings.Contains(calls, "send\x00--json\x00--if\x00idle\x00--\x00t2\x00"+body+"\x00") || strings.Contains(calls, "--force") || strings.Contains(calls, "--wait") {
		t.Fatal(calls)
	}
}

func TestCoreDiscoveryRejectsInvalidAndDuplicateTopology(t *testing.T) {
	for _, value := range []any{nil, map[string]any{}, map[string]any{"terminals": []any{map[string]any{"address": "--help", "state": "live"}}}, map[string]any{"terminals": []any{map[string]any{"address": "t2", "state": "live"}, map[string]any{"address": "t2", "state": "live"}}}} {
		f := coreSetup(t)
		f.write(t, "ls", value)
		if _, err := f.h.List(context.Background()); err == nil {
			t.Fatal("invalid topology accepted", value)
		}
	}
}

func TestCoreDiscoverySkipsRemoteAndFinished(t *testing.T) {
	f := coreSetup(t)
	f.write(t, "ls", map[string]any{"terminals": []any{map[string]any{"address": "box/t2", "state": "live"}, map[string]any{"address": "t3", "state": "finished"}}})
	agents, err := f.h.List(context.Background())
	if err != nil || len(agents) != 0 || strings.Contains(f.calls(t), "get\x00") {
		t.Fatal(agents, err)
	}
}

func TestCoreDiscoveryDoesNotGuessDirectoryOrBypassRoot(t *testing.T) {
	for _, current := range []any{nil, map[string]any{"cwd": "relative"}, map[string]any{"cwd": "/"}} {
		f := coreSetup(t)
		f.terminal["foreground"] = current
		f.write(t, "get", f.terminal)
		agents, err := f.h.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ResolveAgent(f.h.Root, "gm", agents); err == nil {
			t.Fatal("missing or outside foreground accepted")
		}
	}
	f := coreSetup(t)
	link := filepath.Join(f.dir, "escape")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	f.terminal["foreground"] = map[string]any{"cwd": link}
	f.write(t, "get", f.terminal)
	agents, err := f.h.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAgent(f.h.Root, "gm", agents); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestCoreRechecksNameGenerationTurnAndReadiness(t *testing.T) {
	for _, change := range []map[string]any{{"name": "review"}, {"pid": 43}, {"foreground": map[string]any{"cwd": "/", "pid": 78}}, {"turn": 5}, {"status": map[string]any{"app": "pi", "state": "blocked"}}, {"status": map[string]any{"app": "pi", "state": "working"}}, {"status": nil}, {"address": "t3"}, {"state": "finished"}} {
		f := coreSetup(t)
		a := f.agent(t)
		for key, value := range change {
			f.terminal[key] = value
		}
		f.write(t, "get", f.terminal)
		if err := f.h.PromptAgent(context.Background(), "gm", a, "body"); err == nil {
			t.Fatal("changed seat accepted", change)
		}
		if strings.Contains(f.calls(t), "send\x00") {
			t.Fatal("sent after change", change)
		}
	}
}

func TestCoreMalformedSubmissionIsNotConfirmed(t *testing.T) {
	for _, value := range []any{nil, map[string]any{}, map[string]any{"turn": 3, "status": "idle"}, map[string]any{"turn": 5, "status": "working", "error": "oops"}} {
		f := coreSetup(t)
		a := f.agent(t)
		f.write(t, "send", value)
		if err := f.h.PromptAgent(context.Background(), "gm", a, "body"); err == nil {
			t.Fatal("invalid response accepted", value)
		}
	}
}

func TestCoreConfirmationUsesPreSendTurnNotStartedReceipt(t *testing.T) {
	f := coreSetup(t)
	a := f.agent(t)
	f.write(t, "send", map[string]any{"turn": 4, "status": "idle", "started": false})
	if err := f.h.PromptAgent(context.Background(), "gm", a, "body"); err != nil {
		t.Fatal("started=false does not mean paste failed", err)
	}
	for _, bad := range []any{map[string]any{"turn": 4, "status": "idle"}, map[string]any{"turn": 5, "status": "idle", "started": true}, map[string]any{"turn": 4, "status": "done", "started": true}} {
		f.write(t, "send", bad)
		if err := f.h.PromptAgent(context.Background(), "gm", a, "body"); err == nil {
			t.Fatal("ambiguous or changed pre-send snapshot accepted", bad)
		}
	}
}

func TestCoreUnavailableSocketDoesNotInvokeHC(t *testing.T) {
	f := coreSetup(t)
	f.h.Socket = filepath.Join(t.TempDir(), "missing.sock")
	if _, err := f.h.List(context.Background()); err == nil {
		t.Fatal("missing core accepted")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "calls")); !os.IsNotExist(err) {
		t.Fatal("hc invoked against an unavailable core", err)
	}
}

func TestCoreCommandTimeoutIsBounded(t *testing.T) {
	f := coreSetup(t)
	// exec 使逾時只留下受 CommandContext 管理的單一行程。
	if err := os.WriteFile(f.h.Binary, []byte("#!/bin/sh\nexec sleep 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	f.h.Timeout = 30 * time.Millisecond
	start := time.Now()
	if _, err := f.h.List(context.Background()); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout was not bounded")
	}
}

type coreWorkerFake struct{ *fakeHerdr }

func (f coreWorkerFake) PromptAgent(ctx context.Context, _ string, a Agent, body string) error {
	return f.Prompt(ctx, a.Pane, body)
}

func TestCoreUncertainNeverAutoRetriesOrPassesLaterMessages(t *testing.T) {
	s, c := fixture(t)
	c.Backend, c.MaxAttempts = "core", 1
	h := coreWorkerFake{&fakeHerdr{err: errors.New("submission unknown")}}
	w := worker(s, c, h)
	first := enqueue(t, s, "gm", "first")
	a := seat(c, "gm", "idle", "t2", 4)
	a.Backend = "core"
	if err := w.Dispatch(context.Background(), "gm", a); err != nil {
		t.Fatal(err)
	}
	enqueue(t, s, "gm", "later")
	for range 3 {
		if err := w.Dispatch(context.Background(), "gm", a); err != nil {
			t.Fatal(err)
		}
	}
	if h.count() != 1 {
		t.Fatal("uncertain auto retried", h.count())
	}
	message, err := s.Q.GetMessage(context.Background(), first.ID)
	if err != nil || message.State != "uncertain" {
		t.Fatal(message, err)
	}
	last, err := s.Q.LatestBatch(context.Background(), "gm")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resolve(context.Background(), last.ID, "retry", "inspected not delivered"); err != nil {
		t.Fatal(err)
	}
	h.err = nil
	if err := w.Dispatch(context.Background(), "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 {
		t.Fatal("explicit retry did not unblock")
	}
}
