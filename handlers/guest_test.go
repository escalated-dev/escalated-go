package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/services"
)

type lastCodeMailer struct{ text string }

func (m *lastCodeMailer) SendMail(_ context.Context, msg services.Mail) error {
	m.text = msg.Text
	return nil
}

func guestHandlerFixture(t *testing.T) (*GuestTicketHandler, *sql.DB) {
	t.Helper()
	db := testdb.Open(t)
	s := testdb.Store(t, db)
	h := NewGuestTicketHandler(s, services.NewTicketService(s))
	h.Access = services.NewGuestAccess(db, testdb.Prefix, services.GuestAccessConfig{Secret: "test"}, &lastCodeMailer{}, "", nil, nil)
	return h, db
}

// guestCode requests a ticket code for email and returns the proof fields.
func guestCode(t *testing.T, h *GuestTicketHandler, email string) string {
	t.Helper()
	id, err := h.Access.Challenge(context.Background(), email, services.GuestPurposeTicket, "198.51.100.9")
	if err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`[0-9]{8}`).FindString(h.Access.Mailer.(*lastCodeMailer).text)
	return `"verification_id":"` + id + `","verification_code":"` + code + `"`
}

func TestGuestTicketCreateAndLookup(t *testing.T) {
	h, db := guestHandlerFixture(t)
	defer db.Close()

	body := `{"guest_name":"Pat","guest_email":"pat@example.com","subject":"Help","description":"Please assist",` + guestCode(t, h, "pat@example.com") + `}`
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
