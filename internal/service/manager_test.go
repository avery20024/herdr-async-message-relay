package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

type fixture struct {
	m      *Manager
	c      deliver.Config
	ready  atomic.Bool
	loaded atomic.Bool
	mu     sync.Mutex
	calls  []string
	fail   string
}

func setup(t *testing.T, platform string) *fixture {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "hsvc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err = os.MkdirAll(filepath.Join(root, "角色", "dev"), 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "source-hrelay")
	if err = os.WriteFile(binary, []byte("test executable snapshot"), 0700); err != nil {
		t.Fatal(err)
	}
	f := &fixture{c: deliver.Config{Root: root, StateDir: filepath.Join(root, "queue"), Herdr: binary,
		BatchSize: 3, BatchBytes: 12000, Poll: 500 * time.Millisecond, CommandTimeout: 10 * time.Second}}
	f.m = &Manager{Base: filepath.Join(root, "registry"), Home: filepath.Join(root, "home"),
		Platform: platform, UID: 501, Executable: binary, wait: 30 * time.Millisecond,
		Environment: []string{"HOME=" + filepath.Join(root, "home"), "PATH=/usr/bin:/bin", "HERDR_SOCKET_PATH=/test/herdr.sock", "SECRET_TOKEN=not-for-services"}}
	f.m.probe = func(context.Context, deliver.Config) (bool, error) { return f.ready.Load(), nil }
	f.m.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		call := name + " " + strings.Join(args, " ")
		f.calls = append(f.calls, call)
		if f.fail != "" && strings.Contains(call, f.fail) {
			return nil, errors.New("fake service manager failure")
		}
		if name == "launchctl" && args[0] == "print" && !f.loaded.Load() {
			return nil, errors.New("not loaded")
		}
		if strings.Contains(call, "bootstrap") || strings.Contains(call, "kickstart") || strings.Contains(call, "--user enable --now") {
			f.loaded.Store(true)
			f.ready.Store(true)
		}
		if strings.Contains(call, "bootout") || strings.Contains(call, "--user disable --now") {
			f.loaded.Store(false)
			f.ready.Store(false)
		}
		return []byte("ok"), nil
	}
	return f
}

func TestEnsureSupervisedServicesAndPrivateStableBinary(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			f := setup(t, platform)
			if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
				t.Fatal(err)
			}
			r, err := f.m.load(f.c)
			if err != nil || !r.Managed || !r.AutoStart {
				t.Fatalf("record=%+v error=%v", r, err)
			}
			if r.Binary == f.m.Executable || !strings.HasPrefix(r.Binary, f.m.Base+"/") {
				t.Fatal("service must not depend on managed plugin checkout")
			}
			body, err := os.ReadFile(r.Binary)
			if err != nil || string(body) != "test executable snapshot" {
				t.Fatalf("binary snapshot: %q %v", body, err)
			}
			if _, ok := r.Environment["SECRET_TOKEN"]; ok {
				t.Fatal("unrelated secrets must not be persisted")
			}
			for path, mode := range map[string]os.FileMode{f.m.Base: 0700, f.m.dir(f.c): 0700, f.m.recordPath(f.c): 0600, r.Binary: 0700} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("private permissions %s: %v", path, err)
				}
			}
			before := len(f.calls)
			if err = f.m.Ensure(context.Background(), f.c, false); err != nil || len(f.calls) != before {
				t.Fatalf("healthy service must not restart: %v %v", err, f.calls)
			}
		})
	}
}

func TestLegacyDaemonReusedWithoutManagerOrStop(t *testing.T) {
	f := setup(t, "darwin")
	f.ready.Store(true)
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	r, err := f.m.load(f.c)
	if err != nil || r.Managed || len(f.calls) != 0 || r.Binary != "" {
		t.Fatalf("legacy takeover: %+v %v %v", r, err, f.calls)
	}
	if err := f.m.Stop(context.Background(), f.c); err == nil || len(f.calls) != 0 {
		t.Fatal("must not terminate a foreground or unknown daemon")
	}
	// 舊服務日後停止，已登記專案才由受管理服務接續；不重建 SQLite。
	f.ready.Store(false)
	if err := f.m.Restore(context.Background()); err != nil || !f.ready.Load() {
		t.Fatalf("restore after legacy exit: %v", err)
	}
}

