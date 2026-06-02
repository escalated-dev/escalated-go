-- +goose Up
-- Newsletter system: lists, list members, templates, newsletters, deliveries
-- + marketing_opt_out_at column on contacts. Mirrors Laravel/NestJS schema.
CREATE TABLE IF NOT EXISTS escalated_newsletter_lists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT,
    kind TEXT NOT NULL,
    filter_json TEXT,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_escalated_nl_kind ON escalated_newsletter_lists (kind);
CREATE INDEX IF NOT EXISTS idx_escalated_nl_created_by ON escalated_newsletter_lists (created_by);

CREATE TABLE IF NOT EXISTS escalated_newsletter_list_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL,
    contact_id INTEGER NOT NULL,
    added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    added_by INTEGER,
    UNIQUE (list_id, contact_id),
    FOREIGN KEY (list_id) REFERENCES escalated_newsletter_lists(id) ON DELETE CASCADE,
    FOREIGN KEY (contact_id) REFERENCES escalated_contacts(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_escalated_nlm_contact ON escalated_newsletter_list_members (contact_id);

CREATE TABLE IF NOT EXISTS escalated_newsletter_templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    theme TEXT NOT NULL DEFAULT 'default',
    subject_template TEXT,
    body_markdown TEXT NOT NULL,
    merge_fields_schema TEXT,
    created_by INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_escalated_nlt_theme ON escalated_newsletter_templates (theme);
CREATE INDEX IF NOT EXISTS idx_escalated_nlt_created_by ON escalated_newsletter_templates (created_by);

CREATE TABLE IF NOT EXISTS escalated_newsletters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subject TEXT NOT NULL,
    from_email TEXT NOT NULL,
    from_name TEXT,
    reply_to TEXT,
    target_list_id INTEGER NOT NULL,
    template_id INTEGER,
    theme TEXT,
    body_markdown TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    scheduled_at DATETIME,
    sent_at DATETIME,
    created_by INTEGER,
    sent_by INTEGER,
    summary_total INTEGER NOT NULL DEFAULT 0,
    summary_sent INTEGER NOT NULL DEFAULT 0,
    summary_opened INTEGER NOT NULL DEFAULT 0,
    summary_clicked INTEGER NOT NULL DEFAULT 0,
    summary_bounced INTEGER NOT NULL DEFAULT 0,
    summary_complained INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (target_list_id) REFERENCES escalated_newsletter_lists(id),
    FOREIGN KEY (template_id) REFERENCES escalated_newsletter_templates(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_escalated_n_status ON escalated_newsletters (status);
CREATE INDEX IF NOT EXISTS idx_escalated_n_scheduled_at ON escalated_newsletters (scheduled_at);
CREATE INDEX IF NOT EXISTS idx_escalated_n_status_sched ON escalated_newsletters (status, scheduled_at);

CREATE TABLE IF NOT EXISTS escalated_newsletter_deliveries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    newsletter_id INTEGER NOT NULL,
    contact_id INTEGER NOT NULL,
    email_at_send TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    tracking_token TEXT NOT NULL UNIQUE,
    sent_at DATETIME,
    opened_at DATETIME,
    last_clicked_at DATETIME,
    clicks_count INTEGER NOT NULL DEFAULT 0,
    bounce_reason TEXT,
    failure_reason TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    claimed_at DATETIME,
    next_attempt_at DATETIME,
    is_test INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (newsletter_id) REFERENCES escalated_newsletters(id) ON DELETE CASCADE,
    FOREIGN KEY (contact_id) REFERENCES escalated_contacts(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_escalated_nd_nl_status ON escalated_newsletter_deliveries (newsletter_id, status);
CREATE INDEX IF NOT EXISTS idx_escalated_nd_contact ON escalated_newsletter_deliveries (contact_id);
CREATE INDEX IF NOT EXISTS idx_escalated_nd_status_claimed ON escalated_newsletter_deliveries (status, claimed_at);

ALTER TABLE escalated_contacts ADD COLUMN marketing_opt_out_at DATETIME;
CREATE INDEX IF NOT EXISTS idx_escalated_contact_opt_out ON escalated_contacts (marketing_opt_out_at);

-- +goose Down
DROP INDEX IF EXISTS idx_escalated_contact_opt_out;
ALTER TABLE escalated_contacts DROP COLUMN marketing_opt_out_at;
DROP TABLE IF EXISTS escalated_newsletter_deliveries;
DROP TABLE IF EXISTS escalated_newsletters;
DROP TABLE IF EXISTS escalated_newsletter_templates;
DROP TABLE IF EXISTS escalated_newsletter_list_members;
DROP TABLE IF EXISTS escalated_newsletter_lists;
