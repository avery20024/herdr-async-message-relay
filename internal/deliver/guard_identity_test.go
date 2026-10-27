package deliver

import (
	"path/filepath"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

func TestTeamGuardUnknownIdentityDoesNotBypassObservedClaude(t *testing.T) {
	for _, kind := range []string{"", "pi"} {
		t.Run(kind, func(t *testing.T) {
			s, c := fixture(t)
			m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
			if err := m.ConfigureTeam(t.Context(), true); err != nil {
				t.Fatal(err)
			}
			if err := m.Apply(t.Context(), "gm", "wC:p5", claudeguard.Event{Name: "SessionStart", Session: "s"}); err != nil {
				t.Fatal(err)
			}
			if err := m.Apply(t.Context(), "gm", "wC:p5", claudeguard.Event{Name: "PreToolUse", Session: "s", Tool: "AskUserQuestion", ToolID: "q"}); err != nil {
				t.Fatal(err)
			}
			a := seat(c, "gm", "idle", "wC:p5", 1)
			a.Session = AgentSession{Agent: kind, Kind: "id", Value: "other"}
			h := &fakeHerdr{agents: []Agent{a}}
			msg := enqueue(t, s, "gm", "body")
			if err := worker(s, c, h).Dispatch(t.Context(), "gm", a); err != nil {
				t.Fatal(err)
			}
			got, _ := s.Q.GetMessage(t.Context(), msg.ID)
			want := "queued"
			if kind == "pi" {
				want = "submitted"
			}
			if got.State != want {
				t.Fatal(kind, got.State, want)
			}
		})
	}
}
