package claudeguard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordingWhileOffAndPersistentTimes(t *testing.T) {
	m := setup(t)
	if err := m.ConfigureTeam(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	apply(t, m, "SessionStart", "")
	for i := 0; i < 25; i++ {
		id := fmt.Sprint(i)
		apply(t, m, "PreToolUse", id)
		apply(t, m, "PostToolUse", id)
	}
	_, s, err := m.Inspect(t.Context(), "gm")
	if err != nil || len(s.Recent) != 20 {
		t.Fatal(s, err)
	}
	for _, q := range s.Recent {
		if q.StartedAt <= 0 || q.EndedAt < q.StartedAt || q.DurationMS != q.EndedAt-q.StartedAt || q.Outcome != "completed" {
			t.Fatal(q)
		}
	}
	f, err := os.Open(filepath.Join(m.Dir, "gm", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	n := 0
	for scan.Scan() {
		var e HistoryEvent
		if err := json.Unmarshal(scan.Bytes(), &e); err != nil || e.At <= 0 {
			t.Fatal(e, err)
		}
		n++
	}
	if scan.Err() != nil || n != 51 {
		t.Fatal(n, scan.Err())
	}
	if err := m.ConfigureTeam(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, true)
	apply(t, m, "PreToolUse", "open")
	if err := m.ConfigureTeam(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if err := m.ConfigureTeam(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
}
func TestInterruptedQuestionHasNoInventedEnd(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	apply(t, m, "PreToolUse", "q1")
	e := event("SessionStart", "")
	e.Session = "new-session"
	if err := m.Apply(t.Context(), "gm", "wC:p5", e); err != nil {
		t.Fatal(err)
	}
	_, s, err := m.Inspect(t.Context(), "gm")
	if err != nil || len(s.Recent) != 1 {
		t.Fatal(s, err)
	}
	q := s.Recent[0]
	if q.Outcome != "interrupted" || q.EndedAt != 0 || q.DurationMS != 0 {
		t.Fatal(q)
	}
}
func TestTeamSwitchWaitsForInFlightSubmission(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	release, _, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err = m.ConfigureTeam(ctx, false)
	release()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("switch raced with submission", err)
	}
	p, err := m.TeamPolicy()
	if err != nil || !p.Enabled {
		t.Fatal(p, err)
	}
}
func TestQuestionTextAndAnswersAreNotPersisted(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	for _, name := range []string{"PreToolUse", "PostToolUse"} {
		e, err := Decode(strings.NewReader(`{"hook_event_name":"` + name + `","session_id":"session-1","tool_name":"AskUserQuestion","tool_use_id":"q","tool_input":{"question":"PRIVATE-QUESTION"},"tool_response":{"answer":"PRIVATE-ANSWER"}}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Apply(t.Context(), "gm", "wC:p5", e); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"state.json", "events.jsonl"} {
		b, err := os.ReadFile(filepath.Join(m.Dir, "gm", name))
		if err != nil || strings.Contains(string(b), "PRIVATE-") {
			t.Fatal(name, err)
		}
	}
}

func TestHistorySymlinkAndNonregularRejected(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	path := filepath.Join(m.Dir, "gm", "events.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "victim"), path); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(t.Context(), "gm", "wC:p5", event("PreToolUse", "q")); err == nil {
		t.Fatal("followed history symlink")
	}
}
