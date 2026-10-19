package router_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/escalated-dev/escalated-go/services"

	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

func accessFixture(t *testing.T) (*guardFixture, *models.Ticket) {
	t.Helper()
	f := newGuardFixture(t)
	access, err := ticketaccess.New(f.db, "escalated_", ticketaccess.Options{Visibility: "department"})
	if err != nil {
		t.Fatal(err)
	}
	f.esc.Access = access
	f.esc.Store.(interface{ ConfigureTicketAccess(*ticketaccess.Access) }).ConfigureTicketAccess(access)
	department := &models.Department{Name: "Own", Slug: "own", IsActive: true}
	if err := f.esc.Store.CreateDepartment(context.Background(), department); err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`CREATE TABLE IF NOT EXISTS escalated_department_agent (department_id BIGINT NOT NULL, agent_id VARCHAR(255) NOT NULL, PRIMARY KEY(department_id,agent_id))`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(sqldialect.Rebind(f.db, `INSERT INTO escalated_department_agent (department_id,agent_id) VALUES (?,?)`), department.ID, "900")
	if err != nil {
		t.Fatal(err)
	}
	visible := &models.Ticket{Subject: "Visible", Description: "Details", TicketType: "question", DepartmentID: &department.ID}
	if err := f.esc.Store.CreateTicket(context.Background(), visible); err != nil {
		t.Fatal(err)
	}
	return f, visible
}

