package db

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed schema.sql
var schema string

// Migrate 僅執行初始 DDL；應用查詢一律由 sqlc 產生。
func Migrate(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, schema)
	return err
}
