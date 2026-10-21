package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

// The admin ticket pages (Laravel Admin\TicketController): the queue and
// ticket screen with the agent pages' props plus agents, savedViews and
// is_snoozed, their writes at escalated.admin.tickets.*, bulk actions, split,
// merge and the panels by reference.

func TestAdminTicketIndexProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/admin/tickets?priority=urgent", uiAgent, "admin")
			if page.Component != "Escalated/Admin/Tickets/Index" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "tickets", "filters", "departments", "tags", "agents", "savedViews")
			tickets := obj(t, page.Props, "tickets")
			paginatorKeys(t, "tickets", tickets)
			hasKeys(t, "row", first(t, list(t, tickets, "data"), "tickets.data"), ticketRowKeys...)
			if tickets["total"].(float64) != 1 {
				t.Errorf("priority=urgent: %v tickets, want 1", tickets["total"])
			}
			agent := first(t, list(t, page.Props, "agents"), "agents")
			hasKeys(t, "agent", agent, "id", "name", "email")
			if got := list(t, page.Props, "savedViews"); len(got) != 0 {
				t.Errorf("savedViews %v, want none", got)
			}
		})
	}
}

func TestAdminTicketShowProps(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			page := visit(t, h, "/escalated/admin/tickets/"+url.PathEscape(f.ticket.Reference), uiAgent, "admin")
			if page.Component != "Escalated/Admin/Tickets/Show" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "ticket", "departments", "tags", "cannedResponses", "agents", "macros",
				"is_following", "followers_count", "is_snoozed")
			if _, ok := page.Props["customActions"]; ok {
				t.Error("the admin screen got customActions, which Laravel does not pass")
			}
			ticket := obj(t, page.Props, "ticket")
			hasKeys(t, "ticket", ticket, append(ticketRowKeys, "description", "replies", "attachments", "activities",
				"pinned_notes", "satisfaction_rating", "sla_policy", "related_tickets")...)
			if len(list(t, ticket, "replies")) != 3 {
				t.Errorf("replies %d, want 3", len(list(t, ticket, "replies")))
			}

			// Admin pages are for admins.
			if rec := send(h, http.MethodGet, "/escalated/admin/tickets", uiAgent, "agent", ""); !denied(rec.Code) {
				t.Errorf("an agent opened the admin queue: %d", rec.Code)
			}
		})
	}
}