func TestStopPersistsPauseAndOnlyExplicitStartResumes(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			f := setup(t, platform)
			if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
				t.Fatal(err)
			}
			if err := f.m.Stop(context.Background(), f.c); err != nil {
				t.Fatal(err)
			}
			calls := strings.Join(f.calls, "\n")
			if !strings.Contains(calls, "disable") || (platform == "darwin" && !strings.Contains(calls, "bootout")) {
				t.Fatal("pause must survive login, not just terminate current process", calls)
			}
			before := len(f.calls)
			if err := f.m.Ensure(context.Background(), f.c, false); err == nil || !strings.Contains(err.Error(), "paused") {
				t.Fatalf("must respect manual pause: %v", err)
			}
			if err := f.m.Restore(context.Background()); err != nil || len(f.calls) != before {
				t.Fatalf("startup must not resume paused projects: %v", err)
			}
			if err := f.m.Ensure(context.Background(), f.c, true); err != nil || !f.ready.Load() {
				t.Fatalf("explicit resume failed: %v", err)
			}
		})
	}
}

func TestFailedStopRemainsPausedUntilExplicitNonDisruptiveResume(t *testing.T) {
	f := setup(t, "darwin")
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	f.fail = "bootout"
	if err := f.m.Stop(context.Background(), f.c); err == nil || !f.ready.Load() {
		t.Fatal("a failed stop must not be reported as stopped")
	}
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil {
		t.Fatal("normal ensure must respect pause even if the old process survived")
	}
	f.fail = ""
	before := len(f.calls)
	if err := f.m.Ensure(context.Background(), f.c, true); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != before+1 || !strings.Contains(f.calls[before], "launchctl enable") {
		t.Fatal("explicit resume must re-enable login without restarting a live process", f.calls[before:])
	}
}

func TestConcurrentEnsureStartsOnce(t *testing.T) {
	f := setup(t, "darwin")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if strings.Count(strings.Join(f.calls, "\n"), "bootstrap") != 1 {
		t.Fatal("duplicate service starts", f.calls)
	}
}

func TestContextMismatchAndUnhealthyResponseNeverTakeOver(t *testing.T) {
	f := setup(t, "darwin")
	f.ready.Store(true)
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	f.m.Environment = []string{"HERDR_SOCKET_PATH=/another/herdr.sock"}
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil {
		t.Fatal("different Herdr server must not take over")
	}
	if err := f.m.Restore(context.Background()); err != nil || len(f.calls) != 0 {
		t.Fatal("startup must skip projects belonging to another Herdr server", err)
	}
	f.m.Environment = []string{"HERDR_SOCKET_PATH=/test/herdr.sock"}
	f.m.probe = func(context.Context, deliver.Config) (bool, error) { return false, errors.New("HTTP 500") }
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil || len(f.calls) != 0 {
		t.Fatal("unhealthy existing responder must not trigger takeover")
	}
}

func TestExplicitRestartAppliesSettingsButNormalEnsureDoesNot(t *testing.T) {
	f := setup(t, "linux")
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	changed := f.c
	changed.BatchSize = 2
	if err := f.m.Ensure(context.Background(), changed, false); err != nil {
		t.Fatal(err)
	}
	r, err := f.m.load(f.c)
	if err != nil || r.Config.BatchSize != 3 {
		t.Fatalf("normal send must preserve registered settings: %+v %v", r, err)
	}
	if err := f.m.Stop(context.Background(), f.c); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Ensure(context.Background(), changed, true); err != nil {
		t.Fatal(err)
	}
	r, err = f.m.load(f.c)
	if err != nil || r.Config.BatchSize != 2 || r.Environment["DELIVER_BATCH_SIZE"] != "2" {
		t.Fatalf("explicit restart did not update settings: %+v %v", r, err)
	}
}

func TestFirstRegistrationNeedsHerdrContextAndLiveProject(t *testing.T) {
	f := setup(t, "darwin")
	f.m.Environment = nil
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil || !strings.Contains(err.Error(), "HERDR_SOCKET_PATH") {
		t.Fatalf("must not guess Herdr context: %v", err)
	}
	f.m.Environment = []string{"HERDR_SOCKET_PATH=/test/herdr.sock"}
	if err := os.RemoveAll(filepath.Join(f.c.Root, "角色")); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil || len(f.calls) != 0 {
		t.Fatal("removed project must not install a restart-looping service")
	}
}

