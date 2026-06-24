-- +goose Up
-- Per-ticket side conversation threads + replies. Mirrors the Laravel
-- side_conversations / side_conversation_replies tables.
CREATE TABLE IF NOT EXISTS escalated_side_conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id INTEGER NOT NULL,
    subject TEXT NOT NULL,
    channel TEXT NOT NULL DEFAULT 'internal',
    status TEXT NOT NULL DEFAULT 'open',
    created_by TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_escalated_side_conversations_ticket
    ON escalated_side_conversations (ticket_id);

CREATE TABLE IF NOT EXISTS escalated_side_conversation_replies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    side_conversation_id INTEGER NOT NULL,
    body TEXT NOT NULL,
    author_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_escalated_side_conversation_replies_conv
    ON escalated_side_conversation_replies (side_conversation_id);

-- +goose Down
DROP TABLE IF EXISTS escalated_side_conversation_replies;
DROP TABLE IF EXISTS escalated_side_conversations;
