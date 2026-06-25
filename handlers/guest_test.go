package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

func guestHandlerFixture(t *testing.T) (*GuestTicketHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	s := store.NewSQLiteStore(db, "escalated_")
	return NewGuestTicketHandler(s, services.NewTicketService(s)), db
}

func TestGuestTicketCreateAndLookup(t *testing.T) {
	h, db := guestHandlerFixture(t)
	defer db.Close()

	body := `{"guest_name":"Pat","guest_email":"pat@example.com","subject":"Help","description":"Please assist"}`
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/guest/tickets", bytes.NewBufferString(body)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var created struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	token, _ := created.Data["guest_token"].(string)
	if token == "" {
		t.Fatal("expected guest_token in create response")
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/guest/tickets/"+token, nil)
	req2.SetPathValue("token", token)
	h.Show(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("show: want 200, got %d (%s)", rec2.Code, rec2.Body.String())
	}
	var found struct {
		Data map[string]any `json:"data"`
	}
	_ = json.NewDecoder(rec2.Body).Decode(&found)
	if found.Data["subject"] != "Help" {
		t.Fatalf("want subject Help, got %v", found.Data["subject"])
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/guest/tickets/unknown", nil)
	req3.SetPathValue("token", "unknown")
	h.Show(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("unknown token: want 404, got %d", rec3.Code)
	}
}

func TestGuestTicketCreateValidation(t *testing.T) {
	h, db := guestHandlerFixture(t)
	defer db.Close()

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/guest/tickets", bytes.NewBufferString(`{"subject":"x"}`)))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing fields: want 422, got %d", rec.Code)
	}
}
