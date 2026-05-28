package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
)

func customActionHandler(ms *handlerMockStore, reg *actions.Registry, fired *string) *APIHandler {
	h := NewAPIHandler(ms, services.NewTicketService(ms), renderer.NewJSONRenderer(), func(_ *http.Request) int64 { return 1 })
	h.Actions = reg
	h.RoutePrefix = "/escalated"
	if fired != nil {
		h.OnCustomAction = func(_ context.Context, e actions.CustomActionEvent) error {
			*fired = e.Action
			return nil
		}
	}
	return h
}

func customActionRequest(ticketID, action string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodPost, "/api/tickets/"+ticketID+"/actions/"+action, bytes.NewReader([]byte(`{"payload":{}}`)))
	req.SetPathValue("id", ticketID)
	req.SetPathValue("action", action)
	return httptest.NewRecorder(), req
}

func TestAPICustomAction(t *testing.T) {
	t.Run("dispatches a visible, enabled action", func(t *testing.T) {
		ms := newHandlerMockStore()
		ms.tickets[1] = &models.Ticket{ID: 1, Reference: "TK-1"}
		var fired string
		h := customActionHandler(ms, actions.NewRegistry([]actions.TicketAction{{Key: "sync-crm", Label: "Sync CRM"}}), &fired)

		rec, req := customActionRequest("1", "sync-crm")
		h.CustomAction(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d; body %s", rec.Code, rec.Body.String())
		}
		if fired != "sync-crm" {
			t.Errorf("expected OnCustomAction with 'sync-crm', got %q", fired)
		}
	})

	t.Run("404 for unknown action", func(t *testing.T) {
		ms := newHandlerMockStore()
		ms.tickets[1] = &models.Ticket{ID: 1, Reference: "TK-1"}
		h := customActionHandler(ms, actions.NewRegistry(nil), nil)

		rec, req := customActionRequest("1", "nope")
		h.CustomAction(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("403 for disabled action", func(t *testing.T) {
		ms := newHandlerMockStore()
		ms.tickets[1] = &models.Ticket{ID: 1, Reference: "TK-1"}
		reg := actions.NewRegistry([]actions.TicketAction{{
			Key:     "sync-crm",
			Label:   "Sync CRM",
			Enabled: func(*models.Ticket, int64) bool { return false },
		}})
		h := customActionHandler(ms, reg, nil)

		rec, req := customActionRequest("1", "sync-crm")
		h.CustomAction(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}
