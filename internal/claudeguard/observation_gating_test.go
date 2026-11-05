package claudeguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreWithoutSessionStartBlocksUntilMatchingPost(t *testing.T) {
	m := setup(t)
	allowed(t, m, true)
	apply(t, m, "PreToolUse", "q1")
	allowed(t, m, false)
	apply(t, m, "PreToolUse", "q2")
	apply(t, m, "PostToolUseFailure", "q1")
	allowed(t, m, false)
	apply(t, m, "PostToolUse", "q2")
	allowed(t, m, false)
	if err := m.Apply(t.Context(), "gm", "wC:p5", event("PostToolUse", "unrelated")); err == nil {
		t.Fatal("accepted unrelated completion")
	}
	allowed(t, m, false)
	apply(t, m, "PostToolUse", "q1")
	allowed(t, m, true)
}

func TestHarnessRestartWithSameSessionExcludesOldHooks(t *testing.T) {
	m := setup(t)
	first, err := m.StartRun(t.Context(), "gm", "wC:p5")
	if err != nil {
		t.Fatal(err)
	}
	applyRun := func(run Launch, name, id string) error {
		e := event(name, id)
		e.RunID = run.ID
		return m.Apply(t.Context(), "gm", "wC:p5", e)
	}
	if err := applyRun(first, "PreToolUse", "old-question"); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	second, err := m.StartRun(t.Context(), "gm", "wC:p5")
	if err != nil || second.ID == first.ID || second.StartedAt <= 0 {
		t.Fatal(second, err)
	}
	allowed(t, m, true) // 相同session／pane的舊pending不屬於這次啟動。
	if err := applyRun(second, "PreToolUse", "new-question"); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	for _, name := range []string{"PreToolUse", "PostToolUse", "SessionStart", "SessionEnd"} {
		if err := applyRun(first, name, "old-question"); err == nil {
			t.Fatalf("accepted old %s", name)
		}
		allowed(t, m, false)
	}
	if err := applyRun(second, "SessionStart", ""); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false) // 同世代重複SessionStart不解鎖。
	_, s, err := m.Inspect(t.Context(), "gm")
	if err != nil || s.RunID != second.ID || s.RunStartedAt != second.StartedAt || s.Started["new-question"] <= 0 || len(s.Pending) != 1 {
		t.Fatal(s, err)
	}
	if err := applyRun(second, "PostToolUse", "new-question"); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, true)
}

func TestUnreadableObservationAllowsButPreservesEvidence(t *testing.T) {
	m := setup(t)
	apply(t, m, "PreToolUse", "q")
	file := filepath.Join(m.Dir, "gm", "state.json")
	if err := os.WriteFile(file, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	warnings := 0
	m.OnUnavailable = func(error) { warnings++ }
	allowed(t, m, true)
	if warnings != 1 {
		t.Fatal("missing diagnostic", warnings)
	}
	if got, _ := os.ReadFile(file); string(got) != "broken" {
		t.Fatal("rewrote unavailable evidence")
	}
}

func TestRunIdentityWinsOverWallClock(t *testing.T) {
	m := setup(t)
	launch, err := m.StartRun(t.Context(), "gm", "wC:p5")
	if err != nil {
		t.Fatal(err)
	}
	e := event("PreToolUse", "q")
	e.RunID = launch.ID
	if err := m.Apply(t.Context(), "gm", "wC:p5", e); err != nil {
		t.Fatal(err)
	}
	_, s, err := m.Inspect(t.Context(), "gm")
	if err != nil {
		t.Fatal(err)
	}
	s.Started["q"] = launch.StartedAt - 1000 // 模擬紀錄期間時鐘回撥，不可當成重啟。
	if err := writeJSON(filepath.Join(m.Dir, "gm"), "state.json", s); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
}
