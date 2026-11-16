package deliver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

func uncertainBatch(t *testing.T, s *Store, role string) (sqlc.Message, string) {
	t.Helper()
	m := enqueue(t, s, role, "original body")
	id := NewID("batch-")
	if err := s.Claim(t.Context(), sqlc.Batch{ID: id, Recipient: role, Pane: "old-seat", CreatedAt: 1}, []sqlc.Message{m}); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), id, "uncertain", "submission observation interrupted"); err != nil {
		t.Fatal(err)
	}
	return m, id
}

func TestManualRetryWaitsForReplacementSeatAndPreservesFIFO(t *testing.T) {
	s, c := fixture(t)
	c.Backend = "core"
	c.BatchSize = 1
	original, id := uncertainBatch(t, s, "gm")
	later := enqueue(t, s, "gm", "later")
	h := coreWorkerFake{&fakeHerdr{}}
	w := worker(s, c, h)
	if err := s.Resolve(t.Context(), id, "retry", "operator inspected; accepts possible duplicate"); err != nil {
		t.Fatal(err)
	}
	if err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	if h.count() != 0 {
		t.Fatal("sent without seat")
	}
	a := seat(c, "gm", "working", "t9", 5)
	a.Backend = "core"
	h.agents = []Agent{a}
	if err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	if h.count() != 0 {
		t.Fatal("sent to busy seat")
	}
	h.agents[0].Status = "idle"
	if err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	if h.count() != 1 || h.panes[0] != "t9" || !strings.Contains(h.prompts[0], original.ID) || strings.Contains(h.prompts[0], later.ID) || !strings.Contains(h.prompts[0], "RETRY:") {
		t.Fatal(h.prompts, h.panes)
	}
	a = seat(c, "gm", "idle", "t9", 6)
	a.Backend = "core"
	h.agents = []Agent{a}
	if err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.wg.Wait()
	if h.count() != 2 || !strings.Contains(h.prompts[1], later.ID) || strings.Contains(h.prompts[1], "RETRY:") {
		t.Fatal(h.prompts)
	}
}

func TestQueueBlocksStatusHistoryAndResolution(t *testing.T) {
	s, c := fixture(t)
	c.Backend = "core"
	m, id := uncertainBatch(t, s, "gm")
	next := enqueue(t, s, "gm", "waiting")
	// 摘要僅回傳最新100筆，舊 uncertain 阻塞仍須可見。
	for i := 0; i < 110; i++ {
		enqueue(t, s, "ba", fmt.Sprint(i))
		if err := s.Claim(t.Context(), sqlc.Batch{ID: NewID("batch-"), Recipient: "ba", CreatedAt: int64(i + 2)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	h := API{Store: s, Config: c}.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/status", nil))
	var status struct {
		QueueBlocks []QueueBlock   `json:"queue_blocks"`
		Messages    []sqlc.Message `json:"messages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil || rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String(), err)
	}
	if len(status.QueueBlocks) != 1 || status.QueueBlocks[0].BatchID != id || status.QueueBlocks[0].Recipient != "gm" || status.QueueBlocks[0].Reason != "uncertain_requires_resolution" {
		t.Fatal(status.QueueBlocks)
	}
	if len(status.Messages) != 100 {
		t.Fatal(len(status.Messages))
	}
	p := browse(t, h, "?state=queued&recipient=gm")
	if len(p.QueueBlocks) != 1 || !p.RetrySupported || len(p.Items) != 1 || p.Items[0].ID != next.ID {
		t.Fatal(p)
	}
	legacy := c
	legacy.Backend = "herdr"
	if got := browse(t, API{Store: s, Config: legacy}.Handler(), ""); got.QueueBlocks == nil || len(got.QueueBlocks) != 0 {
		t.Fatal("legacy not a core barrier", got.QueueBlocks)
	}

	request := func(root, msg string, confirm bool) int {
		b, _ := json.Marshal(map[string]any{"project_root": root, "message_id": msg, "action": "retry", "note": "operator inspected; possible duplicate accepted", "confirm": confirm})
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("POST", "/v1/batches/"+id+"/resolve", bytes.NewReader(b)))
		return out.Code
	}
	if request("/wrong", m.ID, true) != 409 || request(c.Root, next.ID, true) != 409 || request(c.Root, m.ID, false) != 400 {
		t.Fatal("unsafe retry accepted")
	}
	before, _ := s.Q.GetMessage(t.Context(), m.ID)
	if before.State != "uncertain" {
		t.Fatal(before)
	}
	if code := request(c.Root, m.ID, true); code != 200 {
		t.Fatal(code)
	}
	after, _ := s.Q.GetMessage(t.Context(), m.ID)
	if after.State != "queued" || after.BatchID.Valid || after.ID != m.ID || after.Body != m.Body || after.IdempotencyKey != m.IdempotencyKey || after.Seq != m.Seq {
		t.Fatal(after)
	}
	if request(c.Root, m.ID, true) != 409 {
		t.Fatal("duplicate retry must fail")
	}
	p = browse(t, h, "?recipient=gm")
	if len(p.QueueBlocks) != 0 || len(p.Items) != 2 || p.Items[1].ID != m.ID {
		t.Fatal(p)
	}
}
