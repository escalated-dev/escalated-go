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
		"CREATE TABLE IF NOT EXISTS " + prefix + "department_agent (department_id BIGINT NOT NULL REFERENCES " + prefix + "departments(id) ON DELETE CASCADE, agent_id VARCHAR(255) NOT NULL, PRIMARY KEY(department_id,agent_id))",
	} {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}
