-- name: BindProject :exec
INSERT INTO project_binding (singleton, root) VALUES (1, ?) ON CONFLICT(singleton) DO NOTHING;

-- name: BoundProject :one
SELECT root FROM project_binding WHERE singleton = 1;

-- name: InsertMessage :exec
INSERT INTO messages (id, idempotency_key, sender, recipient, body, created_at, state)
VALUES (?, ?, ?, ?, ?, ?, 'queued') ON CONFLICT(idempotency_key) DO NOTHING;

-- name: GetByKey :one
SELECT * FROM messages WHERE idempotency_key = ?;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = ?;

-- name: CountStates :many
SELECT state, COUNT(*) AS count FROM messages GROUP BY state;

-- name: ListMessages :many
SELECT * FROM messages ORDER BY seq DESC LIMIT ?;

-- name: ListBatches :many
SELECT * FROM batches ORDER BY rowid DESC LIMIT ?;

-- name: PendingRecipients :many
SELECT recipient FROM messages WHERE state IN ('queued','dispatching','uncertain')
GROUP BY recipient ORDER BY MIN(seq);

-- name: HeadMessages :many
SELECT * FROM messages WHERE recipient = ? AND state IN ('queued','dispatching','uncertain')
ORDER BY seq LIMIT ?;

-- name: LatestBatch :one
SELECT * FROM batches WHERE recipient = ? ORDER BY rowid DESC LIMIT 1;

-- name: InsertBatch :exec
INSERT INTO batches (id, recipient, pane, baseline_seq, state, created_at, updated_at)
VALUES (?, ?, ?, ?, 'dispatching', ?, ?);

-- name: InsertBatchMember :exec
INSERT INTO batch_members (batch_id, message_id, position) VALUES (?, ?, ?);

-- name: BatchMessages :many
SELECT messages.* FROM batch_members JOIN messages ON messages.id = batch_members.message_id
WHERE batch_members.batch_id = ? ORDER BY batch_members.position;

-- name: ClaimMessage :execrows
UPDATE messages SET state = 'dispatching', batch_id = ? WHERE id = ? AND state = 'queued';

-- name: FinishBatch :execrows
UPDATE batches SET state = ?, updated_at = ?, detail = ? WHERE id = ? AND state = 'dispatching';

-- name: FinishMessages :exec
UPDATE messages SET state = ? WHERE batch_id = ? AND state = 'dispatching';

-- name: RecoverBatches :exec
UPDATE batches SET state = 'uncertain', updated_at = ?, detail = 'daemon restarted during dispatch; inspect target before resolving'
WHERE state = 'dispatching';

-- name: RecoverMessages :exec
UPDATE messages SET state = 'uncertain' WHERE state = 'dispatching';

-- name: GetBatch :one
SELECT * FROM batches WHERE id = ?;

-- name: ResolveBatch :execrows
UPDATE batches SET state = ?, updated_at = ?, detail = ? WHERE id = ? AND state = 'uncertain';

-- name: ResolveMessagesSubmitted :exec
UPDATE messages SET state = 'submitted' WHERE batch_id = ? AND state = 'uncertain';

-- name: ResolveMessagesRetry :exec
UPDATE messages SET state = 'queued', batch_id = NULL WHERE batch_id = ? AND state = 'uncertain';

-- name: BrowseMessages :many
SELECT messages.*, batches.state AS batch_state,
       batches.updated_at AS batch_updated_at, batches.detail AS batch_detail
FROM messages LEFT JOIN batches ON batches.id = messages.batch_id
WHERE (sqlc.arg(before_seq) = 0 OR messages.seq < sqlc.arg(before_seq))
  AND (sqlc.arg(sender_filter) = '' OR messages.sender = sqlc.arg(sender_filter))
  AND (sqlc.arg(recipient_filter) = '' OR messages.recipient = sqlc.arg(recipient_filter))
  AND (sqlc.arg(state_filter) = '' OR messages.state = sqlc.arg(state_filter))
ORDER BY messages.seq DESC LIMIT sqlc.arg(page_limit);

-- name: HistoricalSenders :many
SELECT DISTINCT sender FROM messages ORDER BY sender;

-- name: HistoricalRecipients :many
SELECT DISTINCT recipient FROM messages ORDER BY recipient;

-- name: HeadPendingMessages :many
SELECT * FROM messages WHERE recipient = ? AND state IN ('queued','uncertain')
ORDER BY seq LIMIT ?;

-- name: ClaimPendingMessage :execrows
UPDATE messages SET state = 'dispatching', batch_id = ? WHERE id = ? AND state IN ('queued','uncertain');

-- name: MessageAttempts :one
SELECT COUNT(*) AS attempts FROM batch_members WHERE message_id = ?;

-- name: SupersedeBatch :execrows
UPDATE batches SET state = 'resolved_retry', updated_at = ?, detail = ? WHERE id = ? AND state = 'uncertain';

-- name: RecoverBatch :execrows
UPDATE batches SET state = 'uncertain', updated_at = ?, detail = ? WHERE id = ? AND state = 'dispatching';

-- name: RecoverBatchMessages :exec
UPDATE messages SET state = 'uncertain' WHERE batch_id = ? AND state = 'dispatching';
