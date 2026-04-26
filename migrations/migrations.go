// Package migrations provides embedded SQL migrations for the Escalated ticket system.
// Call Migrate(db, prefix) to create all required tables.
package migrations

import (
	"database/sql"
	"fmt"
	"strings"
)

// Migrate runs all Escalated migrations against the given database.
// It is idempotent — tables are created with IF NOT EXISTS.
// The prefix is prepended to all table names (e.g., "escalated_").
func Migrate(db *sql.DB, prefix string) error {
	for _, stmt := range migrationStatements(prefix) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration error: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

func migrationStatements(p string) []string {
	// Use TEXT for JSON columns — works on both PostgreSQL (json/jsonb also accepts TEXT) and SQLite.
	// PostgreSQL users can alter these to jsonb after migration if desired.
	stmts := []string{
		// 1. SLA Policies (created first because departments reference them)
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			first_response_hours TEXT NOT NULL,
			resolution_hours TEXT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"sla_policies"),

		// 2. Departments
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			slug VARCHAR(255) NOT NULL,
			description TEXT,
			email VARCHAR(255),
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			default_sla_policy_id BIGINT REFERENCES %s(id) ON DELETE SET NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"departments", p+"sla_policies"),

		// 3. Tags
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			slug VARCHAR(255) NOT NULL,
			color VARCHAR(7),
			description TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"tags"),

		// 4. Tickets
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			reference VARCHAR(255) NOT NULL,
			subject VARCHAR(255) NOT NULL,
			description TEXT NOT NULL,
			status INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 1,
			ticket_type VARCHAR(50) DEFAULT 'question',
			requester_type VARCHAR(255),
			requester_id BIGINT,
			guest_name VARCHAR(255),
			guest_email VARCHAR(255),
			guest_token VARCHAR(64),
			contact_id BIGINT,
			assigned_to BIGINT,
			department_id BIGINT REFERENCES %s(id) ON DELETE SET NULL,
			sla_policy_id BIGINT REFERENCES %s(id) ON DELETE SET NULL,
			merged_into_id BIGINT,
			sla_first_response_due_at TIMESTAMP,
			sla_resolution_due_at TIMESTAMP,
			sla_breached BOOLEAN NOT NULL DEFAULT FALSE,
			first_response_at TIMESTAMP,
			resolved_at TIMESTAMP,
			closed_at TIMESTAMP,
			metadata TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"tickets", p+"departments", p+"sla_policies"),

		// 5. Replies
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			body TEXT NOT NULL,
			author_type VARCHAR(255),
			author_id BIGINT,
			is_internal BOOLEAN NOT NULL DEFAULT FALSE,
			is_system BOOLEAN NOT NULL DEFAULT FALSE,
			is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"replies", p+"tickets"),

		// 6. Ticket tags join table
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			tag_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			PRIMARY KEY (ticket_id, tag_id)
		)`, p+"ticket_tags", p+"tickets", p+"tags"),

		// 7. Ticket activities
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			action VARCHAR(255) NOT NULL,
			causer_type VARCHAR(255),
			causer_id BIGINT,
			details TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"ticket_activities", p+"tickets"),

		// 8. Email Channels
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			email_address VARCHAR(255) NOT NULL,
			display_name VARCHAR(255),
			department_id BIGINT REFERENCES %s(id) ON DELETE SET NULL,
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			is_verified BOOLEAN NOT NULL DEFAULT FALSE,
			dkim_status VARCHAR(32) NOT NULL DEFAULT 'pending',
			dkim_public_key TEXT,
			dkim_selector VARCHAR(255),
			reply_to_address VARCHAR(255),
			smtp_protocol VARCHAR(32) DEFAULT 'tls',
			smtp_host VARCHAR(255),
			smtp_port INTEGER,
			smtp_username VARCHAR(255),
			smtp_password VARCHAR(255),
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"email_channels", p+"departments"),

		// 9. Custom Fields
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			slug VARCHAR(255) NOT NULL,
			field_type VARCHAR(50) NOT NULL DEFAULT 'text',
			description TEXT,
			is_required BOOLEAN NOT NULL DEFAULT FALSE,
			options TEXT,
			default_value VARCHAR(255),
			entity_type VARCHAR(50) NOT NULL DEFAULT 'ticket',
			position INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"custom_fields"),

		// 10. Custom Field Values
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			custom_field_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			entity_type VARCHAR(50) NOT NULL DEFAULT 'ticket',
			entity_id BIGINT NOT NULL,
			value TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"custom_field_values", p+"custom_fields"),

		// 11. Custom Objects
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			slug VARCHAR(255) NOT NULL,
			description TEXT,
			field_definitions TEXT DEFAULT '{}',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"custom_objects"),

		// 12. Custom Object Records
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			custom_object_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			title VARCHAR(255),
			data TEXT DEFAULT '{}',
			linked_entity_type VARCHAR(50),
			linked_entity_id BIGINT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"custom_object_records", p+"custom_objects"),

		// 13. Audit Logs
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			action VARCHAR(255) NOT NULL,
			entity_type VARCHAR(50) NOT NULL,
			entity_id BIGINT,
			performer_type VARCHAR(50),
			performer_id BIGINT,
			old_values TEXT,
			new_values TEXT,
			ip_address VARCHAR(45),
			user_agent VARCHAR(255),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"audit_logs"),

		// 14. Business Schedules
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
			hours TEXT NOT NULL DEFAULT '{}',
			is_default BOOLEAN NOT NULL DEFAULT FALSE,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"business_schedules"),

		// 15. Holidays
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			business_schedule_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			date DATE NOT NULL,
			is_recurring BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"holidays", p+"business_schedules"),

		// 16. Two Factors
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL,
			method VARCHAR(32) NOT NULL DEFAULT 'totp',
			secret VARCHAR(255),
			recovery_codes TEXT,
			is_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			verified_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"two_factors"),

		// 17. Workflows
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			trigger_event VARCHAR(255) NOT NULL,
			conditions TEXT NOT NULL DEFAULT '{}',
			actions TEXT NOT NULL DEFAULT '[]',
			position INTEGER NOT NULL DEFAULT 0,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			stop_on_match BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"workflows"),

		// 18. Workflow Logs
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			workflow_id BIGINT NOT NULL,
			ticket_id BIGINT NOT NULL,
			trigger_event VARCHAR(255) NOT NULL,
			status VARCHAR(32) NOT NULL,
			actions_executed TEXT DEFAULT '[]',
			error_message TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"workflow_logs"),

		// 19. Delayed Actions
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			workflow_id BIGINT NOT NULL,
			ticket_id BIGINT NOT NULL,
			action_data TEXT NOT NULL DEFAULT '{}',
			execute_at TIMESTAMP NOT NULL,
			executed BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"delayed_actions"),

		// 20. Attachments
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			reply_id BIGINT REFERENCES %s(id) ON DELETE SET NULL,
			original_filename VARCHAR(255) NOT NULL,
			mime_type VARCHAR(255) NOT NULL,
			size BIGINT NOT NULL DEFAULT 0,
			storage_path TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"attachments", p+"tickets", p+"replies"),

		// 21. Contacts (Pattern B — first-class identity for guest
		// requesters, deduped by email). See escalated-dev/escalated
		// docs/superpowers/plans/2026-04-24-public-tickets-rollout-status.md
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			email VARCHAR(320) NOT NULL UNIQUE,
			name VARCHAR(255),
			user_id BIGINT,
			metadata TEXT NOT NULL DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"contacts"),

		// NOTE: contact_id is already included in the tickets
		// CREATE TABLE above. Deployments that ran prior migrations
		// before this commit must manually add the column:
		//
		//     Postgres: ALTER TABLE escalated_tickets ADD COLUMN
		//               IF NOT EXISTS contact_id BIGINT;
		//     SQLite:   ALTER TABLE escalated_tickets ADD COLUMN
		//               contact_id INTEGER;  (pre-check column existence)
		//
		// The repo convention is fresh-install only; an operator-run
		// ALTER is intentional rather than baking a cross-dialect
		// IF-NOT-EXISTS into the migration runner.
	}

	// Indexes (CREATE INDEX IF NOT EXISTS is supported by PostgreSQL 9.5+ and SQLite 3.3+)
	indexes := []string{
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%sslp_name ON %s (name)", p, p+"sla_policies"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sslp_active ON %s (is_active)", p, p+"sla_policies"),

		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%sdept_slug ON %s (slug)", p, p+"departments"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%sdept_name ON %s (name)", p, p+"departments"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sdept_active ON %s (is_active)", p, p+"departments"),

		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%stag_name ON %s (name)", p, p+"tags"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%stag_slug ON %s (slug)", p, p+"tags"),

		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%stkt_ref ON %s (reference)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_status ON %s (status)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_priority ON %s (priority)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_type ON %s (ticket_type)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_requester ON %s (requester_type, requester_id)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_assigned ON %s (assigned_to)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_dept ON %s (department_id)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_sla ON %s (sla_breached)", p, p+"tickets"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%stkt_guest ON %s (guest_token)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_created ON %s (created_at)", p, p+"tickets"),

		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%srpl_ticket ON %s (ticket_id)", p, p+"replies"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%srpl_author ON %s (author_type, author_id)", p, p+"replies"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%srpl_internal ON %s (is_internal)", p, p+"replies"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%srpl_pinned ON %s (is_pinned)", p, p+"replies"),

		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sact_ticket ON %s (ticket_id)", p, p+"ticket_activities"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sact_action ON %s (action)", p, p+"ticket_activities"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sact_causer ON %s (causer_type, causer_id)", p, p+"ticket_activities"),

		// New parity indexes
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sec_dept ON %s (department_id)", p, p+"email_channels"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sec_active ON %s (is_active)", p, p+"email_channels"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%scf_slug ON %s (slug)", p, p+"custom_fields"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%scf_entity ON %s (entity_type)", p, p+"custom_fields"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%scfv_uniq ON %s (custom_field_id, entity_type, entity_id)", p, p+"custom_field_values"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%sco_slug ON %s (slug)", p, p+"custom_objects"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%scor_obj ON %s (custom_object_id)", p, p+"custom_object_records"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%scor_linked ON %s (linked_entity_type, linked_entity_id)", p, p+"custom_object_records"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sal_entity ON %s (entity_type, entity_id)", p, p+"audit_logs"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sal_performer ON %s (performer_type, performer_id)", p, p+"audit_logs"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sal_created ON %s (created_at)", p, p+"audit_logs"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%shol_sched ON %s (business_schedule_id)", p, p+"holidays"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stf_user ON %s (user_id)", p, p+"two_factors"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%swf_trigger ON %s (trigger_event)", p, p+"workflows"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%swf_active ON %s (is_active)", p, p+"workflows"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%swfl_wf ON %s (workflow_id)", p, p+"workflow_logs"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%swfl_tkt ON %s (ticket_id)", p, p+"workflow_logs"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sda_pending ON %s (executed, execute_at)", p, p+"delayed_actions"),

		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%satt_ticket ON %s (ticket_id)", p, p+"attachments"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%satt_reply ON %s (reply_id)", p, p+"attachments"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stkt_contact ON %s (contact_id)", p, p+"tickets"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%scontact_user ON %s (user_id)", p, p+"contacts"),
	}

	return append(stmts, indexes...)
}

// MigrateSQLite runs migrations with SQLite-compatible syntax.
// SQLite does not support BIGSERIAL, so this version uses INTEGER PRIMARY KEY.
func MigrateSQLite(db *sql.DB, prefix string) error {
	for _, stmt := range sqliteMigrationStatements(prefix) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration error: %w\nSQL: %s", err, stmt)
		}
	}
	return nil
}

func sqliteMigrationStatements(p string) []string {
	// Take the PostgreSQL statements and adapt for SQLite
	stmts := migrationStatements(p)
	var result []string
	for _, s := range stmts {
		s = strings.ReplaceAll(s, "BIGSERIAL", "INTEGER")
		s = strings.ReplaceAll(s, "BIGINT", "INTEGER")
		s = strings.ReplaceAll(s, "BOOLEAN", "INTEGER")
		s = strings.ReplaceAll(s, "VARCHAR(255)", "TEXT")
		s = strings.ReplaceAll(s, "VARCHAR(64)", "TEXT")
		s = strings.ReplaceAll(s, "VARCHAR(50)", "TEXT")
		s = strings.ReplaceAll(s, "VARCHAR(45)", "TEXT")
		s = strings.ReplaceAll(s, "VARCHAR(32)", "TEXT")
		s = strings.ReplaceAll(s, "VARCHAR(7)", "TEXT")
		s = strings.ReplaceAll(s, "VARCHAR(320)", "TEXT")
		result = append(result, s)
	}
	return result
}
