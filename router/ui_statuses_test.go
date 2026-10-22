package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// Custom ticket statuses (the shared statuses spec): the Admin/Statuses pages
// with Laravel's props and writes, and custom statuses on the ticket screens.

const statusIndex = "/escalated/admin/statuses"

// statusBySlug finds a status on the index page.
func statusBySlug(t *testing.T, h http.Handler, slug string) map[string]any {
	t.Helper()
	page := visit(t, h, statusIndex, uiAgent, "admin")
	for _, item := range list(t, page.Props, "statuses") {
		if m := item.(map[string]any); m["slug"] == slug {
			return m
		}
	}
	t.Fatalf("no status %q in %v", slug, page.Props["statuses"])
	return nil
}

func statusID(t *testing.T, h http.Handler, slug string) string {
	t.Helper()
	return fmt.Sprint(int64(statusBySlug(t, h, slug)["id"].(float64)))
}

var statusKeys = []string{"id", "label", "slug", "category", "color", "description", "position", "is_default", "is_system", "created_at", "updated_at"}

func TestAdminStatusPages(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			index := visit(t, h, statusIndex, uiAgent, "admin")
			if index.Component != "Escalated/Admin/Statuses/Index" {
				t.Fatalf("index component %q", index.Component)
			}
			if got := fmt.Sprint(list(t, index.Props, "categories")); got != "[new open pending on_hold solved]" {
				t.Errorf("categories %s", got)
			}
			// The built-ins are listed as system rows.
			system := statusBySlug(t, h, "waiting_on_customer")
			hasKeys(t, "status", system, statusKeys...)
			if system["is_system"] != true || system["category"] != "pending" || system["label"] != "Waiting on Customer" {
				t.Errorf("system row %v", system)
			}
			if len(list(t, index.Props, "statuses")) < len(models.StatusName) {
				t.Errorf("statuses %v", index.Props["statuses"])
			}

			create := visit(t, h, statusIndex+"/create", uiAgent, "admin")
			if create.Component != "Escalated/Admin/Statuses/Form" {
				t.Fatalf("create component %q", create.Component)
			}
			hasKeys(t, "create props", create.Props, "categories")
			if _, has := create.Props["status"]; has {
				t.Errorf("create has a status: %v", create.Props["status"])
			}

			label := "Waiting on vendor " + name
			slug := "waiting_on_vendor_" + name
			wantRedirect(t, pageJSON(h, http.MethodPost, statusIndex, `{"label":"`+label+`","category":"pending","color":"#ff8800",
				"description":"Third party","position":2,"is_default":true}`), "store", statusIndex)
			// The same label again gets a de-duplicated slug, and takes the default.
			wantRedirect(t, pageJSON(h, http.MethodPost, statusIndex, `{"label":"`+label+`","category":"pending","color":"#000000","is_default":true}`), "store again", statusIndex)

			row := statusBySlug(t, h, slug)
			hasKeys(t, "status", row, statusKeys...)
			if row["label"] != label || row["category"] != "pending" || row["color"] != "#ff8800" || row["description"] != "Third party" ||
				row["position"] != float64(2) || row["is_system"] != false {
				t.Errorf("status %v", row)
			}
			if row["is_default"] != false || statusBySlug(t, h, slug+"_2")["is_default"] != true {
				t.Errorf("is_default not exclusive within the category")
			}

			id := statusID(t, h, slug)
			edit := visit(t, h, statusIndex+"/"+id+"/edit", uiAgent, "admin")
			if edit.Component != "Escalated/Admin/Statuses/Form" {
				t.Fatalf("edit component %q", edit.Component)
			}
			hasKeys(t, "edit props", edit.Props, "status", "categories")
			hasKeys(t, "edit status", obj(t, edit.Props, "status"), statusKeys...)

			wantRedirect(t, pageJSON(h, http.MethodPut, statusIndex+"/"+id, `{"label":"Vendor `+name+`","category":"on_hold","color":"#123456",
				"description":"","position":5,"is_default":true}`), "update", statusIndex)
			updated := statusBySlug(t, h, slug)
			if updated["label"] != "Vendor "+name || updated["category"] != "on_hold" || updated["color"] != "#123456" ||
				updated["description"] != nil || updated["position"] != float64(5) || updated["is_default"] != true {
				t.Errorf("updated %v", updated)
			}
			// Now the default of on_hold; pending's default is untouched.
			if statusBySlug(t, h, slug+"_2")["is_default"] != true {
				t.Error("another category's default was cleared")
			}

			for what, body := range map[string]string{
				"label":      `{"label":"","category":"open","color":"#fff"}`,
				"category":   `{"label":"X","category":"closed","color":"#fff"}`,
				"color":      `{"label":"X","category":"open","color":"#0123456789012345678901"}`,
				"position":   `{"label":"X","category":"open","color":"#fff","position":"first"}`,
				"is_default": `{"label":"X","category":"open","color":"#fff","is_default":"maybe"}`,
			} {
				wantPageErrors(t, pageJSON(h, http.MethodPost, statusIndex, body), "store with a bad "+what, "Escalated/Admin/Statuses/Form", what)
			}
			if rec := sendAPI(h, http.MethodPost, statusIndex, uiAgent, "admin", `{"label":"X","category":"open"}`); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"color"`) {
				t.Errorf("store without a colour: %d %s", rec.Code, rec.Body.String())
			}

			// A system row: label and colour edit, category does not, no delete.
			sysID := statusID(t, h, "escalated")
			wantPageErrors(t, pageJSON(h, http.MethodPut, statusIndex+"/"+sysID, `{"label":"Escalated","category":"solved","color":"#EF4444"}`),
				"system category change", "Escalated/Admin/Statuses/Form", "category")
			wantRedirect(t, pageJSON(h, http.MethodPut, statusIndex+"/"+sysID, `{"label":"Escalated to tier 2","category":"open","color":"#EF0000","position":3}`), "system update", statusIndex)
			if got := statusBySlug(t, h, "escalated"); got["label"] != "Escalated to tier 2" || got["category"] != "open" || got["slug"] != "escalated" || got["is_system"] != true {
				t.Errorf("system row after update %v", got)
			}
			wantPageErrors(t, pageJSON(h, http.MethodDelete, statusIndex+"/"+sysID, ""), "system delete", "Escalated/Admin/Statuses/Index", "status")
			if rec := sendAPI(h, http.MethodDelete, statusIndex+"/"+sysID, uiAgent, "admin", ""); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("API system delete: %d %s", rec.Code, rec.Body.String())
			}
			statusBySlug(t, h, "escalated")

			// Deleting a custom status: its tickets keep their built-in status.
			var sid int64
			if _, err := fmt.Sscan(id, &sid); err != nil {
				t.Fatal(err)
			}
			show := "/escalated/agent/tickets/" + url.PathEscape(f.ticket.Reference)
			wantBack(t, postPage(h, http.MethodPost, show+"/status", uiAgent, "agent", show, url.Values{"status": {slug}}), "custom status", show)
			if got := f.reload(t); got.Status != models.StatusInProgress || got.TicketStatusID == nil || *got.TicketStatusID != sid {
				t.Fatalf("ticket %s custom %v", got.StatusString(), got.TicketStatusID)
			}
			wantRedirect(t, pageJSON(h, http.MethodDelete, statusIndex+"/"+id, ""), "destroy", statusIndex)
			if got := f.reload(t); got.Status != models.StatusInProgress || got.TicketStatusID != nil {
				t.Fatalf("after destroy: %s custom %v", got.StatusString(), got.TicketStatusID)
			}
			if rec := send(h, http.MethodGet, statusIndex+"/"+id+"/edit", uiAgent, "admin", ""); rec.Code != http.StatusNotFound {
				t.Errorf("deleted status edit: %d", rec.Code)
			}

			// Admin only.
			for _, path := range []string{statusIndex, statusIndex + "/create"} {
				if rec := sendAPI(h, http.MethodGet, path, uiAgent, "agent", ""); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
					t.Errorf("agent GET %s: %d", path, rec.Code)
				}
			}
			if rec := sendAPI(h, http.MethodPost, statusIndex, uiAgent, "agent", `{"label":"X","category":"open","color":"#fff"}`); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound && rec.Code != http.StatusUnauthorized {
				t.Errorf("agent store: %d", rec.Code)
			}
		})
	}
}

func TestCustomStatusOnTheTicketScreens(t *testing.T) {
	f := newUIFixture(t)
	h := f.mounts()["chi"]
	wantRedirect(t, pageJSON(h, http.MethodPost, statusIndex, `{"label":"Fixed upstream","category":"solved","color":"#00aa00"}`), "store", statusIndex)
	wantRedirect(t, pageJSON(h, http.MethodPost, statusIndex, `{"label":"Waiting on vendor","category":"pending","color":"#ff8800"}`), "store", statusIndex)

	ref := url.PathEscape(f.ticket.Reference)
	show := "/escalated/agent/tickets/" + ref
	wantBack(t, postPage(h, http.MethodPost, show+"/status", uiAgent, "agent", show, url.Values{"status": {"fixed_upstream"}}), "custom status", show)
	got := f.reload(t)
	if got.Status != models.StatusResolved || got.ResolvedAt == nil || got.TicketStatusID == nil {
		t.Fatalf("ticket %s resolved_at %v custom %v", got.StatusString(), got.ResolvedAt, got.TicketStatusID)
	}
	wantPageErrors(t, postPage(h, http.MethodPost, show+"/status", uiAgent, "agent", show, url.Values{"status": {"nope"}}),
		"unknown status", "Escalated/Agent/TicketShow", "status")

	// Agent and admin pages carry custom_status and status_category.
	for _, path := range []string{show, "/escalated/admin/tickets/" + ref} {
		page := visit(t, h, path, uiAgent, "admin")
		ticket := obj(t, page.Props, "ticket")
		custom := obj(t, ticket, "custom_status")
		hasKeys(t, path+" custom_status", custom, "id", "label", "slug", "color", "category")
		if custom["label"] != "Fixed upstream" || custom["slug"] != "fixed_upstream" || custom["category"] != "solved" ||
			ticket["status_category"] != "solved" || ticket["status"] != "resolved" {
			t.Errorf("%s: ticket status %v custom %v category %v", path, ticket["status"], custom, ticket["status_category"])
		}
	}
	queue := visit(t, h, "/escalated/agent/tickets?status=fixed_upstream", uiAgent, "agent")
	rows := list(t, obj(t, queue.Props, "tickets"), "data")
	if len(rows) != 1 {
		t.Fatalf("custom status filter: %d rows", len(rows))
	}
	if row := rows[0].(map[string]any); obj(t, row, "custom_status")["slug"] != "fixed_upstream" || row["status_category"] != "solved" {
		t.Errorf("queue row %v", row)
	}
	other := visit(t, h, "/escalated/agent/tickets?status=open", uiAgent, "agent")
	for _, item := range list(t, obj(t, other.Props, "tickets"), "data") {
		if row := item.(map[string]any); row["custom_status"] != nil || row["status_category"] != "open" {
			t.Errorf("open row %v", row)
		}
	}

	// The customer sees the built-in status only.
	for _, path := range []string{"/escalated/tickets/" + ref, "/escalated/tickets"} {
		rec := send(h, http.MethodGet, path, customerB, "", "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Fixed upstream") || strings.Contains(rec.Body.String(), "fixed_upstream") ||
			strings.Contains(rec.Body.String(), "custom_status") || !strings.Contains(rec.Body.String(), `"resolved"`) {
			t.Errorf("customer %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}

	// The JSON API: custom_status on show and list, and PATCH takes a slug.
	rec := sendAPI(h, http.MethodGet, "/escalated/api/tickets/"+fmt.Sprint(f.ticket.ID), uiAgent, "agent", "")
	var detail struct {
		Ticket map[string]any `json:"ticket"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || obj(t, detail.Ticket, "custom_status")["slug"] != "fixed_upstream" || detail.Ticket["status_category"] != "solved" {
		t.Fatalf("API show: %d %s", rec.Code, rec.Body.String())
	}
	if _, leaked := detail.Ticket["ticket_status_id"]; leaked {
		t.Error("API show carries ticket_status_id")
	}
	rec = sendAPI(h, http.MethodPatch, "/escalated/api/tickets/"+fmt.Sprint(f.ticket.ID), uiAgent, "agent", `{"status":"waiting_on_vendor"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("API patch: %d %s", rec.Code, rec.Body.String())
	}
	if got := f.reload(t); got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID == nil {
		t.Fatalf("after API patch: %s %v", got.StatusString(), got.TicketStatusID)
	}
	if rec := sendAPI(h, http.MethodPatch, "/escalated/api/tickets/"+fmt.Sprint(f.ticket.ID), uiAgent, "agent", `{"status":"nope"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("API patch unknown status: %d", rec.Code)
	}
	// The anchor itself, as a built-in, clears the custom status.
	if rec := sendAPI(h, http.MethodPatch, "/escalated/api/tickets/"+fmt.Sprint(f.ticket.ID), uiAgent, "agent", `{"status":"waiting_on_customer"}`); rec.Code != http.StatusOK {
		t.Fatalf("API patch built-in: %d %s", rec.Code, rec.Body.String())
	}
	if got := f.reload(t); got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID != nil {
		t.Fatalf("after API patch built-in: %s %v", got.StatusString(), got.TicketStatusID)
	}
	rec = sendAPI(h, http.MethodGet, "/escalated/api/tickets", uiAgent, "agent", "")
	var listed struct {
		Tickets []map[string]any `json:"tickets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || len(listed.Tickets) == 0 {
		t.Fatalf("API list: %d %s", rec.Code, rec.Body.String())
	}
	for _, row := range listed.Tickets {
		if _, has := row["custom_status"]; !has || row["status_category"] == nil {
			t.Errorf("API list row %v", row)
		}
	}

	// Bulk status takes a custom slug.
	queuePath := "/escalated/agent/tickets"
	wantBack(t, postPage(h, http.MethodPost, queuePath+"/bulk", uiAgent, "agent", queuePath, url.Values{
		"ticket_ids[]": {fmt.Sprint(f.ticket.ID), fmt.Sprint(f.other.ID)}, "action": {"status"}, "value": {"waiting_on_vendor"},
	}), "bulk custom status", queuePath)
	for _, id := range []int64{f.ticket.ID, f.other.ID} {
		if got, _ := f.esc.Store.GetTicket(context.Background(), id); got.Status != models.StatusWaitingOnCustomer || got.TicketStatusID == nil {
			t.Errorf("bulk: ticket %d %s %v", id, got.StatusString(), got.TicketStatusID)
		}
	}
}
