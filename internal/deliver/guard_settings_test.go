package deliver

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/claudeguard"
)

func TestTeamGuardSettingsAPI(t *testing.T) {
	s, c := fixture(t)
	live := &fakeHerdr{agents: []Agent{seat(c, "dev", "idle", "w1:p1", 1), seat(c, "ba", "idle", "w1:p2", 1), seat(c, "gm", "idle", "w1:p3", 1), seat(c, "arch", "idle", "w1:p4", 1)}}
	h := API{Store: s, Config: c, Herdr: live}.Handler()
	request := func(method, body string) GuardSettings {
		t.Helper()
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest(method, "/v1/claude-guard", strings.NewReader(body)))
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		var result GuardSettings
		if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := request("GET", "")
	if first.Policy.Enabled || first.ProjectRoot != c.Root || len(first.Observations) != 4 {
		t.Fatal(first)
	}
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	for _, name := range []string{"SessionStart", "PreToolUse"} {
		if err := m.Apply(t.Context(), "gm", "wC:p5", claudeguard.Event{Name: name, Session: "s", Tool: "AskUserQuestion", ToolID: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	off := request("PUT", `{"version":1,"enabled":false}`)
	if off.Policy.Enabled {
		t.Fatal("not disabled")
	}
	if off.Observations[3].Role != "gm" || off.Observations[3].Status != "pending" {
		t.Fatal(off.Observations)
	}
	if err := m.Apply(t.Context(), "gm", "wC:p5", claudeguard.Event{Name: "PostToolUse", Session: "s", Tool: "AskUserQuestion", ToolID: "q"}); err != nil {
		t.Fatal(err)
	}
	after := request("GET", "")
	if after.Policy.Enabled || after.Observations[3].Status != "observed" {
		t.Fatal(after)
	}
	request("PUT", `{"version":1,"enabled":true}`)
	for _, body := range []string{`{}`, `null`, `{"version":1,"enabled":null}`, `{"version":1,"enabled":false,"enabled":true}`, `{"version":2,"enabled":false}`, `{"version":1,"enabled":false} {}`, strings.Repeat("x", 1025)} {
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("PUT", "/v1/claude-guard", strings.NewReader(body)))
		if out.Code != 400 {
			t.Fatal(body, out.Code)
		}
		if !request("GET", "").Policy.Enabled {
			t.Fatal("bad request changed policy")
		}
	}
}
func TestGuardDiscoveryFailureDoesNotPreventDisable(t *testing.T) {
	s, c := fixture(t)
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	if err := m.Apply(t.Context(), "担当A", "w1:p1", claudeguard.Event{Name: "SessionStart", Session: "test"}); err != nil {
		t.Fatal(err)
	}
	h := API{Store: s, Config: c, Herdr: &fakeHerdr{listErr: errors.New("offline")}}.Handler()
	out := httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest("PUT", "/v1/claude-guard", strings.NewReader(`{"version":1,"enabled":false}`)))
	var result GuardSettings
	if err := json.Unmarshal(out.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if out.Code != 200 || result.Policy.Enabled || result.DiscoveryError == "" || len(result.Observations) != 1 || result.Observations[0].Role != "担当A" {
		t.Fatal(out.Code, out.Body.String())
	}
}

func TestTeamGuardOffKeepsRecordingAndHerdrBlockedGate(t *testing.T) {
	s, c := fixture(t)
	m := claudeguard.Manager{Dir: filepath.Join(c.StateDir, "claude-guard")}
	a := seat(c, "gm", "blocked", "wC:p5", 1)
	a.Session = AgentSession{Agent: "claude", Kind: "id", Value: "s"}
	h := &fakeHerdr{agents: []Agent{a}}
	w := worker(s, c, h)
	for _, name := range []string{"SessionStart", "PreToolUse"} {
		if err := m.Apply(t.Context(), "gm", a.Pane, claudeguard.Event{Name: name, Session: "s", Tool: "AskUserQuestion", ToolID: "q"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.ConfigureTeam(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	msg := enqueue(t, s, "gm", "body")
	if err := w.Dispatch(t.Context(), "gm", a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Q.GetMessage(t.Context(), msg.ID)
	if got.State != "queued" || h.count() != 0 {
		t.Fatal("off bypassed blocked")
	}
	a.Status = "idle"
	h.agents = []Agent{a}
	if err := w.Dispatch(t.Context(), "gm", a); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Q.GetMessage(t.Context(), msg.ID)
	if got.State != "submitted" {
		t.Fatal("off did not bypass Hooks gate", got.State)
	}
	if err := m.Apply(t.Context(), "gm", a.Pane, claudeguard.Event{Name: "PostToolUse", Session: "s", Tool: "AskUserQuestion", ToolID: "q"}); err != nil {
		t.Fatal(err)
	}
	_, state, err := m.Inspect(t.Context(), "gm")
	if err != nil || len(state.Recent) != 1 || state.Recent[0].EndedAt == 0 {
		t.Fatal(state, err)
	}
}
