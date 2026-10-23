package claudeguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUnconfiguredHooksAreNoOp(t *testing.T) {
	for _, parentExists := range []bool{false, true} {
		m := Manager{Dir: filepath.Join(t.TempDir(), "guards")}
		if parentExists {
			if err := os.Mkdir(m.Dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"SessionStart", "PreToolUse", "PostToolUse", "PostToolUseFailure", "SessionEnd"} {
			if err := m.Apply(t.Context(), "gm", "wC:p5", event(name, "q1")); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		release, enabled, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-1")
		if err != nil || enabled || release == nil {
			t.Fatalf("unconfigured acquisition: enabled=%v err=%v", enabled, err)
		}
		release()
		if _, err := os.Lstat(filepath.Join(m.Dir, "gm")); !os.IsNotExist(err) {
			t.Fatalf("no-op created role state: %v", err)
		}
		if !parentExists {
			if _, err := os.Lstat(m.Dir); !os.IsNotExist(err) {
				t.Fatalf("no-op created guard directory: %v", err)
			}
		}
	}
}

func TestExistingPolicyDirectoryCannotSilentlyOptOut(t *testing.T) {
	for _, kind := range []string{"missing-policy", "dangling-role-link"} {
		t.Run(kind, func(t *testing.T) {
			m := Manager{Dir: t.TempDir()}
			dir := filepath.Join(m.Dir, "gm")
			var err error
			if kind == "missing-policy" {
				err = os.Mkdir(dir, 0700)
			} else {
				err = os.Symlink(filepath.Join(m.Dir, "missing-target"), dir)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Apply(t.Context(), "gm", "wC:p5", event("SessionStart", "")); err == nil {
				t.Fatal("hook silently opted out")
			}
			allowed(t, m, false)
			if _, _, err := m.Inspect(t.Context(), "gm"); err == nil {
				t.Fatal("status accepted missing policy")
			}
		})
	}
}

func TestInvalidPolicyFailsClosedOnEveryRead(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`, ``, `broken`,
		`{"enabled":true}`, `{"version":1}`, `{"enable":true}`,
		`{"version":1,"enable":true}`, `{"version":1,"enabled":null}`,
		`{"version":null,"enabled":false}`, `{"version":0,"enabled":false}`,
		`{"version":2,"enabled":false}`, `{"version":"1","enabled":true}`,
		`{"version":1,"enabled":1}`, `{"version":1,"enabled":"false"}`,
		`{"version":1,"enabled":true,"extra":false}`,
		`{"version":1,"Enabled":false}`,
		`{"version":1,"enabled":true,"enabled":false}`,
		`{"version":1,"version":1,"enabled":false}`,
		`{"version":1,"enabled":false} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			m := setup(t)
			apply(t, m, "SessionStart", "")
			policyPath := filepath.Join(m.Dir, "gm", "policy.json")
			statePath := filepath.Join(m.Dir, "gm", "state.json")
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(policyPath, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			allowed(t, m, false)
			if err := m.Apply(t.Context(), "gm", "wC:p5", event("SessionStart", "")); err == nil {
				t.Fatal("hook accepted invalid policy")
			}
			if _, _, err := m.Inspect(t.Context(), "gm"); err == nil {
				t.Fatal("status accepted invalid policy")
			}
			if err := m.Configure(t.Context(), "gm", true); err == nil {
				t.Fatal("enable silently repaired invalid policy")
			}
			after, err := os.ReadFile(statePath)
			if err != nil || string(before) != string(after) {
				t.Fatalf("invalid policy changed state: %v", err)
			}
			if got, err := os.ReadFile(policyPath); err != nil || string(got) != body {
				t.Fatalf("invalid policy silently replaced: %v", err)
			}
		})
	}
}

func TestExplicitVersionedPolicyAndDisableRecovery(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		m := setup(t)
		if err := m.Configure(t.Context(), "gm", enabled); err != nil {
			t.Fatal(err)
		}
		p, _, err := m.Inspect(t.Context(), "gm")
		if err != nil || p.Version != 1 || p.Enabled != enabled {
			t.Fatalf("policy=%+v err=%v", p, err)
		}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip Policy
		if err := json.Unmarshal(data, &roundtrip); err != nil || roundtrip != p {
			t.Fatalf("roundtrip=%+v err=%v", roundtrip, err)
		}
	}
	m := setup(t)
	if err := os.WriteFile(filepath.Join(m.Dir, "gm", "policy.json"), []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	// 修復舊原型政策必須明確停用再啟用，不自動將損毀資料視為安全。
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
