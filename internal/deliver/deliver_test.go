package deliver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

func fixture(t *testing.T) (*Store, Config) {
	t.Helper()
	root := t.TempDir()
	for _, role := range []string{"gm", "dev", "ba", "arch"} {
		if err := os.MkdirAll(filepath.Join(root, "角色", role), 0700); err != nil {
			t.Fatal(err)
		}
	}
	c := Config{Root: root, StateDir: root, BatchSize: 3, BatchBytes: 12000, Poll: 10 * time.Millisecond, CommandTimeout: time.Second, RetryBackoff: 0, MaxAttempts: 3}
	s, err := OpenStore(context.Background(), filepath.Join(root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s, c
}
func enqueue(t *testing.T, s *Store, to, body string) sqlc.Message {
	t.Helper()
	m, err := s.Enqueue(context.Background(), SendRequest{Sender: "dev", Recipient: to, Body: body, Key: NewID("test-")})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func seat(c Config, role, status, pane string, seq int64) Agent {
	return Agent{CWD: filepath.Join(c.Root, "角色", role), Pane: pane, Status: status, Seq: &seq}
}
func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeHerdr struct {
	mu      sync.Mutex
	agents  []Agent
	prompts []string
	panes   []string
	err     error
	lists   int
	block   chan struct{}
}

func (f *fakeHerdr) List(context.Context) ([]Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	return append([]Agent{}, f.agents...), nil
}
func (f *fakeHerdr) Prompt(ctx context.Context, pane, body string) error {
	f.mu.Lock()
	f.prompts = append(f.prompts, body)
	f.panes = append(f.panes, pane)
	f.mu.Unlock()
	if f.block != nil && pane == "wC:p5" {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}
func (f *fakeHerdr) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.prompts) }
func worker(s *Store, c Config, h Herdr) *Worker {
	return &Worker{Store: s, Config: c, Herdr: h, Log: logger()}
}

func TestEnqueueIdempotencyPersistence(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	r := SendRequest{Sender: "dev", Recipient: "gm", Body: "exact\nbody 'quoted'", Key: "same-key"}
	m, err := s.Enqueue(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Enqueue(ctx, r)
	if err != nil || again.ID != m.ID {
		t.Fatalf("duplicate: %+v %v", again, err)
	}
	r.Body = "different"
	if _, err = s.Enqueue(ctx, r); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(ctx, filepath.Join(c.Root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	got, err := reopened.Q.GetMessage(ctx, m.ID)
	if err != nil || got.Body != m.Body || got.State != "queued" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestConcurrentIdempotency(t *testing.T) {
	s, _ := fixture(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			_, err := s.Enqueue(context.Background(), SendRequest{Sender: "dev", Recipient: "gm", Body: "body", Key: "same"})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	rows, err := s.Q.ListMessages(context.Background(), 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
}

func TestBatchThreeFIFOAndStateFence(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	h := &fakeHerdr{}
	w := worker(s, c, h)
	for i := 1; i <= 5; i++ {
		enqueue(t, s, "gm", fmt.Sprintf("body-%d\nnot compressed", i))
	}
	a := seat(c, "gm", "idle", "wC:p5", 10)
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatal(h.count())
	}
	text := h.prompts[0]
	if !strings.Contains(text, "[hrelay | batch ") || !strings.Contains(text, "keep contexts separate") || !strings.Contains(text, "| 3 messages]") {
		t.Fatal(text)
	}
	for i := 1; i <= 3; i++ {
		if !strings.Contains(text, fmt.Sprintf("body-%d\nnot compressed", i)) {
			t.Fatal(text)
		}
	}
	if strings.Contains(text, "body-4") {
		t.Fatal(text)
	}
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatal("stale idle allowed another batch")
	}
	a = seat(c, "gm", "idle", "wC:p5", 12)
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || !strings.Contains(h.prompts[1], "| 2 messages]") || !strings.Contains(h.prompts[1], "body-4") {
		t.Fatal(h.prompts)
	}
}

func TestConfigurableBatchAndByteLimit(t *testing.T) {
	for _, size := range []int{1, 2, 4} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, c := fixture(t)
			c.BatchSize = size
			h := &fakeHerdr{}
			w := worker(s, c, h)
			for range 5 {
				enqueue(t, s, "gm", "body")
			}
			if err := w.Dispatch(context.Background(), "gm", seat(c, "gm", "done", "wA:pB", 1)); err != nil {
				t.Fatal(err)
			}
			if strings.Count(h.prompts[0], "\n--- message ") != size {
				t.Fatal(h.prompts)
			}
		})
	}
	t.Run("bytes", func(t *testing.T) {
		s, c := fixture(t)
		c.BatchBytes = 2048
		h := &fakeHerdr{}
		w := worker(s, c, h)
		for range 3 {
			enqueue(t, s, "gm", strings.Repeat("x", 900))
		}
		if err := w.Dispatch(context.Background(), "gm", seat(c, "gm", "idle", "wC:p5", 1)); err != nil {
			t.Fatal(err)
		}
		if len(h.prompts[0]) > 2048 || strings.Count(h.prompts[0], "\n--- message ") != 1 {
			t.Fatal(len(h.prompts[0]))
		}
	})
}

func TestBusyAndUnknownNeverDispatch(t *testing.T) {
	s, c := fixture(t)
	h := &fakeHerdr{}
	w := worker(s, c, h)
	enqueue(t, s, "gm", "body")
	for _, status := range []string{"working", "blocked", "unknown", "", "gone"} {
		if err := w.Dispatch(context.Background(), "gm", seat(c, "gm", status, "wC:p5", 1)); err != nil {
			t.Fatal(err)
		}
	}
	if h.count() != 0 {
		t.Fatal(h.count())
	}
}

func TestUncertainRetriesAfterBackoffAndKeepsHistory(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff = 0
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("submission response lost")}
	w := worker(s, c, h)
	first := enqueue(t, s, "gm", "first")
	enqueue(t, s, "gm", "second")
	a := seat(c, "gm", "idle", "wC:p5", 1)
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Q.GetMessage(ctx, first.ID)
	if got.State != "uncertain" {
		t.Fatal(got)
	}
	old := got.BatchID.String
	// 退避結束後同一批訊息以 FIFO 重新打包，送到目前解析到的座位。
	h.err = nil
	a = seat(c, "gm", "idle", "wD:p7", 100)
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || h.panes[1] != "wD:p7" || !strings.Contains(h.prompts[1], "first") || !strings.Contains(h.prompts[1], "second") {
		t.Fatal(h.prompts)
	}
	if !strings.Contains(h.prompts[1], "RETRY: 2 of 2 were sent earlier") || strings.Contains(h.prompts[0], "RETRY") {
		t.Fatal(h.prompts)
	}
	if m, _ := s.Q.GetMessage(ctx, first.ID); m.State != "submitted" {
		t.Fatal(m)
	}
	members, err := s.Q.BatchMessages(ctx, old)
	if err != nil || len(members) != 2 || members[0].ID != first.ID {
		t.Fatalf("old batch membership lost: %+v %v", members, err)
	}
	if b, err := s.Q.GetBatch(ctx, old); err != nil || b.State != "resolved_retry" {
		t.Fatalf("old batch not marked retried: %+v %v", b, err)
	}
	if err := s.Resolve(ctx, old, "retry", "again"); err == nil {
		t.Fatal("superseded batch resolved twice")
	}
}

func TestRetryBackoffDefersUncertainBatch(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff = time.Hour
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("submission response lost")}
	w := worker(s, c, h)
	enqueue(t, s, "gm", "first")
	a := seat(c, "gm", "idle", "wC:p5", 1)
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	h.err = nil
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 100)); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatal("retry ignored backoff")
	}
}

