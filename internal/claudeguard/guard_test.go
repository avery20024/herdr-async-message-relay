package claudeguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) Manager {
	t.Helper()
	m := Manager{Dir: t.TempDir()}
	if err := m.Configure(t.Context(), "gm", true); err != nil {
		t.Fatal(err)
	}
	return m
}
func event(name, id string) Event {
	return Event{Name: name, Session: "session-1", Tool: "AskUserQuestion", ToolID: id}
}
func apply(t *testing.T, m Manager, name, id string) {
	t.Helper()
	if err := m.Apply(t.Context(), "gm", "wC:p5", event(name, id)); err != nil {
		t.Fatal(err)
	}
}
func allowed(t *testing.T, m Manager, want bool) {
	t.Helper()
	release, enabled, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-1")
	if release != nil {
		release()
	}
	if !enabled || (err == nil) != want {
		t.Fatalf("enabled=%v err=%v want=%v", enabled, err, want)
	}
}
func TestQuestionLifecycle(t *testing.T) {
	m := setup(t)
	allowed(t, m, false)
	apply(t, m, "SessionStart", "")
	allowed(t, m, true)
	apply(t, m, "PreToolUse", "q1")
	apply(t, m, "PreToolUse", "q2")
	allowed(t, m, false)
	apply(t, m, "PostToolUse", "q1")
	allowed(t, m, false)
	apply(t, m, "SessionStart", "")
	allowed(t, m, false)
	apply(t, m, "PostToolUse", "q2")
	allowed(t, m, true)
	apply(t, m, "SessionEnd", "")
	allowed(t, m, false)
}
func TestFailureAndMissingCompletionStayBlocked(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	apply(t, m, "PreToolUse", "q1")
	apply(t, m, "PostToolUseFailure", "q1")
	apply(t, m, "PostToolUse", "q1")
	allowed(t, m, false)
	apply(t, m, "SessionStart", "")
	allowed(t, m, false)
	// 只有新的 session 初始化可清除舊 unknown，沒有逾時放行。
	e := event("SessionStart", "")
	e.Session = "session-2"
	if err := m.Apply(t.Context(), "gm", "wC:p5", e); err != nil {
		t.Fatal(err)
	}
	release, _, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-2")
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestStaleEventsCannotUnlock(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	apply(t, m, "PreToolUse", "q1")
	for _, e := range []Event{event("PostToolUse", "wrong"), {Name: "PostToolUse", Session: "old", Tool: "AskUserQuestion", ToolID: "q1"}} {
		if err := m.Apply(t.Context(), "gm", "wC:p5", e); err == nil {
			t.Fatal("accepted stale event")
		}
	}
	allowed(t, m, false)
}
func TestIdentityAndCorruptionFailClosed(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	for _, v := range [][2]string{{"wC:p6", "session-1"}, {"wC:p5", "session-2"}, {"wC:p5", ""}} {
		release, _, err := m.Acquire(t.Context(), "gm", v[0], v[1])
		if release != nil {
			release()
		}
		if err == nil {
			t.Fatal("identity mismatch allowed")
		}
	}
	if err := os.WriteFile(filepath.Join(m.Dir, "gm", "state.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	if err := m.Apply(t.Context(), "gm", "wC:p5", event("SessionStart", "")); err == nil {
		t.Fatal("corruption reset")
	}
}
func TestOptInAndDisable(t *testing.T) {
	m := Manager{Dir: t.TempDir()}
	release, enabled, err := m.Acquire(t.Context(), "gm", "wC:p5", "")
	if err != nil || enabled {
		t.Fatalf("%v %v", enabled, err)
	}
	release()
	if err := m.Configure(t.Context(), "gm", true); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	if err := m.Configure(t.Context(), "gm", false); err != nil {
		t.Fatal(err)
	}
	release, enabled, err = m.Acquire(t.Context(), "gm", "wC:p5", "")
	if err != nil || enabled {
		t.Fatalf("%v %v", enabled, err)
	}
	release()
}
func TestSubmissionLockSerializesPreHook(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	release, _, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		done <- m.Apply(ctx, "gm", "wC:p5", event("PreToolUse", "q1"))
	}()
	err = <-done
	release()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hook ran during submission: %v", err)
	}
	apply(t, m, "PreToolUse", "q1")
	allowed(t, m, false)
}
func TestBadInput(t *testing.T) {
	for _, body := range []string{"{} garbage", strings.Repeat("x", 1024*1024+1)} {
		if _, err := Decode(strings.NewReader(body)); err == nil {
			t.Fatal("accepted bad JSON")
		}
	}
	m := setup(t)
	if err := m.Configure(t.Context(), "../escape", true); err == nil {
		t.Fatal("path traversal")
	}
	for _, e := range []Event{{Name: "Notification", Session: "s"}, {Name: "PreToolUse", Session: "s", Tool: "Bash", ToolID: "t"}, {Name: "PreToolUse", Session: "s", Tool: "AskUserQuestion"}} {
		if err := m.Apply(t.Context(), "gm", "wC:p5", e); err == nil {
			t.Fatal("accepted unsupported event")
		}
	}
}
func TestMissingStateAndReenableAreNotReady(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	if err := os.Remove(filepath.Join(m.Dir, "gm", "state.json")); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	apply(t, m, "SessionStart", "")
	if err := m.Configure(t.Context(), "gm", false); err != nil {
		t.Fatal(err)
	}
	if err := m.Configure(t.Context(), "gm", true); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	apply(t, m, "SessionStart", "")
	allowed(t, m, true)
}

func TestReenableDoesNotResetPending(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	apply(t, m, "PreToolUse", "q1")
	if err := m.Configure(t.Context(), "gm", true); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
}
