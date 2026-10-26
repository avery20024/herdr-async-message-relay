package deliver

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

func TestMalformedGuardPolicyKeepsMessagesQueued(t *testing.T) {
	for _, policy := range []string{`{}`, `null`, `{"enable":true}`, `{"enabled":true}`, `{"version":1,"enabled":null}`, `{"version":2,"enabled":false}`} {
		t.Run(policy, func(t *testing.T) {
			s, c := fixture(t)
			dir := filepath.Join(c.StateDir, "claude-guard")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := (claudeguard.Manager{Dir: dir}).Apply(t.Context(), "gm", "wC:p5", claudeguard.Event{Name: "SessionStart", Session: "session-1"}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "team-policy.json"), []byte(policy), 0600); err != nil {
				t.Fatal(err)
			}
			a := seat(c, "gm", "idle", "wC:p5", 1)
			a.Session = AgentSession{Agent: "claude", Kind: "id", Value: "session-1"}
			h := &fakeHerdr{agents: []Agent{a}}
			msg := enqueue(t, s, "gm", "must remain queued")
			if err := worker(s, c, h).Dispatch(t.Context(), "gm", a); err != nil {
				t.Fatal(err)
			}
			got, err := s.Q.GetMessage(t.Context(), msg.ID)
			if err != nil || got.State != "queued" || h.count() != 0 {
				t.Fatalf("message=%+v err=%v prompts=%d", got, err, h.count())
			}
			if _, err := s.Q.LatestBatch(t.Context(), "gm"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("unexpected batch/error: %v", err)
			}
		})
	}
}