func TestAttemptLimitQuarantinesAndKeepsFIFOFlowing(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff, c.MaxAttempts = 0, 1
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("submission response lost")}
	w := worker(s, c, h)
	enqueue(t, s, "gm", "stuck")
	a := seat(c, "gm", "idle", "wC:p5", 1)
	if err := w.Dispatch(ctx, "gm", a); err != nil {
		t.Fatal(err)
	}
	enqueue(t, s, "gm", "later")
	h.err = nil
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 100)); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || strings.Contains(h.prompts[1], "stuck") || !strings.Contains(h.prompts[1], "later") {
		t.Fatal(h.prompts)
	}
}

func TestStaleDispatchingRecoversForRetry(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff = 0
	ctx := context.Background()
	m := enqueue(t, s, "gm", "body")
	b := sqlc.Batch{ID: NewID("batch-"), Recipient: "gm", Pane: "wC:p5", BaselineSeq: 1, CreatedAt: time.Now().UnixMilli()}
	if err := s.Claim(ctx, b, []sqlc.Message{m}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE batches SET updated_at = ? WHERE id = ?", time.Now().Add(-time.Hour).UnixMilli(), b.ID); err != nil {
		t.Fatal(err)
	}
	h := &fakeHerdr{}
	w := worker(s, c, h)
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p9", 50)); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || !strings.Contains(h.prompts[0], "body") {
		t.Fatal(h.prompts)
	}
	if old, err := s.Q.GetBatch(ctx, b.ID); err != nil || old.State != "resolved_retry" {
		t.Fatalf("%+v %v", old, err)
	}
}

func TestRecoveryAndResolveSubmitted(t *testing.T) {
	s, c := fixture(t)
	// 重啟恢復後仍需退避，避免恢復即重播；本案例驗證人工確認已提交。
	c.RetryBackoff = time.Hour
	ctx := context.Background()
	m := enqueue(t, s, "gm", "body")
	b := sqlc.Batch{ID: NewID("batch-"), Recipient: "gm", Pane: "wC:p5", BaselineSeq: 1, CreatedAt: time.Now().UnixMilli()}
	if err := s.Claim(ctx, b, []sqlc.Message{m}); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Q.GetMessage(ctx, m.ID)
	if got.State != "uncertain" {
		t.Fatal(got)
	}
	h := &fakeHerdr{}
	w := worker(s, c, h)
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 2)); err != nil {
		t.Fatal(err)
	}
	if h.count() != 0 {
		t.Fatal("replayed during retry backoff")
	}
	if err := s.Resolve(ctx, b.ID, "submitted", "confirmed prompt submitted"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Q.GetMessage(ctx, m.ID)
	if got.State != "submitted" {
		t.Fatal(got)
	}
}

