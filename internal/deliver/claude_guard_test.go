package deliver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

func TestClaudeGuardKeepsQuestionQueued(t *testing.T) {
	s, c := fixture(t)
	ctx := t.Context()
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	if err := m.Configure(ctx, "gm", true); err != nil {
		t.Fatal(err)
	}
	a := seat(c, "gm", "idle", "wC:p5", 1)
	a.Session = AgentSession{Agent: "claude", Kind: "id", Value: "session-1"}
	h := &fakeHerdr{agents: []Agent{a}}
	w := worker(s, c, h)
	msg := enqueue(t, s, "gm", "defer this notification")
	for _, name := range []string{"", "SessionStart", "PreToolUse", "PostToolUse"} {
		if name != "" {
			if err := m.Apply(ctx, "gm", a.Pane, claudeguard.Event{Name: name, Session: a.Session.Value, Tool: "AskUserQuestion", ToolID: "q1"}); err != nil {
				t.Fatal(err)
			}
		}
		// SessionStart 的 ready 只測 guard；避免真的提交直到最後一步。
		if name == "SessionStart" {
			continue
		}
		if err := w.Dispatch(ctx, "gm", a); err != nil {
			t.Fatal(err)
		}
		got, err := s.Q.GetMessage(ctx, msg.ID)
		if err != nil {
			t.Fatal(err)
		}
		if name != "PostToolUse" {
			if got.State != "queued" || h.count() != 0 {
				t.Fatalf("state=%s prompts=%d", got.State, h.count())
			}
			batches, err := s.Q.LatestBatch(ctx, "gm")
			if err == nil {
				t.Fatalf("deferred created batch %+v", batches)
			}
		} else if got.State != "submitted" || h.count() != 1 {
			t.Fatalf("state=%s prompts=%d", got.State, h.count())
		}
	}
}
func TestClaudeGuardRechecksLiveSeat(t *testing.T) {
	for _, change := range []string{"status", "seq", "session", "pane"} {
		t.Run(change, func(t *testing.T) {
			s, c := fixture(t)
			ctx := context.Background()
			m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
			if err := m.Configure(ctx, "gm", true); err != nil {
				t.Fatal(err)
			}
			a := seat(c, "gm", "idle", "wC:p5", 1)
			a.Session = AgentSession{Agent: "claude", Kind: "id", Value: "session-1"}
			if err := m.Apply(ctx, "gm", a.Pane, claudeguard.Event{Name: "SessionStart", Session: a.Session.Value}); err != nil {
				t.Fatal(err)
			}
			current := a
			switch change {
			case "status":
				current.Status = "blocked"
			case "seq":
				seq := int64(2)
				current.Seq = &seq
			case "session":
				current.Session.Value = "other"
			case "pane":
				current.Pane = "wC:p6"
			}
			h := &fakeHerdr{agents: []Agent{current}}
			msg := enqueue(t, s, "gm", "body")
			if err := worker(s, c, h).Dispatch(ctx, "gm", a); err != nil {
				t.Fatal(err)
			}
			got, err := s.Q.GetMessage(ctx, msg.ID)
			if err != nil || got.State != "queued" || h.count() != 0 {
				t.Fatalf("%+v %v prompts=%d", got, err, h.count())
			}
		})
	}
}
