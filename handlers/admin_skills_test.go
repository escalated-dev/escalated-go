package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
)

func openSkillsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	return db
}

type staticAgents struct {
	users []SkillFormAgent
}

func (s staticAgents) ListAgentsForSkillForm(_ context.Context) ([]SkillFormAgent, error) {
	return s.users, nil
}

func TestSkillsHandler_StoreAndList(t *testing.T) {
	db := openSkillsTestDB(t)
	defer db.Close()

	agents := staticAgents{users: []SkillFormAgent{{ID: 1, Name: "A", Email: "a@x"}, {ID: 2, Name: "B", Email: "b@x"}}}
	h := NewSkillsHandler(db, "escalated_", renderer.NewJSONRenderer(), agents)

	_, err := db.Exec(`INSERT INTO escalated_tags (name, slug, created_at, updated_at) VALUES ('bug','bug', ?, ?)`, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	body := `{"name":"Networking","routing_tag_ids":[1],"routing_department_ids":[],"agents":[{"user_id":1,"proficiency":4}]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/skills", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	h.StoreSkill(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("store: want 201 got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["id"] == nil {
		t.Fatal("expected id in response")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/admin/skills", nil)
	rec2 := httptest.NewRecorder()
	h.ListSkills(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec2.Code, rec2.Body.String())
	}
	var page map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	skills, _ := page["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("skills len: want 1 got %d", len(skills))
	}
}

func TestSkillsHandler_ValidateUnknownTag(t *testing.T) {
	db := openSkillsTestDB(t)
	defer db.Close()
	h := NewSkillsHandler(db, "escalated_", renderer.NewJSONRenderer(), nil)

	body := `{"name":"X","routing_tag_ids":[999],"routing_department_ids":[],"agents":[]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/skills", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	h.StoreSkill(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSkillRoutingService_FindMatchingAgents(t *testing.T) {
	db := openSkillsTestDB(t)
	defer db.Close()

	now := time.Now()
	_, err := db.Exec(`INSERT INTO escalated_tags (name, slug, created_at, updated_at) VALUES ('bug','bug', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var tagID int64
	if err := db.QueryRow(`SELECT id FROM escalated_tags LIMIT 1`).Scan(&tagID); err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`INSERT INTO escalated_departments (name, slug, is_active, created_at, updated_at) VALUES ('Support','support', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var deptID int64
	if err := db.QueryRow(`SELECT id FROM escalated_departments LIMIT 1`).Scan(&deptID); err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`INSERT INTO escalated_skills (name, slug, created_at, updated_at) VALUES ('S1','s1', ?, ?), ('S2','s2', ?, ?)`, now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var s1, s2 int64
	if err := db.QueryRow(`SELECT id FROM escalated_skills WHERE slug='s1'`).Scan(&s1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id FROM escalated_skills WHERE slug='s2'`).Scan(&s2); err != nil {
		t.Fatal(err)
	}

	_, _ = db.Exec(`INSERT INTO escalated_skill_routing_tags (skill_id, tag_id) VALUES (?, ?)`, s1, tagID)
	_, _ = db.Exec(`INSERT INTO escalated_skill_routing_departments (skill_id, department_id) VALUES (?, ?)`, s2, deptID)

	_, err = db.Exec(`INSERT INTO escalated_tickets (reference, subject, description, status, priority, department_id, created_at, updated_at)
		VALUES ('T-1','sub','desc', 0, 1, ?, ?, ?)`, deptID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var ticketID int64
	if err := db.QueryRow(`SELECT id FROM escalated_tickets LIMIT 1`).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`INSERT INTO escalated_ticket_tags (ticket_id, tag_id) VALUES (?, ?)`, ticketID, tagID)

	_, _ = db.Exec(`INSERT INTO escalated_agent_skills (user_id, skill_id, proficiency, created_at, updated_at) VALUES
		(10, ?, 4, ?, ?), (10, ?, 4, ?, ?), (20, ?, 4, ?, ?), (20, ?, 4, ?, ?)`, s1, now, now, s2, now, now, s1, now, now, s2, now, now)

	for i := 0; i < 3; i++ {
		ref := fmt.Sprintf("L-%d", i)
		_, _ = db.Exec(`INSERT INTO escalated_tickets (reference, subject, description, status, priority, assigned_to, created_at, updated_at)
			VALUES (?, 's','d', 0, 1, 20, ?, ?)`, ref, now, now)
	}
	_, _ = db.Exec(`INSERT INTO escalated_tickets (reference, subject, description, status, priority, assigned_to, created_at, updated_at)
		VALUES ('L-Z','s','d', 0, 1, 10, ?, ?)`, now, now)

	svc := services.NewSkillRoutingService(db, "escalated_")
	ticket := &models.Ticket{ID: ticketID, DepartmentID: &deptID, Tags: []models.Tag{{ID: tagID}}}
	users, err := svc.FindMatchingAgents(context.Background(), ticket)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("want 2 users got %d", len(users))
	}
	if users[0].ID != 10 {
		t.Fatalf("first user: want id 10 (lower ticket load when proficiency equal) got %d", users[0].ID)
	}
	if users[1].ID != 20 {
		t.Fatalf("second user want 20 got %d", users[1].ID)
	}
}

func TestSkillRoutingService_NoRequiredSkills(t *testing.T) {
	db := openSkillsTestDB(t)
	defer db.Close()
	svc := services.NewSkillRoutingService(db, "escalated_")
	ticket := &models.Ticket{ID: 1, Tags: nil}
	users, err := svc.FindMatchingAgents(context.Background(), ticket)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 0 {
		t.Fatalf("want empty, got %#v", users)
	}
}
