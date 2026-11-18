package db

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed schema.sql
var schema string

// Migrate 執行初始 DDL 及後續遞增遷移；user_version pragma 追蹤已套用版本。
func Migrate(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	return migrate1AddCanceledState(ctx, conn)
}

// migrate1AddCanceledState 將 messages.state CHECK 擴增 'canceled' 值。
// SQLite 不支援直接修改 CHECK 約束，需關閉外鍵後重建資料表。
func migrate1AddCanceledState(ctx context.Context, conn *sql.DB) error {
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON") }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE messages_v1 (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    idempotency_key TEXT NOT NULL UNIQUE,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('queued','dispatching','submitted','uncertain','canceled')),
    batch_id TEXT REFERENCES batches(id)
)`,
		`INSERT INTO messages_v1 SELECT * FROM messages`,
		`DROP TABLE messages`,
		`ALTER TABLE messages_v1 RENAME TO messages`,
		`CREATE INDEX IF NOT EXISTS messages_recipient_seq ON messages(recipient, seq)`,
		`PRAGMA user_version = 1`,
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return tx.Commit()
}
