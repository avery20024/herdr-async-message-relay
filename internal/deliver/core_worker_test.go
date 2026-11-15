package deliver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

func TestCoreQueueAPIThenWorkerDeliversOnlyWhenIdle(t *testing.T) {
	f := coreSetup(t)
	s, c := fixture(t)
	c.Root, c.Backend, c.MaxAttempts = f.h.Root, "core", 1
	w := worker(s, c, f.h)
	body := "verbatim\n'quoted' $(never)"
	request, _ := json.Marshal(SendRequest{Sender: "dev", Recipient: "gm", Body: body, Key: "queued-first"})
	response := httptest.NewRecorder()
	(API{Store: s, Config: c}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(request)))
	if response.Code != http.StatusAccepted {
		t.Fatal(response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(f.dir, "calls")); !os.IsNotExist(err) {
		t.Fatal("enqueue called hc", err)
	}
	var accepted struct {
		ID       string `json:"id"`
		Accepted bool   `json:"accepted"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &accepted); err != nil || !accepted.Accepted {
		t.Fatal(accepted, err)
	}
	for _, state := range []string{"blocked", "working", "error"} {
		f.terminal["status"] = map[string]any{"app": "pi", "state": state}
		f.write(t, "get", f.terminal)
		if err := w.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		w.wg.Wait()
		message, err := s.Q.GetMessage(context.Background(), accepted.ID)
		if err != nil || message.State != "queued" || strings.Contains(f.calls(t), "send\x00") {
			t.Fatal(message, err)
		}
	}
	f.terminal["status"] = map[string]any{"app": "pi", "state": "idle"}
	f.write(t, "get", f.terminal)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	message, err := s.Q.GetMessage(context.Background(), accepted.ID)
	if err != nil || message.State != "submitted" || !strings.Contains(f.calls(t), body) {
		t.Fatal(message, err)
	}
}

func TestCoreNewProcessGenerationDoesNotInheritOldTurnFence(t *testing.T) {
	s, c := fixture(t)
	c.Backend, c.MaxAttempts = "core", 1
	h := coreWorkerFake{&fakeHerdr{}}
	w := worker(s, c, h)
	a := seat(c, "gm", "idle", "t2", 100)
	a.Backend = "core"
	a.Session = AgentSession{Agent: "pi", Kind: "core-process", Value: "100:42:77"}
	enqueue(t, s, "gm", "first")
	if err := w.Dispatch(context.Background(), "gm", a); err != nil {
		t.Fatal(err)
	}
	batch, err := s.Q.LatestBatch(context.Background(), "gm")
	if err != nil || batch.Pane != "t2@100:42:77" {
		t.Fatal(batch, err)
	}
	enqueue(t, s, "gm", "later")
	if err := w.Dispatch(context.Background(), "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatal("same generation bypassed turn fence")
	}
	zero := int64(0)
	a.Seq = &zero
	a.Session.Value = "100:42:88"
	if err := w.Dispatch(context.Background(), "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || strings.Contains(h.prompts[1], "first") || !strings.Contains(h.prompts[1], "later") {
		t.Fatal(h.prompts)
	}
}

func TestCoreRestartRecoveryHoldsFIFOForManualResolution(t *testing.T) {
	s, c := fixture(t)
	c.Backend, c.MaxAttempts = "core", 1
	ctx := context.Background()
	message := enqueue(t, s, "gm", "possibly submitted before restart")
	batch := sqlc.Batch{ID: NewID("batch-"), Recipient: "gm", Pane: "t2", BaselineSeq: 4, CreatedAt: time.Now().UnixMilli()}
	if err := s.Claim(ctx, batch, []sqlc.Message{message}); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	h := coreWorkerFake{&fakeHerdr{}}
	w := worker(s, c, h)
	a := seat(c, "gm", "idle", "t2", 100)
	a.Backend = "core"
	enqueue(t, s, "gm", "later")
	for range 2 {
		if err := w.Dispatch(ctx, "gm", a); err != nil {
			t.Fatal(err)
		}
	}
	if h.count() != 0 {
		t.Fatal("restarted core auto replayed or passed FIFO")
	}
	if err := s.Resolve(ctx, batch.ID, "submitted", "operator verified previous send"); err != nil {
		t.Fatal(err)
	}
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || strings.Contains(h.prompts[0], "possibly submitted before restart") || !strings.Contains(h.prompts[0], "later") {
		t.Fatal(h.prompts)
	}
}

type coreBlockingFake struct {
	*fakeHerdr
	blocked   chan struct{}
	delivered chan struct{}
}

func (h coreBlockingFake) PromptAgent(ctx context.Context, _ string, a Agent, body string) error {
	if err := h.Prompt(ctx, a.Pane, body); err != nil {
		return err
	}
	if a.Pane == "t2" {
		select {
		case <-h.blocked:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if a.Pane == "t3" {
		close(h.delivered)
	}
	return nil
}

func TestCoreOneSlowRecipientDoesNotBlockOtherRole(t *testing.T) {
	s, c := fixture(t)
	c.Backend, c.MaxAttempts = "core", 1
	gm := seat(c, "gm", "idle", "t2", 4)
	gm.Backend = "core"
	ba := seat(c, "ba", "idle", "t3", 4)
	ba.Backend = "core"
	h := coreBlockingFake{fakeHerdr: &fakeHerdr{agents: []Agent{gm, ba}}, blocked: make(chan struct{}), delivered: make(chan struct{})}
	w := worker(s, c, h)
	enqueue(t, s, "gm", "slow")
	enqueue(t, s, "ba", "independent")
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); w.wg.Wait() }()
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow role blocked independent role")
	}
	close(h.blocked)
	w.wg.Wait()
	if h.count() != 2 {
		t.Fatal(h.count())
	}
}
