-- +goose Up
-- CSAT ratings: one per ticket (unique index), submittable only once the
-- ticket is resolved or closed. Mirrors the Laravel satisfaction_ratings
-- table. rated_by_* is an optional polymorphic reference to the rater.
CREATE TABLE IF NOT EXISTS escalated_satisfaction_ratings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id INTEGER NOT NULL,
    rating INTEGER NOT NULL,
    comment TEXT,
    rated_by_type TEXT,
    rated_by_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_escalated_satisfaction_ratings_ticket
    ON escalated_satisfaction_ratings (ticket_id);

-- +goose Down
DROP TABLE IF EXISTS escalated_satisfaction_ratings;
