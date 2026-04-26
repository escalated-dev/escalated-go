-- +goose Up
-- Creates the time-based admin Automation rules table.
-- Distinct from escalated_workflows (event-driven) and escalated_macros
-- (agent manual). See escalated-developer-context/domain-model/
-- workflows-automations-macros.md.
CREATE TABLE IF NOT EXISTS escalated_automations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    conditions TEXT NOT NULL DEFAULT '[]',
    actions TEXT NOT NULL DEFAULT '[]',
    active INTEGER NOT NULL DEFAULT 1,
    position INTEGER NOT NULL DEFAULT 0,
    last_run_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_escalated_automations_active
    ON escalated_automations (active);

-- +goose Down
DROP TABLE IF EXISTS escalated_automations;
