CREATE TABLE IF NOT EXISTS project_binding (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    root TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS batches (
    id TEXT PRIMARY KEY,
    recipient TEXT NOT NULL,
    pane TEXT NOT NULL,
    baseline_seq INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('dispatching','submitted','uncertain','resolved_retry','resolved_submitted')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    detail TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    idempotency_key TEXT NOT NULL UNIQUE,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('queued','dispatching','submitted','uncertain')),
    batch_id TEXT REFERENCES batches(id)
);
CREATE TABLE IF NOT EXISTS batch_members (
    batch_id TEXT NOT NULL REFERENCES batches(id),
    message_id TEXT NOT NULL REFERENCES messages(id),
    position INTEGER NOT NULL,
    PRIMARY KEY (batch_id, message_id),
    UNIQUE (batch_id, position)
);
CREATE INDEX IF NOT EXISTS messages_recipient_seq ON messages(recipient, seq);
CREATE INDEX IF NOT EXISTS batches_recipient_time ON batches(recipient, created_at);
