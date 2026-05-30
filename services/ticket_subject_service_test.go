package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

type fakeProject struct {
	id, name, account string
}

func (p fakeProject) TicketSubjectTitle() string     { return p.name }
func (p fakeProject) TicketSubjectSubtitle() *string { s := "Project · " + p.account; return &s }
func (p fakeProject) TicketSubjectURL() *string      { s := "https://app.test/projects/" + p.id; return &s }
func (p fakeProject) TicketSubjectColor() *string    { s := "#2563eb"; return &s }
func (p fakeProject) TicketSubjectIcon() *string     { s := "folder"; return &s }

func testSubjectStore(t *testing.T) (store.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	return store.NewSQLiteStore(db, "escalated_"), db
}

func testResolver(projects map[string]fakeProject) func(string, string) (models.TicketSubject, bool) {
	return func(subjectType, subjectID string) (models.TicketSubject, bool) {
		if subjectType != "Project" {
			return nil, false
		}
		p, ok := projects[subjectID]
		if !ok {
			return nil, false
		}
		return p, true
	}
}

func TestAttachSubjectIdempotent(t *testing.T) {
	s, db := testSubjectStore(t)
	defer db.Close()

	ctx := context.Background()
	ticket := &models.Ticket{Reference: "T-1", Subject: "Help", Description: "x", Status: models.StatusOpen}
	if err := s.CreateTicket(ctx, ticket); err != nil {
		t.Fatal(err)
	}

	projects := map[string]fakeProject{"prj_9f1c": {id: "prj_9f1c", name: "Acme Redesign", account: "Acme"}}
	ss := NewTicketSubjectService(s, []string{"Project"}, testResolver(projects))
	role := "project"

	link1, err := ss.AttachSubject(ctx, ticket.ID, "Project", "prj_9f1c", &role, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	account := "account"
	link2, err := ss.AttachSubject(ctx, ticket.ID, "Project", "prj_9f1c", &account, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if link1.ID != link2.ID {
		t.Fatalf("expected same link id, got %d and %d", link1.ID, link2.ID)
	}
	links, err := s.ListTicketSubjectLinks(ctx, ticket.ID)
	if err != nil || len(links) != 1 {
		t.Fatalf("expected 1 link, got %v err %v", links, err)
	}
	if links[0].Role == nil || *links[0].Role != "account" {
		t.Errorf("expected role account, got %v", links[0].Role)
	}
}

func TestSerializeTicketSubjects(t *testing.T) {
	s, db := testSubjectStore(t)
	defer db.Close()

	ctx := context.Background()
	ticket := &models.Ticket{Reference: "T-2", Subject: "Help", Description: "x", Status: models.StatusOpen}
	_ = s.CreateTicket(ctx, ticket)

	projects := map[string]fakeProject{
		"7": {id: "7", name: "Acme Redesign", account: "Acme"},
	}
	ss := NewTicketSubjectService(s, []string{"Project"}, testResolver(projects))
	role := "project"
	_, _ = ss.AttachSubject(ctx, ticket.ID, "Project", "7", &role, nil, false)

	views, err := ss.ListViews(ctx, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("expected 1 view, got %d", len(views))
	}
	v := views[0]
	if v.Title != "Acme Redesign" || v.Missing {
		t.Errorf("unexpected view: %+v", v)
	}
	if v.Subtitle == nil || *v.Subtitle != "Project · Acme" {
		t.Errorf("subtitle: %v", v.Subtitle)
	}
}

func TestSyncSubjects(t *testing.T) {
	s, db := testSubjectStore(t)
	defer db.Close()

	ctx := context.Background()
	ticket := &models.Ticket{Reference: "T-3", Subject: "Help", Description: "x", Status: models.StatusOpen}
	_ = s.CreateTicket(ctx, ticket)

	projects := map[string]fakeProject{
		"a": {id: "a", name: "A"},
		"b": {id: "b", name: "B"},
		"c": {id: "c", name: "C"},
	}
	ss := NewTicketSubjectService(s, []string{"Project"}, testResolver(projects))
	_, _ = ss.AttachSubject(ctx, ticket.ID, "Project", "a", nil, nil, false)

	primary := "primary"
	err := ss.SyncSubjects(ctx, ticket.ID, []TicketSubjectRef{
		{Type: "Project", ID: "b", Role: &primary},
		{Type: "Project", ID: "c"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	links, _ := s.ListTicketSubjectLinks(ctx, ticket.ID)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0].SubjectID != "b" || links[0].Position != 0 {
		t.Errorf("first link: %+v", links[0])
	}
	if links[1].SubjectID != "c" || links[1].Position != 1 {
		t.Errorf("second link: %+v", links[1])
	}
}

func TestAllowlistEnforcement(t *testing.T) {
	s, db := testSubjectStore(t)
	defer db.Close()

	ctx := context.Background()
	ticket := &models.Ticket{Reference: "T-4", Subject: "Help", Description: "x", Status: models.StatusOpen}
	_ = s.CreateTicket(ctx, ticket)

	ss := NewTicketSubjectService(s, []string{"Other"}, nil)
	_, err := ss.AttachSubject(ctx, ticket.ID, "Project", "1", nil, nil, false)
	if !errors.Is(err, ErrTicketSubjectTypeNotAllowed) {
		t.Fatalf("expected ErrTicketSubjectTypeNotAllowed, got %v", err)
	}

	ssOpen := NewTicketSubjectService(s, nil, nil)
	_, err = ssOpen.AttachSubject(ctx, ticket.ID, "Project", "1", nil, nil, true)
	if !errors.Is(err, ErrTicketSubjectAPIDisabled) {
		t.Fatalf("expected ErrTicketSubjectAPIDisabled, got %v", err)
	}
}
