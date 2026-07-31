package services

import (
	"context"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/store"
)

// End-to-end proof of the P0 fix: creating a ticket through the TicketService
// runs the wired WorkflowRunner, which fires a matching workflow and records a
// log row — all off the request path. Before the wiring, ts.Create fired
// webhooks but never touched a workflow, so no log was ever written.
func TestTicketServiceCreateFiresWorkflow(t *testing.T) {
	db := newWorkflowTestDB(t)
	ts := NewTicketService(store.NewSQLiteStore(db, "escalated_"))
	ts.Workflows = NewWorkflowRunner(db, discardLogger())

	insertWorkflow(t, db, "ticket.created", `{}`,
		`[{"type":"add_note","value":"Workflow saw {{reference}}"}]`, true, false, 0)

	ticket, err := ts.Create(context.Background(), CreateTicketInput{
		Subject:     "Need help",
		Description: "Please assist",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The runner fires in a goroutine so it never blocks or breaks the mutation;
	// poll briefly for its log row.
	var logCount int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = db.QueryRow(`SELECT COUNT(*) FROM escalated_workflow_logs WHERE ticket_id = ?`, ticket.ID).Scan(&logCount)
		if logCount == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if logCount != 1 {
		t.Fatalf("workflow_logs rows for ticket %d = %d, want 1 (workflow did not fire on ticket.created)", ticket.ID, logCount)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM escalated_workflow_logs WHERE ticket_id = ?`, ticket.ID).Scan(&status); err != nil {
		t.Fatalf("read log status: %v", err)
	}
	if status != "success" {
		t.Errorf("log status = %q, want success", status)
	}

	// The action ran: an internal note with the interpolated reference.
	var note string
	if err := db.QueryRow(
		`SELECT body FROM escalated_replies WHERE ticket_id = ? AND is_internal = TRUE`, ticket.ID,
	).Scan(&note); err != nil {
		t.Fatalf("read workflow note: %v", err)
	}
	if want := "Workflow saw " + ticket.Reference; note != want {
		t.Errorf("note body = %q, want %q", note, want)
	}
}
