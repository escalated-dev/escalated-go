-- +goose Up
-- Typed links between tickets (problem_incident, parent_child, related).
-- Mirrors the Laravel ticket_links table; unique per parent+child+type.
CREATE TABLE IF NOT EXISTS escalated_ticket_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_ticket_id INTEGER NOT NULL,
    child_ticket_id INTEGER NOT NULL,
    link_type TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_escalated_ticket_links_unique
    ON escalated_ticket_links (parent_ticket_id, child_ticket_id, link_type);

CREATE INDEX IF NOT EXISTS idx_escalated_ticket_links_parent
    ON escalated_ticket_links (parent_ticket_id);

CREATE INDEX IF NOT EXISTS idx_escalated_ticket_links_child
    ON escalated_ticket_links (child_ticket_id);

-- +goose Down
DROP TABLE IF EXISTS escalated_ticket_links;
