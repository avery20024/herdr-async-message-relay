package deliver

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"
)

// 已達重試上限的訊息不得佔住候選視窗，否則後續 queued 又會被餓死（審核 H-1）。
func TestHeldMessagesDoNotStarveLaterQueued(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff, c.MaxAttempts = 0, 1
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("agent_prompt_stalled")}
	w := worker(s, c, h)
	for i := 0; i < 13; i++ {
		enqueue(t, s, "gm", "held")
	}
	for i := 0; i < 5; i++ {
		if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", int64(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	held, err := s.Q.HeldMessages(ctx, headHeld(c))
	if err != nil || len(held) != 13 {
		t.Fatalf("held=%d err=%v", len(held), err)
	}
	enqueue(t, s, "gm", "FRESH")
	h.err = nil
	before := h.count()
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 100)); err != nil {
		t.Fatal(err)
	}
	if h.count() != before+1 || !strings.Contains(h.prompts[len(h.prompts)-1], "FRESH") {
		t.Fatalf("held messages blocked the queue: %v", h.prompts)
	}
}

// 部分重試時，舊批次必須保持 uncertain，讓被保留的訊息仍能人工 resolve（審核 M-1）。
func TestPartialRetryKeepsHeldMessageResolvable(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff, c.MaxAttempts = 0, 2
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("submission response lost")}
	w := worker(s, c, h)
	first := enqueue(t, s, "gm", "one")
	enqueue(t, s, "gm", "two")
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 1)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Q.GetMessage(ctx, first.ID)
	old := got.BatchID.String
	// 模擬 first 先前已多嘗試一次，使其達上限；同批次另一則仍可重試。
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO batches(id,recipient,pane,baseline_seq,state,created_at,updated_at,detail) VALUES('earlier','gm','wC:p5',0,'resolved_submitted',1,1,'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO batch_members(batch_id,message_id,position) VALUES('earlier',?,1)`, first.ID); err != nil {
		t.Fatal(err)
	}
	h.err = nil
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 100)); err != nil {
		t.Fatal(err)
	}
	if held, _ := s.Q.HeldMessages(ctx, headHeld(c)); len(held) != 1 || held[0].ID != first.ID {
		t.Fatalf("expected first held, got %+v", held)
	}
	oldBatch, err := s.Q.GetBatch(ctx, old)
	if err != nil || oldBatch.State != "uncertain" {
		t.Fatalf("old batch settled early: %+v %v", oldBatch, err)
	}
	if err := s.Resolve(ctx, old, "retry", "operator checked the held message"); err != nil {
		t.Fatalf("held message not resolvable: %v", err)
	}
	if m, _ := s.Q.GetMessage(ctx, first.ID); m.State != "queued" {
		t.Fatal(m)
	}
}

// 被保留訊息的告警必須去重，不能每輪掃描洗版（審核 L-1）。
func TestHeldWarningLoggedOnce(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff, c.MaxAttempts = 0, 1
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("lost")}
	var buf bytes.Buffer
	w := &Worker{Store: s, Config: c, Herdr: h, Log: slog.New(slog.NewTextHandler(&buf, nil))}
	enqueue(t, s, "gm", "stuck")
	if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", 1)); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	for i := 0; i < 20; i++ {
		if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", int64(100+i))); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(buf.String(), "messages held after retry limit"); n != 1 {
		t.Fatalf("held warning repeated %d times", n)
	}
}

func headHeld(c Config) sqlc.HeldMessagesParams {
	return sqlc.HeldMessagesParams{Recipient: "gm", MaxAttempts: int64(c.MaxAttempts)}
}

// 預設設定（BatchSize=3、MaxAttempts=3）下的長時間失敗再恢復，不得再讓新訊息送不出（審核 H-1）。
func TestDefaultsOutageThenRecoveryStillDelivers(t *testing.T) {
	s, c := fixture(t)
	c.RetryBackoff = 0
	ctx := context.Background()
	h := &fakeHerdr{err: errors.New("agent_prompt_stalled")}
	w := worker(s, c, h)
	for i := 0; i < 20; i++ {
		enqueue(t, s, "gm", "m")
	}
	for i := 0; i < 60; i++ {
		if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", int64(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	held, err := s.Q.HeldMessages(ctx, headHeld(c))
	if err != nil || len(held) == 0 {
		t.Fatalf("expected held messages, got %d %v", len(held), err)
	}
	enqueue(t, s, "gm", "FRESH-AFTER-RECOVERY")
	h.err = nil
	before := h.count()
	for i := 0; i < 5; i++ {
		if err := w.Dispatch(ctx, "gm", seat(c, "gm", "idle", "wC:p5", int64(1000+i))); err != nil {
			t.Fatal(err)
		}
	}
	if h.count() == before {
		t.Fatalf("queue still stalled after recovery with %d held", len(held))
	}
	if !strings.Contains(h.prompts[len(h.prompts)-1], "FRESH-AFTER-RECOVERY") {
		t.Fatalf("fresh message not delivered: %v", h.prompts[len(h.prompts)-1])
	}
}
