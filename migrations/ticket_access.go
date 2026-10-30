package migrations

import (
	"database/sql"
	"fmt"
	"strings"
)

// upgradeTicketAccess is additive and idempotent for existing installations.
func upgradeTicketAccess(db *sql.DB, prefix string, postgres bool) error {
	collation := "BINARY"
	if postgres {
		collation = `"C"`
	}
	boolFalse := "INTEGER NOT NULL DEFAULT 0"
	if postgres {
		boolFalse = "BOOLEAN NOT NULL DEFAULT FALSE"
	}
	timestamp := "TIMESTAMP"
	if postgres {
		timestamp = "TIMESTAMPTZ"
	}
	columns := []struct{ table, name, definition string }{
		// Verified guest access (docs: guest grants). Nullable, so existing
		// tickets are untouched.
		{"tickets", "guest_verified_email", "VARCHAR(255)"},
		{"tickets", "guest_email_verified_at", timestamp},
		{"tickets", "guest_access_hash", "VARCHAR(64)"},
		{"tickets", "guest_access_expires_at", timestamp},
		{"tickets", "account_type", "VARCHAR(191) COLLATE " + collation},
		{"tickets", "account_id", "VARCHAR(191) COLLATE " + collation},
		{"sla_policies", "department_id", "BIGINT REFERENCES " + prefix + "departments(id) ON DELETE RESTRICT"},
		{"sla_policies", "ticket_type", "VARCHAR(50)"},
		// Business hours (Laravel business_hours_only plus the schedule a
		// policy or department counts against). Off and unset by default, so
		// existing policies keep calendar-hour deadlines.
		{"sla_policies", "business_hours_only", boolFalse},
		{"sla_policies", "business_schedule_id", "BIGINT REFERENCES " + prefix + "business_schedules(id) ON DELETE SET NULL"},
		{"departments", "business_schedule_id", "BIGINT REFERENCES " + prefix + "business_schedules(id) ON DELETE SET NULL"},
		// Custom field columns Laravel has and the original table lacked.
		// Nullable, so existing fields are untouched.
		{"custom_fields", "placeholder", "VARCHAR(255)"},
		{"custom_fields", "validation_rules", "TEXT"},
		{"custom_fields", "conditions", "TEXT"},
		// Custom ticket statuses: the sub-status a ticket is in, if any. The
		// built-in status column stays as it is; existing tickets get NULL.
		{"tickets", "ticket_status_id", "BIGINT REFERENCES " + prefix + "ticket_statuses(id) ON DELETE SET NULL"},
		// Widget chat tokens (services.GuestAccess.IssueChatToken): only the
		// token's SHA-256 and when it expires. Chats in progress keep NULLs
		// until their next request moves their token here.
		{"chat_sessions", "visitor_token_hash", "VARCHAR(64)"},
		{"chat_sessions", "visitor_token_expires_at", timestamp},
		// Two-factor (ADR 2026-10-29-configurable-access-features): the last
		// TOTP time step accepted, so a code cannot be used twice.
		{"two_factors", "last_used_step", "BIGINT"},
	}
	for _, column := range columns {
		exists := false
		table := prefix + column.table
		if postgres {
			if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name=$2)`, strings.ToLower(table), column.name).Scan(&exists); err != nil {
				return err
			}
		} else {
			rows, err := db.Query("PRAGMA table_info(" + table + ")")
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, notNull, pk int
				var name, kind string
				var defaultValue any
				if err := rows.Scan(&id, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
					rows.Close()
					return err
				}
				if name == column.name {
					exists = true
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		if !exists {
			if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return fmt.Errorf("ticket access migration: %w", err)
			}
		}
	}
	for _, query := range []string{
		"CREATE INDEX IF NOT EXISTS " + prefix + "tickets_account_idx ON " + prefix + "tickets(account_type,account_id)",
		"CREATE INDEX IF NOT EXISTS " + prefix + "sla_targets_idx ON " + prefix + "sla_policies(department_id,ticket_type)",
		// One-time guest email codes (only an HMAC digest of the code).
		"CREATE TABLE IF NOT EXISTS " + prefix + "guest_verifications (id VARCHAR(64) PRIMARY KEY, email VARCHAR(255) NOT NULL, purpose VARCHAR(16) NOT NULL, code_hash VARCHAR(64) NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, expires_at " + timestamp + " NOT NULL, used_at " + timestamp + ", created_at " + timestamp + " NOT NULL)",
		"CREATE INDEX IF NOT EXISTS " + prefix + "guest_verifications_expires_idx ON " + prefix + "guest_verifications(expires_at)",
		"CREATE INDEX IF NOT EXISTS " + prefix + "tickets_ticket_status_idx ON " + prefix + "tickets(ticket_status_id)",
		"CREATE UNIQUE INDEX IF NOT EXISTS " + prefix + "chat_sessions_visitor_token_idx ON " + prefix + "chat_sessions(visitor_token_hash)",
		"CREATE TABLE IF NOT EXISTS " + prefix + "department_agent (department_id BIGINT NOT NULL REFERENCES " + prefix + "departments(id) ON DELETE CASCADE, agent_id VARCHAR(255) NOT NULL, PRIMARY KEY(department_id,agent_id))",
	} {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return seedSystemStatuses(db, prefix, postgres)
}

// systemStatuses are the built-in statuses' system rows. They match
// models.BuiltInStatuses and models.BuiltInStatusCategory (this package cannot
// import models); the migration test checks they agree.
var systemStatuses = []struct{ slug, label, color, category string }{
	{"open", "Open", "#3B82F6", "open"},
	{"in_progress", "In Progress", "#8B5CF6", "open"},
	{"waiting_on_customer", "Waiting on Customer", "#F59E0B", "pending"},
	{"waiting_on_agent", "Waiting on Agent", "#F97316", "open"},
	{"escalated", "Escalated", "#EF4444", "open"},
	{"resolved", "Resolved", "#10B981", "solved"},
	{"closed", "Closed", "#6B7280", "solved"},
	{"reopened", "Reopened", "#3B82F6", "open"},
	{"snoozed", "Snoozed", "#A855F7", "on_hold"},
	{"live", "Live", "#22C55E", "open"},
}

// seedSystemStatuses inserts the system row of every built-in status that has
// none (matched by slug), so it is safe to run on every start. Positions count
// up within each category.
func seedSystemStatuses(db *sql.DB, prefix string, postgres bool) error {
	table := prefix + "ticket_statuses"
	insert := "INSERT INTO " + table + " (label, slug, category, color, position, is_default, is_system, created_at, updated_at) " +
		"SELECT ?, ?, ?, ?, ?, FALSE, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP WHERE NOT EXISTS (SELECT 1 FROM " + table + " WHERE slug = ?)"
	if postgres {
		insert = "INSERT INTO " + table + " (label, slug, category, color, position, is_default, is_system, created_at, updated_at) " +
			"SELECT $1, $2::VARCHAR, $3, $4, $5, FALSE, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP WHERE NOT EXISTS (SELECT 1 FROM " + table + " WHERE slug = $2::VARCHAR)"
	}
	positions := map[string]int{}
	for _, builtIn := range systemStatuses {
		position := positions[builtIn.category]
		positions[builtIn.category]++
		args := []any{builtIn.label, builtIn.slug, builtIn.category, builtIn.color, position, builtIn.slug}
		if postgres {
			args = args[:5]
		}
		if _, err := db.Exec(insert, args...); err != nil {
			return fmt.Errorf("seeding system statuses: %w", err)
		}
	}
	return nil
}
