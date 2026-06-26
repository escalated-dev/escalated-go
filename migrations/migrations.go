// Package migrations provides embedded SQL migrations for the Escalated ticket system.
// Call Migrate(db, prefix) to create all required tables.
package migrations

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// UserIDColumnType returns the SQL column type for a host user id.
// Default BIGINT (existing behavior). Set ESCALATED_USER_KEY_TYPE=uuid|string
// (or varchar) to use VARCHAR(255) for UUID/string-keyed hosts.
func UserIDColumnType() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ESCALATED_USER_KEY_TYPE"))) {
	case "uuid", "string", "varchar":
		return "VARCHAR(255)"
	default:
		return "BIGINT"
	}
}

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
	userCol := UserIDColumnType()
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
			requester_id %s,
			guest_name VARCHAR(255),
			guest_email VARCHAR(255),
			guest_token VARCHAR(64),
			contact_id BIGINT,
			assigned_to %s,
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
		)`, p+"tickets", userCol, userCol, p+"departments", p+"sla_policies"),

		// 5. Replies
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			body TEXT NOT NULL,
			author_type VARCHAR(255),
			author_id %s,
			is_internal BOOLEAN NOT NULL DEFAULT FALSE,
			is_system BOOLEAN NOT NULL DEFAULT FALSE,
			is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"replies", userCol, p+"tickets"),

		// 6. Ticket tags join table
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			tag_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			PRIMARY KEY (ticket_id, tag_id)
		)`, p+"ticket_tags", p+"tickets", p+"tags"),

		// 6a. Ticket followers join table — host users who follow a ticket and
		// are a notification target alongside the assignee/requester. See #72.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			user_id %s NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (ticket_id, user_id)
		)`, p+"ticket_followers", p+"tickets", userCol),

		// 6b. Ticket subjects — host entities a ticket is about (polymorphic).
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			subject_type VARCHAR(255) NOT NULL,
			subject_id VARCHAR(255) NOT NULL,
			role VARCHAR(255),
			position INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (ticket_id, subject_type, subject_id)
		)`, p+"ticket_subjects", p+"tickets"),

		// 7. Ticket activities
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			action VARCHAR(255) NOT NULL,
			causer_type VARCHAR(255),
			causer_id %s,
			details TEXT DEFAULT '{}',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"ticket_activities", userCol, p+"tickets"),

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
			performer_id %s,
			old_values TEXT,
			new_values TEXT,
			ip_address VARCHAR(45),
			user_agent VARCHAR(255),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"audit_logs", userCol),

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
			user_id %s NOT NULL,
			method VARCHAR(32) NOT NULL DEFAULT 'totp',
			secret VARCHAR(255),
			recovery_codes TEXT,
			is_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			verified_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"two_factors", userCol),

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
			user_id %s,
			metadata TEXT NOT NULL DEFAULT '{}',
			marketing_opt_out_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"contacts", userCol),

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

		// 22. Settings — key/value runtime configuration. Backs the
		// public-ticket guest policy (mode / user_id / signup URL
		// template) plus any future runtime-switchable config. Mirrors
		// the schema Symfony's EscalatedSetting and .NET's
		// EscalatedSettings use.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			key VARCHAR(255) NOT NULL,
			value TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"settings"),

		// 23–26. Skills (admin-managed routing + agent proficiency). See
		// escalated-developer-context/domain-model/skills-management.md.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			slug VARCHAR(100) NOT NULL,
			description TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"skills"),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			skill_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			tag_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			UNIQUE (skill_id, tag_id)
		)`, p+"skill_routing_tags", p+"skills", p+"tags"),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			skill_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			department_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			UNIQUE (skill_id, department_id)
		)`, p+"skill_routing_departments", p+"skills", p+"departments"),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			user_id %s NOT NULL,
			skill_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			proficiency SMALLINT NOT NULL DEFAULT 3 CHECK (proficiency >= 1 AND proficiency <= 5),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (user_id, skill_id)
		)`, p+"agent_skills", userCol, p+"skills"),

		// 28. Newsletter lists
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			kind VARCHAR(50) NOT NULL,
			filter_json TEXT,
			created_by %s,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"newsletter_lists", userCol),

		// 29. Newsletter list members
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			list_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			added_by %s,
			UNIQUE (list_id, contact_id)
		)`, p+"newsletter_list_members", p+"newsletter_lists", p+"contacts", userCol),

		// 30. Newsletter templates
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			theme VARCHAR(100) NOT NULL DEFAULT 'default',
			subject_template TEXT,
			body_markdown TEXT NOT NULL,
			merge_fields_schema TEXT,
			created_by %s,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"newsletter_templates", userCol),

		// 31. Newsletters
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			subject VARCHAR(255) NOT NULL,
			from_email VARCHAR(320) NOT NULL,
			from_name VARCHAR(255),
			reply_to VARCHAR(320),
			target_list_id BIGINT NOT NULL REFERENCES %s(id),
			template_id BIGINT REFERENCES %s(id) ON DELETE SET NULL,
			theme VARCHAR(100),
			body_markdown TEXT,
			status VARCHAR(50) NOT NULL DEFAULT 'draft',
			scheduled_at TIMESTAMP,
			sent_at TIMESTAMP,
			created_by %s,
			sent_by %s,
			summary_total INTEGER NOT NULL DEFAULT 0,
			summary_sent INTEGER NOT NULL DEFAULT 0,
			summary_opened INTEGER NOT NULL DEFAULT 0,
			summary_clicked INTEGER NOT NULL DEFAULT 0,
			summary_bounced INTEGER NOT NULL DEFAULT 0,
			summary_complained INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"newsletters", p+"newsletter_lists", p+"newsletter_templates", userCol, userCol),

		// 32. Newsletter deliveries
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			newsletter_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			contact_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			email_at_send VARCHAR(320) NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			tracking_token VARCHAR(64) NOT NULL UNIQUE,
			sent_at TIMESTAMP,
			opened_at TIMESTAMP,
			last_clicked_at TIMESTAMP,
			clicks_count INTEGER NOT NULL DEFAULT 0,
			bounce_reason TEXT,
			failure_reason TEXT,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			claimed_at TIMESTAMP,
			next_attempt_at TIMESTAMP,
			is_test BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"newsletter_deliveries", p+"newsletters", p+"contacts"),
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

		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stsub_subject ON %s (subject_type, subject_id)", p, p+"ticket_subjects"),

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

		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%ssettings_key ON %s (key)", p, p+"settings"),

		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%sskill_slug ON %s (slug)", p, p+"skills"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%sskill_name ON %s (name)", p, p+"skills"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%ssrt_skill ON %s (skill_id)", p, p+"skill_routing_tags"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%ssrt_tag ON %s (tag_id)", p, p+"skill_routing_tags"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%ssrd_skill ON %s (skill_id)", p, p+"skill_routing_departments"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%ssrd_dept ON %s (department_id)", p, p+"skill_routing_departments"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sas_user ON %s (user_id)", p, p+"agent_skills"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sas_skill ON %s (skill_id)", p, p+"agent_skills"),

		// Newsletter system
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%snl_kind ON %s (kind)", p, p+"newsletter_lists"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%snlm_contact ON %s (contact_id)", p, p+"newsletter_list_members"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%snlt_theme ON %s (theme)", p, p+"newsletter_templates"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sn_status ON %s (status)", p, p+"newsletters"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sn_sched ON %s (status, scheduled_at)", p, p+"newsletters"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%snd_nl_status ON %s (newsletter_id, status)", p, p+"newsletter_deliveries"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%snd_status_claimed ON %s (status, claimed_at)", p, p+"newsletter_deliveries"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%scontact_opt_out ON %s (marketing_opt_out_at)", p, p+"contacts"),
	}

	return append(append(stmts, indexes...), engineAddonStatements(p)...)
}

