package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

func subjectHandlerFixture(t *testing.T) (*TicketSubjectHandler, *APIHandler, store.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	s := store.NewSQLiteStore(db, "escalated_")
	resolver := func(subjectType, subjectID string) (models.TicketSubject, bool) {
		if subjectType != "Project" {
			return nil, false
		}
		return fakeHandlerProject{id: subjectID, name: "Acme"}, true
	}
	subjectSvc := services.NewTicketSubjectService(s, []string{"Project"}, resolver)
	ticketSvc := services.NewTicketService(s)
	apiH := NewAPIHandler(s, ticketSvc, renderer.NewJSONRenderer(), func(_ *http.Request) models.UserID { return "" })
	apiH.Subjects = subjectSvc
	return NewTicketSubjectHandler(subjectSvc, ticketSvc), apiH, s, db
}

type fakeHandlerProject struct {
	id, name string
}

func (p fakeHandlerProject) TicketSubjectTitle() string     { return p.name }
func (p fakeHandlerProject) TicketSubjectSubtitle() *string { return nil }
func (p fakeHandlerProject) TicketSubjectURL() *string      { return nil }
func (p fakeHandlerProject) TicketSubjectColor() *string    { return nil }
func (p fakeHandlerProject) TicketSubjectIcon() *string     { return nil }

func TestAttachDetachSubjectHTTP(t *testing.T) {
	h, apiH, s, db := subjectHandlerFixture(t)
	defer db.Close()

	ctx := context.Background()
	ticket := &models.Ticket{
		Reference:   "HT-1",
		Subject:     "Help",
		Description: "body",
		Status:      models.StatusOpen,
		Metadata:    json.RawMessage(`{}`),
	}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}

	tid := strconv.FormatInt(ticket.ID, 10)
	body := bytes.NewBufferString(`{"type":"Project","id":"42","role":"project"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tickets/"+tid+"/subjects", body)
	req.SetPathValue("id", tid)
	rec := httptest.NewRecorder()
	h.AttachSubject(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach: expected 200, got %d %s", rec.Code, rec.Body.String())
	}

	var attachResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &attachResp)
	subject := attachResp["subject"].(map[string]any)
	linkID := int64(subject["id"].(float64))

	delReq := httptest.NewRequest(http.MethodDelete, "/api/tickets/"+tid+"/subjects/"+strconv.FormatInt(linkID, 10), nil)
	delReq.SetPathValue("id", tid)
	delReq.SetPathValue("subject", strconv.FormatInt(linkID, 10))
	delRec := httptest.NewRecorder()
	h.DetachSubject(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("detach: expected 200, got %d %s", delRec.Code, delRec.Body.String())
	}

	showReq := httptest.NewRequest(http.MethodGet, "/api/tickets/"+tid, nil)
	showReq.SetPathValue("id", tid)
	showRec := httptest.NewRecorder()
	apiH.ShowTicket(showRec, showReq)
	if showRec.Code != http.StatusOK {
		t.Fatalf("show: expected 200, got %d", showRec.Code)
	}
	var showResp map[string]any
	_ = json.Unmarshal(showRec.Body.Bytes(), &showResp)
	tkt := showResp["ticket"].(map[string]any)
	subjects, _ := tkt["subjects"].([]any)
	if len(subjects) != 0 {
		t.Errorf("expected empty subjects on ticket, got %v", subjects)
	}
}