func TestTicketAccessListAndSearch(t *testing.T) {
	for _, search := range []string{"", "?search=customer"} {
		t.Run(search, func(t *testing.T) {
			f, visible := accessFixture(t)
			for name, mount := range f.mounts() {
				t.Run(name, func(t *testing.T) {
					response := send(mount, "GET", "/escalated/api/tickets"+search, "900", "agent", "")
					if response.Code != 200 {
						t.Fatalf("status %d: %s", response.Code, response.Body)
					}
					var body struct {
						Tickets []models.Ticket
						Total   int
					}
					if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
						t.Fatal(err)
					}
					want := 1
					if search != "" {
						want = 0
					}
					if body.Total != want || len(body.Tickets) != want {
						t.Fatalf("private ticket exposed: %s", response.Body)
					}
					if want == 1 && body.Tickets[0].ID != visible.ID {
						t.Fatal("wrong ticket")
					}
				})
			}
		})
	}
}
func TestTicketAccessDetailUpdateAndReply(t *testing.T) {
	for _, action := range []struct{ method, suffix, body string }{
		{"GET", "", ""}, {"PATCH", "", `{"subject":"Forbidden"}`}, {"POST", "/replies", `{"body":"Forbidden"}`},
	} {
		t.Run(action.method, func(t *testing.T) {
			f, _ := accessFixture(t)
			for name, mount := range f.mounts() {
				t.Run(name, func(t *testing.T) {
					response := send(mount, action.method, fmt.Sprintf("/escalated/api/tickets/%d%s", f.ticketOfB, action.suffix), "900", "agent", action.body)
					if response.Code != http.StatusNotFound {
						t.Fatalf("want 404, got %d: %s", response.Code, response.Body)
					}
				})
			}
		})
	}
}
func TestTicketAccessReportQuery(t *testing.T) {
	f, _ := accessFixture(t)
	handler := handlers.NewReportHandler(f.db, "escalated_")
	response := send(f.esc.TicketAccessMiddleware(http.HandlerFunc(handler.Index)), "GET", "/report", "900", "agent", "")
	var body struct {
		Total int `json:"total_tickets"`
	}
	if response.Code != 200 {
		t.Fatalf("status %d: %s", response.Code, response.Body)
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 {
		t.Fatalf("unscoped report: %s", response.Body)
	}
}

func TestTicketAccessWidget(t *testing.T) {
	f, _ := accessFixture(t)
	ticket, err := f.esc.Store.GetTicket(context.Background(), f.ticketOfB)
	if err != nil {
		t.Fatal(err)
	}
	token := "private-widget-token"
	if _, err := f.db.Exec(sqldialect.Rebind(f.db, "UPDATE escalated_tickets SET guest_token=? WHERE id=?"), token, ticket.ID); err != nil {
		t.Fatal(err)
	}
	ticket.GuestToken = &token
	if err := f.esc.Store.UpdateTicket(context.Background(), ticket); err != nil {
		t.Fatal(err)
	}
	widget := handlers.NewWidgetHandler(handlers.WidgetConfig{Enabled: true}, f.esc.Store, services.NewTicketService(f.esc.Store))
	handler := f.esc.TicketAccessMiddleware(http.HandlerFunc(widget.LookupTicket))
	path := "/widget/tickets/lookup?ref=" + ticket.Reference + "&token=" + token
	if response := send(handler, "GET", path, "", "", ""); response.Code != 200 {
		t.Fatalf("guest: %d %s", response.Code, response.Body)
	}
	if response := send(handler, "GET", path, "900", "agent", ""); response.Code != 404 {
		t.Fatalf("agent: %d %s", response.Code, response.Body)
	}

	// A request from an allowed embedding site always runs as a guest, even
	// when the browser also carries a signed-in agent's identity.
	embedded := handlers.WidgetCORS(handlers.WidgetConfig{Enabled: true, AllowedOrigins: "https://shop.example.com"}, f.esc.Store)(handler)
	request := httptest.NewRequest("GET", path, nil)
	request.Header.Set(headerUser, "900")
	request.Header.Set(headerRole, "agent")
	request.Header.Set("Origin", "https://shop.example.com")
	response := httptest.NewRecorder()
	embedded.ServeHTTP(response, request)
	if response.Code != 200 || response.Header().Get("Access-Control-Allow-Origin") != "https://shop.example.com" {
		t.Fatalf("embedded agent: %d %s", response.Code, response.Body)
	}
	request.Header.Set("Origin", "https://evil.example.net")
	response = httptest.NewRecorder()
	embedded.ServeHTTP(response, request)
	if response.Code != 404 || response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("agent from a site that is not allowed: %d %s", response.Code, response.Body)
	}
}
func TestTicketAccessBulkAndPreloadedWrites(t *testing.T) {
	f, visible := accessFixture(t)
	ctx, err := f.esc.Access.ForUser(context.Background(), ticketaccess.User{ID: "900", Agent: true})
	if err != nil {
		t.Fatal(err)
	}
	private, err := f.esc.Store.GetTicket(context.Background(), f.ticketOfB)
	if err != nil {
		t.Fatal(err)
	}
	private.Subject = "Forbidden"
	if err := f.esc.Store.UpdateTicket(ctx, private); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("preloaded update: %v", err)
	}
	result, err := ticketaccess.ExecContext(ctx, f.db, sqldialect.Rebind(f.db, "UPDATE escalated_tickets SET priority=? WHERE id=? OR id=?"), models.PriorityHigh, visible.ID, private.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		t.Fatalf("affected %d", count)
	}
	unchanged, _ := f.esc.Store.GetTicket(context.Background(), private.ID)
	if unchanged.Subject == "Forbidden" || unchanged.Priority == models.PriorityHigh {
		t.Fatal("private ticket modified")
	}
	reply := &models.Reply{TicketID: private.ID, Body: "Forbidden"}
	if err := f.esc.Store.CreateReply(ctx, reply); err == nil {
		t.Fatal("private reply accepted")
	}
	reply = &models.Reply{TicketID: visible.ID, Body: "Visible"}
	if err := f.esc.Store.CreateReply(ctx, reply); err != nil {
		t.Fatal(err)
	}
	if _, err := ticketaccess.ExecContext(ctx, f.db, sqldialect.Rebind(f.db, "UPDATE escalated_replies SET ticket_id=? WHERE id=?"), private.ID, reply.ID); err == nil {
		t.Fatal("child moved to inaccessible parent")
	}
}
func TestTicketAccessRelatedRecordsAndOR(t *testing.T) {
	f, visible := accessFixture(t)
	if _, err := f.db.Exec(sqldialect.Rebind(f.db, "INSERT INTO escalated_ticket_links (parent_ticket_id,child_ticket_id,link_type,created_at,updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)"), visible.ID, f.ticketOfB, "related"); err != nil {
		t.Fatal(err)
	}
	ctx, err := f.esc.Access.ForUser(context.Background(), ticketaccess.User{ID: "900", Agent: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"replies", "ticket_activities", "attachments", "ticket_links"} {
		var count int
		if err := ticketaccess.QueryRowContext(ctx, f.db, "SELECT COUNT(*) FROM escalated_"+table+" WHERE 1=1 OR 1=1").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s exposes %d private records", table, count)
		}
	}
	var count int
	if err := ticketaccess.QueryRowContext(ctx, f.db, sqldialect.Rebind(f.db, "SELECT COUNT(*) FROM escalated_tickets WHERE id=? OR id=?"), visible.ID, f.ticketOfB).Scan(&count); err != nil || count != 1 {
		t.Fatalf("OR scope: %d %v", count, err)
	}
}
func TestTicketAccessAccountsIntersectAndUseExactIdentity(t *testing.T) {
	f, visible := accessFixture(t)
	account := &ticketaccess.Account{Type: "Company", ID: "ABC"}
	f.esc.Access.Options.Accounts = ticketaccess.AccountConfig{Enabled: true, ScopeAgents: true, Resolver: func(context.Context, ticketaccess.Reference) (*ticketaccess.Account, error) { return account, nil }}
	for _, id := range []int64{visible.ID, f.ticketOfB} {
		ticket, _ := f.esc.Store.GetTicket(context.Background(), id)
		ticket.AccountType = &account.Type
		ticket.AccountID = &account.ID
		if err := f.esc.Store.UpdateTicket(context.Background(), ticket); err != nil {
			t.Fatal(err)
		}
	}
	ctx, err := f.esc.Access.ForUser(context.Background(), ticketaccess.User{ID: "900", Agent: true})
	if err != nil {
		t.Fatal(err)
	}
	tickets, total, err := f.esc.Store.ListTickets(ctx, models.TicketFilters{AccountType: &account.Type, AccountID: &account.ID, Search: "Visible"})
	if err != nil || total != 1 || len(tickets) != 1 {
		t.Fatalf("combined scope: %d %v", total, err)
	}
	lower := &ticketaccess.Account{Type: "Company", ID: "abc"}
	account = lower
	ctx, _ = f.esc.Access.ForUser(context.Background(), ticketaccess.User{ID: "900", Agent: true})
	if _, total, err := f.esc.Store.ListTickets(ctx, models.TicketFilters{}); err != nil || total != 0 {
		t.Fatalf("case mismatch: %d %v", total, err)
	}
	account = nil
	ctx, _ = f.esc.Access.ForUser(context.Background(), ticketaccess.User{ID: "900", Agent: true})
	if _, total, err := f.esc.Store.ListTickets(ctx, models.TicketFilters{}); err != nil || total != 0 {
		t.Fatalf("missing account: %d %v", total, err)
	}
	admin, _ := f.esc.Access.ForUser(ctx, ticketaccess.User{ID: "1", Admin: true})
	if _, total, err := f.esc.Store.ListTickets(admin, models.TicketFilters{}); err != nil || total != 2 {
		t.Fatalf("admin: %d %v", total, err)
	}
}
func TestTicketAccessConcurrentRequests(t *testing.T) {
	f, _ := accessFixture(t)
	for name, mount := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			var group sync.WaitGroup
			for i := 0; i < 12; i++ {
				group.Add(1)
				go func(index int) {
					defer group.Done()
					role := "agent"
					want := 1
					if index%2 == 0 {
						role = "admin"
						want = 2
					}
					response := send(mount, "GET", "/escalated/api/tickets", "900", role, "")
					var body struct{ Total int }
					err := json.Unmarshal(response.Body.Bytes(), &body)
					if response.Code != 200 || err != nil || body.Total != want {
						t.Errorf("%s: %d %s", role, response.Code, response.Body)
					}
				}(i)
			}
			group.Wait()
		})
	}
}
func TestTicketAccessLiveDeliveryAndRecipientRevocation(t *testing.T) {
	f, visible := accessFixture(t)
	f.esc.Access.Options.ResolveUser = func(_ context.Context, id models.UserID) (*ticketaccess.User, error) {
		return &ticketaccess.User{ID: id, Agent: id == "900", Admin: id == "1"}, nil
	}
	b := services.NewBroadcaster(services.BroadcastConfig{Enabled: true, TicketAccess: f.esc.Access}, nil)
	sub, err := b.Subscribe("user-900", []string{services.AgentChannel(), services.TicketChannel(f.ticketOfB)})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Channels[services.TicketChannel(f.ticketOfB)] {
		t.Fatal("subscribed to private ticket")
	}
	if count, err := b.Publish(services.AgentChannel(), "updated", map[string]any{"ticket_id": visible.ID}); err != nil || count != 1 {
		t.Fatalf("initial: %d %v", count, err)
	}
	event := <-sub.Events
	if count, err := b.Publish(services.AgentChannel(), "updated", map[string]any{"ticket_id": f.ticketOfB}); err != nil || count != 0 {
		t.Fatalf("private: %d %v", count, err)
	}
	if err := services.NewAssignmentService(f.esc.Store).Reassign(context.Background(), f.ticketOfB, "900", nil); err == nil {
		t.Fatal("assigned private ticket")
	}
	if ids := f.esc.Access.Recipients(context.Background(), f.ticketOfB, []models.UserID{"900", "1"}); len(ids) != 1 || ids[0] != "1" {
		t.Fatalf("recipients: %v", ids)
	}
	if _, err := f.db.Exec("DELETE FROM escalated_department_agent"); err != nil {
		t.Fatal(err)
	}
	if b.CanDeliver("user-900", event) {
		t.Fatal("buffered event delivered after revocation")
	}
	if count, err := b.Publish(services.AgentChannel(), "updated", map[string]any{"ticket_id": visible.ID}); err != nil || count != 0 {
		t.Fatalf("revoked: %d %v", count, err)
	}
}

func TestTicketAccessNestedWriteQueries(t *testing.T) {
	f, visible := accessFixture(t)
	ctx, _ := f.esc.Access.ForUser(context.Background(), ticketaccess.User{ID: "900", Agent: true})
	query := "UPDATE escalated_tickets SET subject=COALESCE((SELECT subject FROM escalated_tickets WHERE id=?), 'No private subject') WHERE id=?"
	if _, err := ticketaccess.ExecContext(ctx, f.db, sqldialect.Rebind(f.db, query), f.ticketOfB, visible.ID); err != nil {
		t.Fatal(err)
	}
	current, _ := f.esc.Store.GetTicket(context.Background(), visible.ID)
	if current.Subject != "No private subject" {
		t.Fatal("write subquery exposed a private subject")
	}
}