func TestResolveSeatSafety(t *testing.T) {
	_, c := fixture(t)
	a := seat(c, "gm", "idle", "wC:p5", 5)
	for _, agents := range [][]Agent{nil, {a, a}, {seat(c, "dev", "idle", "wC:p5", 5)}, {{CWD: a.CWD, Pane: "--now", Seq: a.Seq}}, {{CWD: a.CWD, Pane: a.Pane}}} {
		if _, err := ResolveAgent(c.Root, "gm", agents); err == nil {
			t.Fatal(agents)
		}
	}
	found, err := ResolveAgent(c.Root, "gm", []Agent{a})
	if err != nil || found.Pane != "wC:p5" {
		t.Fatalf("%+v %v", found, err)
	}
}

func TestRecreatedPaneGetsQueuedMessages(t *testing.T) {
	s, c := fixture(t)
	h := &fakeHerdr{}
	w := worker(s, c, h)
	enqueue(t, s, "gm", "old queued")
	if err := w.Dispatch(context.Background(), "gm", seat(c, "gm", "working", "wC:p5", 1)); err != nil {
		t.Fatal(err)
	}
	if err := w.Dispatch(context.Background(), "gm", seat(c, "gm", "idle", "wD:p9", 1)); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || h.panes[0] != "wD:p9" {
		t.Fatal(h.panes)
	}
}

func TestDifferentRolesDoNotBlockEachOther(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	release := make(chan struct{})
	h := &fakeHerdr{agents: []Agent{seat(c, "gm", "idle", "wC:p5", 1), seat(c, "ba", "idle", "wC:p3", 1)}, block: release}
	w := worker(s, c, h)
	enqueue(t, s, "gm", "gm")
	enqueue(t, s, "ba", "ba")
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for h.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	w.wg.Wait()
	if h.count() != 2 {
		t.Fatal("one recipient blocked another")
	}
	h.mu.Lock()
	lists := h.lists
	h.mu.Unlock()
	if lists != 1 {
		t.Fatal(lists)
	}
}

func TestAPIValidationAndCommit(t *testing.T) {
	s, c := fixture(t)
	handler := API{Store: s, Config: c}.Handler()
	send := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	valid := `{"sender":"dev","recipient":"gm","body":"hello","idempotency_key":"k"}`
	if out := send(valid); out.Code != 202 || !strings.Contains(out.Body.String(), `"accepted":true`) {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := send(valid); out.Code != 202 {
		t.Fatal(out.Code)
	}
	for _, body := range []string{`{}`, `null`, `{"sender":"../dev","recipient":"gm","body":"x","idempotency_key":"k2"}`, `{"sender":"dev","recipient":"missing","body":"x","idempotency_key":"k2"}`, `{"sender":"dev","recipient":"gm","body":"x","idempotency_key":"k2","unknown":1}`, valid + " {}"} {
		if out := send(body); out.Code != 400 {
			t.Fatal(body, out.Code)
		}
	}
	if out := send(strings.Replace(valid, "hello", "changed", 1)); out.Code != 409 {
		t.Fatal(out.Code)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/v1/status", nil))
	if out.Code != http.StatusOK || strings.Contains(out.Body.String(), "hello") {
		t.Fatal(out.Body.String())
	}
	m, _ := s.Q.GetByKey(context.Background(), "k")
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/v1/messages/"+m.ID, nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), "hello") {
		t.Fatal(out.Body.String())
	}
}

func TestInvalidConfigAndPrivateDB(t *testing.T) {
	s, c := fixture(t)
	_ = s
	info, err := os.Stat(filepath.Join(c.Root, "queue.db"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("%v %v", info, err)
	}
	t.Setenv("DELIVER_POLL_MS", "")
	t.Setenv("DELIVER_BATCH_BYTES", "")
	t.Setenv("DELIVER_BATCH_SIZE", "0")
	if _, err := LoadConfig(c.Root); err == nil {
		t.Fatal("invalid batch accepted")
	}
	t.Setenv("DELIVER_BATCH_SIZE", "7")
	t.Setenv("DELIVER_STATE_DIR", c.Root)
	got, err := LoadConfig(c.Root)
	if err != nil || got.BatchSize != 7 || got.Poll != 500*time.Millisecond {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSQLiteFailureNotAccepted(t *testing.T) {
	s, c := fixture(t)
	s.DB.Close()
	out := httptest.NewRecorder()
	API{Store: s, Config: c}.Handler().ServeHTTP(out, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"sender":"dev","recipient":"gm","body":"x","idempotency_key":"k"}`)))
	if out.Code != 500 {
		t.Fatal(out.Code, out.Body.String())
	}
}
