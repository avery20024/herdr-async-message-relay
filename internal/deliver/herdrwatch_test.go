package deliver

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "herdr.sock")
}

func TestWatchHerdrFiresOnlyAfterConsecutiveMisses(t *testing.T) {
	socket := shortSocket(t)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var fired atomic.Bool
	done := make(chan struct{})
	go func() {
		WatchHerdr(context.Background(), socket, 10*time.Millisecond, 3, func() { fired.Store(true) })
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	if fired.Load() {
		t.Fatal("fired while Herdr socket was alive")
	}
	listener.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop after socket disappeared")
	}
	if !fired.Load() {
		t.Fatal("gone callback was not called")
	}
}

func TestWatchHerdrRecoveryResetsMissCount(t *testing.T) {
	socket := shortSocket(t)
	var fired atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go WatchHerdr(ctx, socket, 10*time.Millisecond, 20, func() { fired.Store(true) })
	time.Sleep(60 * time.Millisecond)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	listener.Close()
	time.Sleep(60 * time.Millisecond)
	if fired.Load() {
		t.Fatal("miss count was not reset by recovery")
	}
}

func TestWatchHerdrStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		WatchHerdr(ctx, shortSocket(t), time.Hour, 1, func() { t.Error("unexpected callback") })
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher ignored context cancellation")
	}
}
