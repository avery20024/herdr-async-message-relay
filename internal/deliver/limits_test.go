package deliver

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

func TestLargestAcceptedSingleMessageFitsEnvelope(t *testing.T) {
	for _, limit := range []int{2048, 12000, 64000} {
		m := sqlc.Message{ID: NewID("msg-"), Sender: strings.Repeat("a", 64), Recipient: strings.Repeat("b", 64), Body: strings.Repeat("x", limit-1024)}
		text := RenderBatch(NewID("batch-"), []sqlc.Message{m})
		if len(text) > limit {
			t.Fatalf("envelope exceeds reserve: %d > %d", len(text), limit)
		}
	}
}

func TestNULBodyRejected(t *testing.T) {
	s, c := fixture(t)
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"sender":"dev","recipient":"gm","body":"bad\u0000body","idempotency_key":"nul"}`))
	out := httptest.NewRecorder()
	API{Store: s, Config: c}.Handler().ServeHTTP(out, r)
	if out.Code != 400 {
		t.Fatal(out.Code, out.Body.String())
	}
	rows, _ := s.Q.ListMessages(context.Background(), 100)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestResolveRequiresConfirmationAndNote(t *testing.T) {
	s, c := fixture(t)
	handler := API{Store: s, Config: c}.Handler()
	for _, body := range []string{`{"action":"retry","note":"checked"}`, `{"action":"retry","confirm":true,"note":" "}`} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("POST", "/v1/batches/batch-test/resolve", strings.NewReader(body)))
		if out.Code != 400 {
			t.Fatal(out.Code, out.Body.String())
		}
	}
}

func TestSameRoleHasOnlyOneInFlightBatch(t *testing.T) {
	s, c := fixture(t)
	release := make(chan struct{})
	h := &fakeHerdr{agents: []Agent{seat(c, "gm", "idle", "wC:p5", 1)}, block: release}
	w := worker(s, c, h)
	for range 6 {
		enqueue(t, s, "gm", "body")
	}
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 連續 tick 即使看到相同 idle 快照，也只能有一個該角色 worker。
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	w.wg.Wait()
	if h.count() != 1 {
		t.Fatal(h.count())
	}
}
