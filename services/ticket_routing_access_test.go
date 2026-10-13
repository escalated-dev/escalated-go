package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

func TestTicketAccessRoutingEngines(t *testing.T) {
	for _, engine := range []string{"automation", "escalation", "workflow"} {
		t.Run(engine, func(t *testing.T) {
			db := testdb.Open(t)
			var s store.Store
			if testdb.Driver(t) == "postgres" {
				s = store.NewPostgresStore(db, testdb.Prefix)
			} else {
				s = store.NewSQLiteStore(db, testdb.Prefix)
			}
			kind, id := "Company", "ABC"
			otherKind, otherID, otherType := "Project", "abc", "incident"
			tickets := []*models.Ticket{
				{Subject: "Match", Description: "Details", AccountType: &kind, AccountID: &id, TicketType: "question"},
				{Subject: "Wrong account", Description: "Details", AccountType: &kind, AccountID: &otherID, TicketType: "question"},
				{Subject: "Wrong model", Description: "Details", AccountType: &otherKind, AccountID: &id, TicketType: "question"},
				{Subject: "Wrong type", Description: "Details", AccountType: &kind, AccountID: &id, TicketType: otherType},
			}
			for _, ticket := range tickets {
				if err := s.CreateTicket(context.Background(), ticket); err != nil {
					t.Fatal(err)
				}
			}
			conditions := `[{"field":"account","operator":"equals","value":{"type":"Company","id":"ABC"}},{"field":"ticket_type","operator":"equals","value":"question"}]`
			actions := `[{"type":"change_priority","value":"3"}]`
			switch engine {
			case "automation":
				if _, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_automations (name,conditions,actions,active,position) VALUES (?,?,?,TRUE,0)`, "account", conditions, actions); err != nil {
					t.Fatal(err)
				}
				if count, err := NewAutomationRunner(db, nil).Run(); err != nil || count != 1 {
					t.Fatalf("automation: %d %v", count, err)
				}
			case "escalation":
				if _, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_escalation_rules (name,conditions,actions,is_active) VALUES (?,?,?,TRUE)`, "account", conditions, actions); err != nil {
					t.Fatal(err)
				}
				if count, err := NewEscalationService(db, nil).EvaluateRules(); err != nil || count != 1 {
					t.Fatalf("escalation: %d %v", count, err)
				}
			case "workflow":
				insertWorkflow(t, db, "ticket.created", `{"all":`+conditions+`}`, actions, true, false, 0)
				for _, ticket := range tickets {
					NewWorkflowRunner(db, nil).RunForEvent("ticket.created", ticket)
				}
			}
			for i, ticket := range tickets {
				current, err := s.GetTicket(context.Background(), ticket.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if i == 0 {
					want = 3
				}
				if current.Priority != want {
					t.Fatalf("ticket %s priority %d", ticket.Subject, current.Priority)
				}
			}
			for _, value := range []string{`{"id":"ABC"}`, `{"type":"Company"}`, `null`, `{`} {
				var condition WorkflowCondition
				raw, _ := json.Marshal(map[string]any{"field": "account", "operator": "not_equals", "value": value})
				if err := json.Unmarshal(raw, &condition); err != nil {
					t.Fatal(err)
				}
				if evaluateSingle(condition, ticketToData(tickets[0])) {
					t.Fatalf("malformed account matched: %s", value)
				}
				clause, _ := accountClause(value, "not_equals")
				if clause != "1=0" {
					t.Fatalf("malformed SQL condition: %s", clause)
				}
			}
		})
	}
}
func TestTicketAccessTargetedSLAClockAndResolvers(t *testing.T) {
	db := testdb.Open(t)
	var s store.Store
	if testdb.Driver(t) == "postgres" {
		s = store.NewPostgresStore(db, testdb.Prefix)
	} else {
		s = store.NewSQLiteStore(db, testdb.Prefix)
	}
	access, err := ticketaccess.New(db, testdb.Prefix, ticketaccess.Options{TargetedSLAs: true, Accounts: ticketaccess.AccountConfig{Enabled: true, Resolver: func(_ context.Context, ref ticketaccess.Reference) (*ticketaccess.Account, error) {
		return &ticketaccess.Account{Type: "Company", ID: ref.Kind}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	s.(interface{ ConfigureTicketAccess(*ticketaccess.Access) }).ConfigureTicketAccess(access)
	department := &models.Department{Name: "Support", Slug: "support", IsActive: true}
	if err := s.CreateDepartment(context.Background(), department); err != nil {
		t.Fatal(err)
	}
	kind := "incident"
	baseline := &models.SLAPolicy{Name: "Default", IsActive: true, IsDefault: true, FirstResponseHours: models.JSONText(`{"low":8}`), ResolutionHours: models.JSONText(`{}`)}
	targeted := &models.SLAPolicy{Name: "Targeted", IsActive: true, DepartmentID: &department.ID, TicketType: &kind, FirstResponseHours: models.JSONText(`{"low":1}`), ResolutionHours: models.JSONText(`{}`)}
	for _, policy := range []*models.SLAPolicy{baseline, targeted} {
		if err := s.CreateSLAPolicy(context.Background(), policy); err != nil {
			t.Fatal(err)
		}
	}
	requesterType := "User"
	requester := models.UserID("42")
	ticket, err := NewTicketService(s).Create(context.Background(), CreateTicketInput{Subject: "Targeted", Description: "Details", DepartmentID: &department.ID, TicketType: kind, RequesterType: &requesterType, RequesterID: &requester})
	if err != nil {
		t.Fatal(err)
	}
	if ticket.AccountID == nil || *ticket.AccountID != "requester" {
		t.Fatal("requester account missing")
	}
	if ticket.SLAPolicyID == nil || *ticket.SLAPolicyID != targeted.ID || ticket.SLAFirstResponseDueAt == nil || !ticket.SLAFirstResponseDueAt.Equal(ticket.CreatedAt.Add(time.Hour)) {
		t.Fatalf("SLA not applied: %+v", ticket)
	}
	ticket, err = s.GetTicket(context.Background(), ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	due := *ticket.SLAFirstResponseDueAt
	ticket.DepartmentID = nil
	ticket.TicketType = "question"
	if err := s.UpdateTicket(context.Background(), ticket); err != nil {
		t.Fatal(err)
	}
	current, _ := s.GetTicket(context.Background(), ticket.ID)
	if !current.SLAFirstResponseDueAt.Equal(due) || *current.SLAPolicyID != targeted.ID {
		t.Fatal("routing reset SLA")
	}
	if err := NewSLAService(s).ApplyDefaultPolicy(context.Background(), ticket.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetTicket(context.Background(), ticket.ID)
	if *current.SLAPolicyID != baseline.ID || !current.SLAFirstResponseDueAt.Equal(current.CreatedAt.Add(8*time.Hour)) {
		t.Fatal("reselection rebased clock")
	}
	standalone := &models.Ticket{Subject: "Subject mapping", Description: "Details", TicketType: "question"}
	if err := s.CreateTicket(context.Background(), standalone); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTicketSubjectService(s, nil, nil).AttachSubject(context.Background(), standalone.ID, "Project", "42", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetTicket(context.Background(), standalone.ID)
	if current.AccountID == nil || *current.AccountID != "subject" {
		t.Fatal("subject account missing")
	}
	if _, err := NewTicketSubjectService(s, nil, nil).AttachSubject(context.Background(), ticket.ID, "Project", "42", nil, nil, false); err != nil {
		t.Fatal(err)
	}
	current, _ = s.GetTicket(context.Background(), ticket.ID)
	if *current.AccountID != "requester" {
		t.Fatal("existing account replaced")
	}
	access.Options.TargetedSLAs = false
	legacy, err := NewTicketService(s).Create(context.Background(), CreateTicketInput{Subject: "Legacy", Description: "Details", DepartmentID: &department.ID, TicketType: kind})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.SLAPolicyID == nil || *legacy.SLAPolicyID != baseline.ID {
		t.Fatal("targeted SLA applied with option off")
	}
}