func TestAdminTicketScreenWrites(t *testing.T) {
	f := newUIFixture(t)
	ctx := context.Background()
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			show := "/escalated/admin/tickets/" + url.PathEscape(f.ticket.Reference)
			post := func(action string, form url.Values) {
				t.Helper()
				wantBack(t, postPage(h, http.MethodPost, show+action, uiAgent, "admin", show, form), action, show)
			}
			post("/status", url.Values{"status": {"in_progress"}})
			post("/priority", url.Values{"priority": {"critical"}})
			post("/note", url.Values{"body": {"admin note " + name}})
			post("/follow", nil)
			if got := f.reload(t); got.Status != models.StatusInProgress || got.Priority != models.PriorityCritical {
				t.Errorf("status %d priority %d", got.Status, got.Priority)
			}
			wantPageErrors(t, postPage(h, http.MethodPost, show+"/priority", uiAgent, "admin", show, url.Values{"priority": {"nope"}}),
				"bad priority", "Escalated/Admin/Tickets/Show", "priority")

			// The panels take the reference too.
			rec := send(h, http.MethodGet, show+"/links", uiAgent, "admin", "")
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), f.other.Reference) {
				t.Errorf("links by reference: %d %s", rec.Code, rec.Body.String())
			}
		})
	}

	// Split a reply into a new ticket, and go to it.
	h := f.mounts()["chi"]
	show := "/escalated/admin/tickets/" + url.PathEscape(f.ticket.Reference)
	replies, _ := f.esc.Store.ListReplies(ctx, models.ReplyFilters{TicketID: f.ticket.ID})
	var public *models.Reply
	for _, r := range replies {
		if r.Body == "Here is a screenshot" {
			public = r
		}
	}
	rec := postPage(h, http.MethodPost, show+"/split", uiAgent, "admin", show, url.Values{"reply_id": {itoa(public.ID)}, "subject": {"Screenshot follow-up"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("split: %d %s", rec.Code, rec.Body.String())
	}
	ref := strings.TrimPrefix(rec.Header().Get("Location"), "/escalated/admin/tickets/")
	split, _ := f.esc.Store.GetTicketByReference(ctx, ref)
	if split == nil || split.Subject != "Screenshot follow-up" || split.Description != "Here is a screenshot" {
		t.Fatalf("split went to %q: %+v", rec.Header().Get("Location"), split)
	}
	wantPageErrors(t, postPage(h, http.MethodPost, show+"/split", uiAgent, "admin", show, url.Values{"reply_id": {"99999"}}),
		"split an unknown reply", "Escalated/Admin/Tickets/Show", "reply_id")

	// Merge from the page goes back.
	other := "/escalated/admin/tickets/" + url.PathEscape(split.Reference)
	rec = postPage(h, http.MethodPost, other+"/merge", uiAgent, "admin", other, url.Values{"target_reference": {f.ticket.Reference}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("merge: %d %s", rec.Code, rec.Body.String())
	}
	if merged, _ := f.esc.Store.GetTicket(ctx, split.ID); merged.MergedIntoID == nil || *merged.MergedIntoID != f.ticket.ID {
		t.Errorf("merged_into_id %v", merged.MergedIntoID)
	}
}

func TestBulkActions(t *testing.T) {
	f := newUIFixture(t)
	ctx := context.Background()
	h := f.mounts()["chi"]
	ids := url.Values{"ticket_ids[]": {itoa(f.ticket.ID), itoa(f.other.ID)}}
	with := func(extra url.Values) url.Values {
		out := url.Values{}
		for k, v := range ids {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	queue := "/escalated/agent/tickets"
	wantBack(t, postPage(h, http.MethodPost, queue+"/bulk", uiAgent, "agent", queue, with(url.Values{"action": {"priority"}, "value": {"critical"}})), "bulk priority", queue)
	for _, id := range []int64{f.ticket.ID, f.other.ID} {
		if got, _ := f.esc.Store.GetTicket(ctx, id); got.Priority != models.PriorityCritical {
			t.Errorf("ticket %d priority %d, want critical", id, got.Priority)
		}
	}
	wantBack(t, postPage(h, http.MethodPost, queue+"/bulk", uiAgent, "agent", queue, with(url.Values{"action": {"assign"}, "value": {"901"}})), "bulk assign", queue)
	if got, _ := f.esc.Store.GetTicket(ctx, f.other.ID); got.AssignedTo == nil || *got.AssignedTo != "901" {
		t.Errorf("other ticket assigned to %v, want 901", got.AssignedTo)
	}

	wantPageErrors(t, postPage(h, http.MethodPost, queue+"/bulk", uiAgent, "agent", queue, with(url.Values{"action": {"explode"}})),
		"unknown action", "Escalated/Agent/TicketIndex", "action")

	// Only admins delete.
	if rec := postPage(h, http.MethodPost, queue+"/bulk", uiAgent, "agent", queue, with(url.Values{"action": {"delete"}})); rec.Code != http.StatusForbidden {
		t.Errorf("agent bulk delete: got %d, want 403", rec.Code)
	}
	rec := sendAPI(h, http.MethodPost, "/escalated/admin/tickets/bulk", uiAgent, "admin",
		`{"ticket_ids":[`+itoa(f.other.ID)+`],"action":"delete"}`)
	var out map[string]any
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["updated"].(float64) != 1 {
		t.Fatalf("admin bulk delete: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := f.esc.Store.GetTicket(ctx, f.other.ID); got != nil {
		t.Error("the ticket was not deleted")
	}
}
