package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func register(t *testing.T, f *fixture) string {
	t.Helper()
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	return f.m.ID(f.c)
}

func TestRenderExitsGracefullyWithHerdrAndRestartsOnlyOnFailure(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		f := setup(t, platform)
		_ = register(t, f)
		r, err := f.m.load(f.c)
		if err != nil {
			t.Fatal(err)
		}
		_, body, err := f.m.render(r)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, deliver.ExitWhenHerdrGoneEnv) {
			t.Fatalf("%s: unit lacks the Herdr watcher flag:\n%s", platform, text)
		}
		if platform == "darwin" && (!strings.Contains(text, "<key>SuccessfulExit</key><false/>") || strings.Contains(text, "<key>KeepAlive</key><true/>")) {
			t.Fatalf("launchd must restart only after unsuccessful exit:\n%s", text)
		}
		if platform == "linux" && (!strings.Contains(text, "Restart=on-failure") || strings.Contains(text, "Restart=always")) {
			t.Fatalf("systemd must restart only on failure:\n%s", text)
		}
		if _, ok := r.Environment[deliver.ExitWhenHerdrGoneEnv]; ok {
			t.Fatal("watcher flag must be added at render time, not stored in the record")
		}
	}
}

func TestInstancesListAllAndResolveIDPrefix(t *testing.T) {
	f := setup(t, "darwin")
	id := register(t, f)
	f.m.Environment = []string{"HOME=" + f.m.Home, "HERDR_SOCKET_PATH=/other/herdr.sock"}
	list, err := f.m.Instances(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != id || !list[0].Ready {
		t.Fatalf("ps must include instances from another Herdr context: %+v %v", list, err)
	}
	if _, err = f.m.find(id[:4]); err != nil {
		t.Fatal(err)
	}
	if _, err = f.m.find("zzzz"); err == nil {
		t.Fatal("unknown id accepted")
	}
	if _, err = f.m.find(id[:3]); err == nil {
		t.Fatal("too-short id accepted")
	}
}

func TestStopAndStartByIDKeepRegistration(t *testing.T) {
	f := setup(t, "darwin")
	id := register(t, f)
	if got, err := f.m.StopByID(context.Background(), id); err != nil || got != id {
		t.Fatalf("stop = %q %v", got, err)
	}
	r, err := f.m.load(f.c)
	if err != nil || r.AutoStart || f.ready.Load() {
		t.Fatalf("stop must pause and keep the record: %+v ready=%v %v", r, f.ready.Load(), err)
	}
	if _, err = f.m.StartByID(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if r, err = f.m.load(f.c); err != nil || !r.AutoStart || !f.ready.Load() {
		t.Fatalf("start must resume: %+v ready=%v %v", r, f.ready.Load(), err)
	}
}

func TestRemoveByIDKeepsQueueUnlessPurged(t *testing.T) {
	for _, purge := range []bool{false, true} {
		f := setup(t, "darwin")
		id := register(t, f)
		if err := os.MkdirAll(f.c.StateDir, 0700); err != nil {
			t.Fatal(err)
		}
		queue := filepath.Join(f.c.StateDir, "queue.db")
		other := filepath.Join(f.c.StateDir, "claude-guard.json")
		for _, p := range []string{queue, other} {
			if err := os.WriteFile(p, []byte("data"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		r, _ := f.m.load(f.c)
		unit, _, _ := f.m.render(r)
		if _, err := os.Stat(unit); err != nil {
			t.Fatalf("unit missing before rm: %v", err)
		}
		if got, err := f.m.RemoveByID(context.Background(), id, purge); err != nil || got != id {
			t.Fatalf("rm purge=%v = %q %v", purge, got, err)
		}
		if _, err := os.Stat(unit); !os.IsNotExist(err) {
			t.Fatalf("unit not removed: %v", err)
		}
		if _, err := f.m.load(f.c); !os.IsNotExist(err) {
			t.Fatalf("registration not removed: %v", err)
		}
		_, queueErr := os.Stat(queue)
		if purge == (queueErr == nil) {
			t.Fatalf("purge=%v but queue existence err=%v", purge, queueErr)
		}
		if _, err := os.Stat(other); err != nil {
			t.Fatalf("unrelated state file must be kept: %v", err)
		}
	}
}

func TestRemoveRefusesRunningUnmanagedRelay(t *testing.T) {
	f := setup(t, "darwin")
	f.ready.Store(true)
	id := register(t, f)
	if _, err := f.m.RemoveByID(context.Background(), id, false); err == nil {
		t.Fatal("running unmanaged relay was removed")
	}
	if _, err := f.m.load(f.c); err != nil {
		t.Fatalf("registration lost after refused rm: %v", err)
	}
}

func TestSameRootWithAnotherStateDirIsRefused(t *testing.T) {
	f := setup(t, "darwin")
	_ = register(t, f)
	second := f.c
	second.StateDir = filepath.Join(f.c.Root, "queue-two")
	err := f.m.Ensure(context.Background(), second, false)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("second state dir for one root must be refused, got %v", err)
	}
	other := f.c
	short, mkErr := os.MkdirTemp("/tmp", "hsvc2-")
	if mkErr != nil {
		t.Fatal(mkErr)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	other.Root = short
	other.StateDir = filepath.Join(other.Root, "queue")
	if err = f.m.Ensure(context.Background(), other, false); err != nil {
		t.Fatalf("different roots must stay independent: %v", err)
	}
}
