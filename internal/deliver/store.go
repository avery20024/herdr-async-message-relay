package deliver

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/avery20024/herdr-async-message-relay/db"
	"github.com/avery20024/herdr-async-message-relay/internal/sqlc"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
	Q  *sqlc.Queries
}

type SendRequest struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Body      string `json:"body"`
	Key       string `json:"idempotency_key"`
}

func NewID(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(buf[:])
}

func OpenStore(ctx context.Context, path string) (*Store, error) {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database must be a regular file: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// 先以私人權限建立檔案，避免 SQLite 第一次開啟時使用寬鬆權限。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	conn, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err := db.Migrate(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return &Store{DB: conn, Q: sqlc.New(conn)}, nil
}

func (s *Store) transaction(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(s.Q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

var ErrConflict = errors.New("idempotency key already used with different content")

func (s *Store) Enqueue(ctx context.Context, r SendRequest) (m sqlc.Message, err error) {
	err = s.transaction(ctx, func(q *sqlc.Queries) error {
		if err := q.InsertMessage(ctx, sqlc.InsertMessageParams{ID: NewID("msg-"), IdempotencyKey: r.Key, Sender: r.Sender, Recipient: r.Recipient, Body: r.Body, CreatedAt: time.Now().UnixMilli()}); err != nil {
			return err
		}
		var e error
		m, e = q.GetByKey(ctx, r.Key)
		if e != nil {
			return e
		}
		if m.Sender != r.Sender || m.Recipient != r.Recipient || m.Body != r.Body {
			return ErrConflict
		}
		return nil
	})
	return
}

func (s *Store) Recover(ctx context.Context) error {
	return s.transaction(ctx, func(q *sqlc.Queries) error {
		if err := q.RecoverBatches(ctx, time.Now().UnixMilli()); err != nil {
			return err
		}
		return q.RecoverMessages(ctx)
	})
}

// Claim 將 queued 或 unsure 訊息重新包進新批次；舊的 unsure 批次會標為已重試，保留成員紀錄供追溯。
func (s *Store) Claim(ctx context.Context, b sqlc.Batch, messages []sqlc.Message) error {
	return s.transaction(ctx, func(q *sqlc.Queries) error {
		if err := q.InsertBatch(ctx, sqlc.InsertBatchParams{ID: b.ID, Recipient: b.Recipient, Pane: b.Pane, BaselineSeq: b.BaselineSeq, CreatedAt: b.CreatedAt, UpdatedAt: b.CreatedAt}); err != nil {
			return err
		}
		for i, m := range messages {
			if m.State == "uncertain" && m.BatchID.Valid {
				if _, err := q.SupersedeBatch(ctx, sqlc.SupersedeBatchParams{ID: m.BatchID.String, UpdatedAt: time.Now().UnixMilli(), Detail: "superseded by retry batch " + b.ID}); err != nil {
					return err
				}
			}
			n, err := q.ClaimPendingMessage(ctx, sqlc.ClaimPendingMessageParams{ID: m.ID, BatchID: sql.NullString{String: b.ID, Valid: true}})
			if err != nil {
				return err
			}
			if n != 1 {
				return errors.New("message is no longer pending")
			}
			if err := q.InsertBatchMember(ctx, sqlc.InsertBatchMemberParams{BatchID: b.ID, MessageID: m.ID, Position: int64(i + 1)}); err != nil {
				return err
			}
		}
		return nil
	})
}

// SupersedeStale 將中斷已久、仍停在 dispatching 的批次就地轉為 unsure，讓後續掃描可以重試。
func (s *Store) SupersedeStale(ctx context.Context, id string) error {
	return s.transaction(ctx, func(q *sqlc.Queries) error {
		n, err := q.RecoverBatch(ctx, sqlc.RecoverBatchParams{ID: id, UpdatedAt: time.Now().UnixMilli(), Detail: "dispatch observation did not finish; retried as uncertain"})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("batch is not dispatching")
		}
		return q.RecoverBatchMessages(ctx, sql.NullString{String: id, Valid: true})
	})
}

func (s *Store) Finish(ctx context.Context, id, state, detail string) error {
	return s.transaction(ctx, func(q *sqlc.Queries) error {
		n, err := q.FinishBatch(ctx, sqlc.FinishBatchParams{ID: id, State: state, Detail: detail, UpdatedAt: time.Now().UnixMilli()})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("batch no longer dispatching")
		}
		return q.FinishMessages(ctx, sqlc.FinishMessagesParams{State: state, BatchID: sql.NullString{String: id, Valid: true}})
	})
}

func (s *Store) Resolve(ctx context.Context, id, action, note string) error {
	if action != "retry" && action != "submitted" {
		return errors.New("action must be retry or submitted")
	}
	return s.transaction(ctx, func(q *sqlc.Queries) error {
		n, err := q.ResolveBatch(ctx, sqlc.ResolveBatchParams{ID: id, State: "resolved_" + action, Detail: note, UpdatedAt: time.Now().UnixMilli()})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("only uncertain batches may be resolved")
		}
		key := sql.NullString{String: id, Valid: true}
		if action == "retry" {
			return q.ResolveMessagesRetry(ctx, key)
		}
		return q.ResolveMessagesSubmitted(ctx, key)
	})
}