// engineAddonStatements returns the engine-domain tables (escalation,
// satisfaction, capacity, ticket links, side conversations) that previously
// shipped only as goose .sql files and were therefore NOT created by the
// inline Migrate path the library actually runs. Column types match those
// .sql definitions (INTEGER flags/counts, TEXT user columns) so the existing
// handlers behave identically. Mirrors the create_* .sql migrations.
func engineAddonStatements(p string) []string {
	return []string{
		// Escalation rules (time-based). sort_order/is_active avoid the SQL
		// reserved word `order`; the JSON contract exposes order/is_active.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			description TEXT,
			trigger_type VARCHAR(255),
			conditions TEXT NOT NULL DEFAULT '[]',
			actions TEXT NOT NULL DEFAULT '[]',
			sort_order INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"escalation_rules"),

		// Satisfaction ratings (CSAT) — one per ticket.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL,
			rating INTEGER NOT NULL,
			comment TEXT,
			rated_by_type TEXT,
			rated_by_id TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"satisfaction_ratings"),

		// Per-agent, per-channel concurrent capacity.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			user_id TEXT NOT NULL,
			channel VARCHAR(64) NOT NULL DEFAULT 'default',
			max_concurrent INTEGER NOT NULL DEFAULT 10,
			current_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"agent_capacity"),

		// Typed links between tickets.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			parent_ticket_id BIGINT NOT NULL,
			child_ticket_id BIGINT NOT NULL,
			link_type VARCHAR(32) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"ticket_links"),

		// Side conversation threads.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			ticket_id BIGINT NOT NULL,
			subject VARCHAR(255) NOT NULL,
			channel VARCHAR(32) NOT NULL DEFAULT 'internal',
			status VARCHAR(32) NOT NULL DEFAULT 'open',
			created_by TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"side_conversations"),

		// Side conversation replies.
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			side_conversation_id BIGINT NOT NULL,
			body TEXT NOT NULL,
			author_id TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`, p+"side_conversation_replies"),

		// Indexes
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sesc_active ON %s (is_active)", p, p+"escalation_rules"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%ssat_ticket ON %s (ticket_id)", p, p+"satisfaction_ratings"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%scap_user_channel ON %s (user_id, channel)", p, p+"agent_capacity"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%scap_user ON %s (user_id)", p, p+"agent_capacity"),
		fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_%stl_unique ON %s (parent_ticket_id, child_ticket_id, link_type)", p, p+"ticket_links"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stl_parent ON %s (parent_ticket_id)", p, p+"ticket_links"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%stl_child ON %s (child_ticket_id)", p, p+"ticket_links"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%ssc_ticket ON %s (ticket_id)", p, p+"side_conversations"),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%sscr_conv ON %s (side_conversation_id)", p, p+"side_conversation_replies"),
	}
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
		s = strings.ReplaceAll(s, "SMALLINT", "INTEGER")
		s = strings.ReplaceAll(s, "BOOLEAN", "INTEGER")
		s = strings.ReplaceAll(s, "VARCHAR(100)", "TEXT")
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