func TestRestoreContinuesPastAnotherProjectFailure(t *testing.T) {
	f, other := setup(t, "darwin"), setup(t, "darwin")
	f.ready.Store(true)
	for _, c := range []deliver.Config{f.c, other.c} {
		if err := f.m.Ensure(context.Background(), c, false); err != nil {
			t.Fatal(err)
		}
	}
	f.ready.Store(false)
	f.m.probe = func(_ context.Context, c deliver.Config) (bool, error) {
		if c.Root == f.c.Root {
			return false, errors.New("one project is unhealthy")
		}
		return f.ready.Load(), nil
	}
	if err := f.m.Restore(context.Background()); err == nil {
		t.Fatal("failure must still be reported")
	}
	r, err := f.m.load(other.c)
	if err != nil || !r.Managed || !f.ready.Load() {
		t.Fatalf("other project did not start: %+v %v", r, err)
	}
}

func TestManagerFailureAndReadinessTimeout(t *testing.T) {
	f := setup(t, "linux")
	f.fail = "daemon-reload"
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil {
		t.Fatal("service manager failure must propagate")
	}
	f.fail = ""
	f.m.probe = func(context.Context, deliver.Config) (bool, error) { return false, nil }
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("expected bounded readiness wait: %v", err)
	}
}

func TestLifecycleLockHonorsCancellation(t *testing.T) {
	f := setup(t, "darwin")
	unlock, err := f.m.lock(context.Background(), f.c)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := f.m.Ensure(ctx, f.c, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock cancellation: %v", err)
	}
}

func TestRenderEscapingAndRejectControlCharacters(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		f := setup(t, platform)
		r, err := f.m.newRecord(f.c)
		if err != nil {
			t.Fatal(err)
		}
		r.Config.Root += " space<&\"%$"
		r.Binary = "/stable path/hrelay"
		_, body, err := f.m.render(r)
		if err != nil {
			t.Fatal(err)
		}
		if platform == "darwin" {
			decoder := xml.NewDecoder(strings.NewReader(string(body)))
			for {
				_, err := decoder.Token()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal("invalid launchd XML", err)
				}
			}
		} else if !strings.Contains(string(body), `space<&\"%%$$`) || !strings.Contains(string(body), `ExecStart="/stable path/hrelay"`) {
			t.Fatal("systemd quoting did not preserve literal argument", string(body))
		}
		r.Environment["PATH"] = "/bin\nExecStart=malicious"
		if _, _, err := f.m.render(r); err == nil {
			t.Fatal("must reject newline injection")
		}
	}
}

func TestRegistrySymlinkAndCorruptionRejected(t *testing.T) {
	f := setup(t, "darwin")
	if err := os.Symlink(f.m.Home, f.m.Base); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Ensure(context.Background(), f.c, false); err == nil {
		t.Fatal("registry symlink must be rejected")
	}
	if err := os.Remove(f.m.Base); err != nil {
		t.Fatal(err)
	}
	f.ready.Store(true)
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.m.recordPath(f.c), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Restore(context.Background()); err == nil || len(f.calls) != 0 {
		t.Fatal("invalid registry must not launch a guessed service")
	}
}

func TestProbeUnixSocketHealthAndBinding(t *testing.T) {
	f := setup(t, "darwin")
	ready, err := probe(context.Background(), f.c)
	if ready || err != nil {
		t.Fatalf("missing socket should allow startup: %v %v", ready, err)
	}
	if err = os.MkdirAll(f.c.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", f.c.Socket())
	if err != nil {
		t.Fatal(err)
	}
	var wrong atomic.Bool
	var broken atomic.Bool
	server := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			http.Error(w, "broken", 500)
			return
		}
		root := f.c.Root
		if wrong.Load() {
			root += "/other"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"project_root": root})
	})}
	done := make(chan struct{})
	go func() { _ = server.Serve(listener); close(done) }()
	t.Cleanup(func() { _ = server.Close(); <-done })
	if ready, err := probe(context.Background(), f.c); !ready || err != nil {
		t.Fatalf("healthy socket: %v %v", ready, err)
	}
	wrong.Store(true)
	if _, err := probe(context.Background(), f.c); err == nil {
		t.Fatal("wrong project binding accepted")
	}
	broken.Store(true)
	if _, err := probe(context.Background(), f.c); err == nil {
		t.Fatal("HTTP failure must not be treated as absent daemon")
	}
}
