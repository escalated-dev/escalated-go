package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

func TestGuestRateFlow(t *testing.T) {
	db := testdb.Open(t)

	s := testdb.Store(t, db)
	ts := services.NewTicketService(s)
	h := NewSatisfactionHandler(db)

	name, email := "Pat", "pat@example.com"
	tkt, err := ts.Create(context.Background(), services.CreateTicketInput{
		Subject:     "Help",
		Description: "x",
		GuestName:   &name,
		GuestEmail:  &email,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := *tkt.GuestToken

	rate := func(tok, bodyJSON string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/guest/tickets/"+tok+"/rate", strings.NewReader(bodyJSON))
		req.SetPathValue("token", tok)
		h.GuestRate(rec, req)
		return rec.Code
	}

	// Open ticket is not yet rateable.
	if code := rate(token, `{"rating":5}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("open ticket: want 422, got %d", code)
	}

	// Resolve it, then a guest can rate once.
	if _, err := db.Exec(sqldialect.Rebind(db, `UPDATE escalated_tickets SET status = ? WHERE id = ?`), models.StatusResolved, tkt.ID); err != nil {
		t.Fatal(err)
	}
	if code := rate(token, `{"rating":5}`); code != http.StatusCreated {
		t.Fatalf("rate: want 201, got %d", code)
	}

	// A second rating is rejected.
	if code := rate(token, `{"rating":4}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("re-rate: want 422, got %d", code)
	}

	// Unknown token is 404.
	if code := rate("nope", `{"rating":5}`); code != http.StatusNotFound {
		t.Fatalf("unknown token: want 404, got %d", code)
	}
}
