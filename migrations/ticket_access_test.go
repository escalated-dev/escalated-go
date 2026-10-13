package migrations_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

func TestTicketAccessUpgradeAndCustomPrefixes(t *testing.T) {
	for _, prefix := range []string{"access_", "Mixed_"} {
		t.Run(prefix, func(t *testing.T) {
			db := testdb.Open(t)
			migrate := func() error {
				if testdb.Driver(t) == "postgres" {
					return migrations.Migrate(db, prefix)
				}
				return migrations.MigrateSQLite(db, prefix)
			}
			if err := migrate(); err != nil {
				t.Fatal(err)
			}
			// Reconstruct the previous nullable-column-free schema in this isolated fixture.
			for _, query := range []string{
				"DROP INDEX " + prefix + "tickets_account_idx", "DROP INDEX " + prefix + "sla_targets_idx",
				"ALTER TABLE " + prefix + "tickets DROP COLUMN account_type", "ALTER TABLE " + prefix + "tickets DROP COLUMN account_id",
				"ALTER TABLE " + prefix + "sla_policies DROP COLUMN department_id", "ALTER TABLE " + prefix + "sla_policies DROP COLUMN ticket_type",
				"DROP TABLE " + prefix + "department_agent",
				"INSERT INTO " + prefix + "tickets (reference,subject,description) VALUES ('OLD-1','Old ticket','Details')",
			} {
				if _, err := db.Exec(query); err != nil {
					t.Fatalf("%s: %v", query, err)
				}
			}
			if err := migrate(); err != nil {
				t.Fatal(err)
			}
			if err := migrate(); err != nil {
				t.Fatalf("second upgrade: %v", err)
			}
			var subject string
			var account sql.NullString
			if err := db.QueryRow("SELECT subject,account_id FROM "+prefix+"tickets WHERE reference='OLD-1'").Scan(&subject, &account); err != nil || subject != "Old ticket" || account.Valid {
				t.Fatalf("legacy row: %s %v %v", subject, account, err)
			}
			var s store.Store
			if testdb.Driver(t) == "postgres" {
				s = store.NewPostgresStore(db, prefix)
			} else {
				s = store.NewSQLiteStore(db, prefix)
			}
			department := &models.Department{Name: "Own", Slug: "own", IsActive: true}
			if err := s.CreateDepartment(context.Background(), department); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(sqldialect.Rebind(db, "INSERT INTO "+prefix+"department_agent(department_id,agent_id) VALUES (?,?)"), department.ID, "agent-uuid"); err != nil {
				t.Fatal(err)
			}
			kind, id := "Company", "ABC"
			ticket := &models.Ticket{Subject: "Scoped", Description: "Details", TicketType: "question", DepartmentID: &department.ID, AccountType: &kind, AccountID: &id}
			if err := s.CreateTicket(context.Background(), ticket); err != nil {
				t.Fatal(err)
			}
			policy, err := ticketaccess.New(db, prefix, ticketaccess.Options{Visibility: "department"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, _ := policy.ForUser(context.Background(), ticketaccess.User{ID: "agent-uuid", Agent: true})
			if list, total, err := s.ListTickets(ctx, models.TicketFilters{}); err != nil || total != 1 || len(list) != 1 || list[0].ID != ticket.ID {
				t.Fatalf("prefixed scope: %d %v", total, err)
			}
			var scopedCount int
			if err := ticketaccess.QueryRowContext(ctx, db, "SELECT COUNT(*) FROM escalated_tickets").Scan(&scopedCount); err != nil || scopedCount != 1 {
				t.Fatalf("direct prefixed query: %d %v", scopedCount, err)
			}
			admin, _ := policy.ForUser(ctx, ticketaccess.User{ID: "admin", Admin: true})
			if err := ticketaccess.QueryRowContext(admin, db, "SELECT COUNT(*) FROM escalated_tickets").Scan(&scopedCount); err != nil || scopedCount != 2 {
				t.Fatalf("admin prefixed query: %d %v", scopedCount, err)
			}
			var count int
			if err := db.QueryRow(sqldialect.Rebind(db, "SELECT COUNT(*) FROM "+prefix+"tickets WHERE account_type=? AND account_id=?"), kind, "abc").Scan(&count); err != nil || count != 0 {
				t.Fatalf("account collation: %d %v", count, err)
			}
		})
	}
}
