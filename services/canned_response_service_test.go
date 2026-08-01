package services

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

func newCannedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Single connection so the in-memory schema persists across queries.
	db.SetMaxOpenConns(1)
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func strptr(s string) *string { return &s }

// Create then FindByID must round-trip every field, including the nullable
// category, and assign an id.
func TestCannedResponseCreateAndFind(t *testing.T) {
	svc := NewCannedResponseService(newCannedTestDB(t), nil)

	agent := models.UserID("7")
	c := &models.CannedResponse{
		Title:     "Greeting",
		Body:      "Hello, how can we help?",
		Category:  strptr("Onboarding"),
		IsShared:  true,
		CreatedBy: &agent,
	}
	if err := svc.Create(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected a non-zero id after create")
	}

	got, err := svc.FindByID(c.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Title != "Greeting" || got.Body != "Hello, how can we help?" {
		t.Errorf("title/body mismatch: %+v", got)
	}
	if got.Category == nil || *got.Category != "Onboarding" {
		t.Errorf("category mismatch: %v", got.Category)
	}
	if !got.IsShared {
		t.Error("expected is_shared true")
	}
	if got.CreatedBy == nil || *got.CreatedBy != agent {
		t.Errorf("created_by mismatch: %v", got.CreatedBy)
	}
}

// A NULL category column must scan back to a nil pointer, not an empty string.
func TestCannedResponseNullCategory(t *testing.T) {
	svc := NewCannedResponseService(newCannedTestDB(t), nil)

	c := &models.CannedResponse{Title: "No cat", Body: "body", IsShared: true}
	if err := svc.Create(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := svc.FindByID(c.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Category != nil {
		t.Errorf("expected nil category, got %q", *got.Category)
	}
}

// ListForAgent returns shared responses plus the agent's own private ones,
// but never another agent's private response.
func TestCannedResponseListForAgent(t *testing.T) {
	svc := NewCannedResponseService(newCannedTestDB(t), nil)

	mine := models.UserID("1")
	other := models.UserID("2")

	must := func(c *models.CannedResponse) {
		t.Helper()
		if err := svc.Create(c); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	must(&models.CannedResponse{Title: "Shared", Body: "b", IsShared: true, CreatedBy: &other})
	must(&models.CannedResponse{Title: "Mine private", Body: "b", IsShared: false, CreatedBy: &mine})
	must(&models.CannedResponse{Title: "Other private", Body: "b", IsShared: false, CreatedBy: &other})

	list, err := svc.ListForAgent(mine)
	if err != nil {
		t.Fatalf("list for agent: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 visible responses, got %d: %+v", len(list), list)
	}
	titles := map[string]bool{}
	for _, c := range list {
		titles[c.Title] = true
	}
	if !titles["Shared"] || !titles["Mine private"] {
		t.Errorf("expected Shared + Mine private, got %v", titles)
	}
	if titles["Other private"] {
		t.Error("agent must not see another agent's private response")
	}

	// ListAll is unscoped and returns everything.
	all, err := svc.ListAll()
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 total responses, got %d", len(all))
	}
}

// Update persists edits; Delete removes the row (FindByID then errors).
func TestCannedResponseUpdateAndDelete(t *testing.T) {
	svc := NewCannedResponseService(newCannedTestDB(t), nil)

	c := &models.CannedResponse{Title: "Draft", Body: "b", Category: strptr("A"), IsShared: true}
	if err := svc.Create(c); err != nil {
		t.Fatalf("create: %v", err)
	}

	c.Title = "Final"
	c.Category = strptr("B")
	c.IsShared = false
	if err := svc.Update(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := svc.FindByID(c.ID)
	if err != nil {
		t.Fatalf("find after update: %v", err)
	}
	if got.Title != "Final" || got.Category == nil || *got.Category != "B" || got.IsShared {
		t.Errorf("update not persisted: %+v", got)
	}

	if err := svc.Delete(c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.FindByID(c.ID); err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}
}
