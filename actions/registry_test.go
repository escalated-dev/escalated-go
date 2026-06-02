package actions

import (
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func TestRegistryForTicket(t *testing.T) {
	ticket := &models.Ticket{ID: 1, Reference: "TK-1"}

	t.Run("serializes a config action with defaults", func(t *testing.T) {
		r := NewRegistry([]TicketAction{{Key: "sync-crm", Label: "Sync CRM"}})

		got := r.ForTicket(ticket, models.UserID("9"))
		if len(got) != 1 {
			t.Fatalf("expected 1 action, got %d", len(got))
		}
		if got[0]["key"] != "sync-crm" || got[0]["variant"] != "secondary" {
			t.Errorf("unexpected serialization: %+v", got[0])
		}
		if got[0]["disabled"] != false {
			t.Errorf("expected enabled action, got disabled=%v", got[0]["disabled"])
		}
		if got[0]["confirmation"] != nil {
			t.Errorf("expected nil confirmation, got %v", got[0]["confirmation"])
		}
	})

	t.Run("omits invisible and marks disabled", func(t *testing.T) {
		r := NewRegistry([]TicketAction{
			{Key: "hidden", Label: "Hidden", Visible: func(*models.Ticket, models.UserID) bool { return false }},
			{Key: "locked", Label: "Locked", Enabled: func(*models.Ticket, models.UserID) bool { return false }},
		})

		got := r.ForTicket(ticket, models.UserID("9"))
		if len(got) != 1 || got[0]["key"] != "locked" {
			t.Fatalf("expected only 'locked', got %+v", got)
		}
		if got[0]["disabled"] != true {
			t.Errorf("expected disabled=true, got %v", got[0]["disabled"])
		}
	})

	t.Run("find and skip-invalid", func(t *testing.T) {
		r := NewRegistry([]TicketAction{
			{Key: "", Label: "no key"},
			{Key: "ok", Label: "Ok"},
		})
		if _, ok := r.Find("ok"); !ok {
			t.Error("expected to find 'ok'")
		}
		if _, ok := r.Find("missing"); ok {
			t.Error("did not expect to find 'missing'")
		}
		if len(r.ForTicket(ticket, models.UserID("1"))) != 1 {
			t.Error("expected invalid (no-key) action to be skipped")
		}
	})
}
