package claudeguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultOnHooksRecordWithoutOptIn(t *testing.T) {
	m := Manager{Dir: filepath.Join(t.TempDir(), "guards")}
	p, err := m.TeamPolicy()
	if err != nil || !p.Enabled {
		t.Fatalf("default: %+v %v", p, err)
	}
	allowed(t, m, false)
	apply(t, m, "SessionStart", "")
	allowed(t, m, true)
	apply(t, m, "PreToolUse", "q1")
	allowed(t, m, false)
	apply(t, m, "PostToolUse", "q1")
	allowed(t, m, true)
	if _, err := os.Stat(filepath.Join(m.Dir, "gm", "events.jsonl")); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidTeamPolicyFailsClosedButRecordingContinues(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `[]`, ``, `broken`, `{"enabled":true}`, `{"version":1}`, `{"version":1,"enabled":null}`,
		`{"version":null,"enabled":false}`, `{"version":2,"enabled":false}`, `{"version":"1","enabled":true}`,
		`{"version":1,"enabled":1}`, `{"version":1,"enabled":true,"extra":false}`,
		`{"version":1,"Enabled":false}`, `{"version":1,"enabled":true,"enabled":false}`,
		`{"version":1,"version":1,"enabled":false}`, `{"version":1,"enabled":false} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			m := setup(t)
			apply(t, m, "SessionStart", "")
			path := filepath.Join(m.Dir, "team-policy.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			allowed(t, m, false)
			apply(t, m, "PreToolUse", "q1")
			if _, _, err := m.Inspect(t.Context(), "gm"); err == nil {
				t.Fatal("invalid policy accepted")
			}
			if err := m.ConfigureTeam(t.Context(), false); err == nil {
				t.Fatal("silently repaired invalid policy")
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != body {
				t.Fatal("policy replaced", err)
			}
		})
	}
}
func TestExplicitVersionedTeamPolicy(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	for _, enabled := range []bool{false, true} {
		if err := m.ConfigureTeam(t.Context(), enabled); err != nil {
			t.Fatal(err)
		}
		p, _, err := m.Inspect(t.Context(), "gm")
		if err != nil || p.Version != 1 || p.Enabled != enabled {
			t.Fatalf("%+v %v", p, err)
		}
		data, _ := json.Marshal(p)
		var again Policy
		if err := json.Unmarshal(data, &again); err != nil || p != again {
			t.Fatal("roundtrip", err)
		}
	}
}
func TestOldRolePoliciesCannotOverrideTeam(t *testing.T) {
	m := setup(t)
	apply(t, m, "SessionStart", "")
	apply(t, m, "PreToolUse", "q1")
	if err := os.WriteFile(filepath.Join(m.Dir, "gm", "policy.json"), []byte(`{"version":1,"enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	allowed(t, m, false)
	if err := m.ConfigureTeam(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	release, enabled, err := m.Acquire(t.Context(), "gm", "wC:p5", "session-1")
	if err != nil || enabled {
		t.Fatal(enabled, err)
	}
	release()
}
func TestGuardSymlinksRejected(t *testing.T) {
	m := setup(t)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(m.Dir, "gm")); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(t.Context(), "gm", "wC:p5", event("SessionStart", "")); err == nil {
		t.Fatal("followed role symlink")
	}
}
