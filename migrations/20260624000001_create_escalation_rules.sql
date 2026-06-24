-- +goose Up
-- Creates the time-based Escalation rules table. Distinct from
-- escalated_automations (general time-based) and escalated_workflows
-- (event-driven). Columns use sort_order / is_active to avoid the SQL
-- reserved word `order`; the JSON contract exposes them as order / is_active.
CREATE TABLE IF NOT EXISTS escalated_escalation_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    trigger_type TEXT,
    conditions TEXT NOT NULL DEFAULT '[]',
    actions TEXT NOT NULL DEFAULT '[]',
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_escalated_escalation_rules_active
    ON escalated_escalation_rules (is_active);

-- +goose Down
DROP TABLE IF EXISTS escalated_escalation_rules;
