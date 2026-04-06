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
		s = strings.ReplaceAll(s, "VARCHAR(7)", "TEXT")
		result = append(result, s)
	}
	return result
}
