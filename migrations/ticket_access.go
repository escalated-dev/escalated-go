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
	columns := []struct{ table, name, definition string }{
		{"tickets", "account_type", "VARCHAR(191) COLLATE " + collation},
		{"tickets", "account_id", "VARCHAR(191) COLLATE " + collation},
		{"sla_policies", "department_id", "BIGINT REFERENCES " + prefix + "departments(id) ON DELETE RESTRICT"},
		{"sla_policies", "ticket_type", "VARCHAR(50)"},
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
		"CREATE TABLE IF NOT EXISTS " + prefix + "department_agent (department_id BIGINT NOT NULL REFERENCES " + prefix + "departments(id) ON DELETE CASCADE, agent_id VARCHAR(255) NOT NULL, PRIMARY KEY(department_id,agent_id))",
	} {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}
