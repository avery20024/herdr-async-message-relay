package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/deliver"
)

func TestRestartInitialRunningAndPausedServices(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			f := setup(t, platform)
			for range 3 {
				if err := f.m.Restart(context.Background(), f.c); err != nil {
					t.Fatal(err)
				}
				if !f.ready.Load() {
					t.Fatal("restart did not become ready")
				}
				if err := f.m.Stop(context.Background(), f.c); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.m.Ensure(context.Background(), f.c, true); err != nil {
				t.Fatal(err)
			}
			if err := f.m.Restart(context.Background(), f.c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestartDoesNotKillUnmanagedDaemon(t *testing.T) {
	f := setup(t, "darwin")
	f.ready.Store(true)
	if err := f.m.Restart(context.Background(), f.c); err == nil || len(f.calls) != 0 {
		t.Fatal("must not restart an unregistered listener", err)
	}
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Restart(context.Background(), f.c); err == nil || len(f.calls) != 0 {
		t.Fatal("must not restart a registered legacy listener", err)
	}
}

func TestRestartFailureDoesNotLaunchReplacement(t *testing.T) {
	for _, kind := range []string{"stop fails", "listener survives", "daemon lock held"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t, "darwin")
			if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "stop fails":
				f.fail = "bootout"
			case "listener survives":
				f.m.probe = func(context.Context, deliver.Config) (bool, error) { return true, nil }
			case "daemon lock held":
				if err := os.MkdirAll(f.c.StateDir, 0700); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(filepath.Join(f.c.StateDir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
					t.Fatal(err)
				}
				defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			}
			before := strings.Count(strings.Join(f.calls, "\n"), "bootstrap")
			if err := f.m.Restart(context.Background(), f.c); err == nil {
				t.Fatal("restart should fail")
			}
			if strings.Count(strings.Join(f.calls, "\n"), "bootstrap") != before {
				t.Fatal("replacement started without confirmed shutdown")
			}
			r, err := f.m.load(f.c)
			if err != nil || r.AutoStart {
				t.Fatal("failed shutdown must remain paused", err)
			}
		})
	}
}

func TestRestartHoldsLifecycleLockAcrossStopAndStart(t *testing.T) {
	f := setup(t, "darwin")
	if err := f.m.Ensure(context.Background(), f.c, false); err != nil {
		t.Fatal(err)
	}
	original := f.m.run
	entered, release := make(chan struct{}), make(chan struct{})
	f.m.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if args[0] == "bootout" {
			close(entered)
			<-release
		}
		return original(ctx, name, args...)
	}
	done := make(chan error, 1)
	go func() { done <- f.m.Restart(context.Background(), f.c) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := f.m.Ensure(ctx, f.c, false)
	close(release)
	if restartErr := <-done; restartErr != nil {
		t.Fatal(restartErr)
	}
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatal("ensure must not interleave the restart transaction", err)
	}
}

func TestLaunchdBootoutOnlyAcceptsExplicitNoSuchProcess(t *testing.T) {
	f := setup(t, "darwin")
	r, err := f.m.newRecord(f.c)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"3", "5"} {
		exitErr := exec.Command("/bin/sh", "-c", "exit "+code).Run()
		f.m.run = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "bootout" {
				return nil, exitErr
			}
			return nil, nil
		}
		err := f.m.stop(context.Background(), r)
		if (err == nil) != (code == "3") {
			t.Fatal("only ESRCH can mean already absent", code, err)
		}
	}
}
