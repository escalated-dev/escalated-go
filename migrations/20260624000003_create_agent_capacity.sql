-- +goose Up
-- Per-agent, per-channel concurrent ticket capacity. Mirrors the Laravel
-- agent_capacity table (unique per user_id + channel).
CREATE TABLE IF NOT EXISTS escalated_agent_capacity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    channel TEXT NOT NULL DEFAULT 'default',
    max_concurrent INTEGER NOT NULL DEFAULT 10,
    current_count INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_escalated_agent_capacity_user_channel
    ON escalated_agent_capacity (user_id, channel);

CREATE INDEX IF NOT EXISTS idx_escalated_agent_capacity_user
    ON escalated_agent_capacity (user_id);

-- +goose Down
DROP TABLE IF EXISTS escalated_agent_capacity;
