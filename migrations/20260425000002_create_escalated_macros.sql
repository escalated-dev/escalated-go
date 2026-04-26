-- +goose Up
-- Creates the agent-applied Macros table.
-- Distinct from escalated_workflows (admin event-driven) and
-- escalated_automations (admin time-based). See escalated-developer-context/
-- domain-model/workflows-automations-macros.md.
CREATE TABLE IF NOT EXISTS escalated_macros (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    actions TEXT NOT NULL DEFAULT '[]',
    is_shared INTEGER NOT NULL DEFAULT 1,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_escalated_macros_is_shared
    ON escalated_macros (is_shared);
CREATE INDEX IF NOT EXISTS idx_escalated_macros_created_by
    ON escalated_macros (created_by);

-- +goose Down
DROP TABLE IF EXISTS escalated_macros;
