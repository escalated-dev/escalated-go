package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// The ticket screens post with Inertia: a form or JSON with X-Inertia, to the
// routes Laravel names escalated.agent.tickets.* and
// escalated.customer.tickets.*, and expect a 303 back to the page (or the page
// again with errors). These tests make each write the way the page does and
// check its effect.

// postPage posts a form as a page visit from the given page.
func postPage(h http.Handler, method, path, user, role, from string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Inertia", "true")
	r.Header.Set("Referer", "http://example.com"+from)
	if user != "" {
		r.Header.Set(headerUser, user)
	}
	if role != "" {
		r.Header.Set(headerRole, role)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func wantBack(t *testing.T, rec *httptest.ResponseRecorder, what, to string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != to {
		t.Fatalf("%s: got %d to %q, want 303 to %q: %s", what, rec.Code, rec.Header().Get("Location"), to, rec.Body.String())
	}
}

func wantPageErrors(t *testing.T, rec *httptest.ResponseRecorder, what, component, field string) {
	t.Helper()
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || page.Component != component {
		t.Fatalf("%s: got %d %s, want %s with errors", what, rec.Code, rec.Body.String(), component)
	}
	if _, ok := obj(t, page.Props, "errors")[field]; !ok {
		t.Errorf("%s: errors %v, want %q", what, page.Props["errors"], field)
	}
}

func (f *uiFixture) reload(t *testing.T) *models.Ticket {
	t.Helper()
	got, err := f.esc.Store.GetTicket(context.Background(), f.ticket.ID)
	if err != nil || got == nil {
		t.Fatalf("reload ticket: %v", err)
	}
	return got
}

func TestAgentTicketScreenWrites(t *testing.T) {
	f := newUIFixture(t)
	ctx := context.Background()
	h := f.mounts()["chi"]
	show := "/escalated/agent/tickets/" + url.PathEscape(f.ticket.Reference)
	post := func(action string, form url.Values) *httptest.ResponseRecorder {
		return postPage(h, http.MethodPost, show+action, uiAgent, "agent", show, form)
	}

	wantBack(t, post("/status", url.Values{"status": {"resolved"}}), "status", show)
	if got := f.reload(t); got.Status != models.StatusResolved {
		t.Errorf("status %d, want resolved", got.Status)
	}

	wantBack(t, post("/priority", url.Values{"priority": {"low"}}), "priority", show)
	if got := f.reload(t); got.Priority != models.PriorityLow {
		t.Errorf("priority %d, want low", got.Priority)
	}

	wantBack(t, post("/assign", url.Values{"agent_id": {"901"}}), "assign", show)
	if got := f.reload(t); got.AssignedTo == nil || *got.AssignedTo != "901" {
		t.Errorf("assigned_to %v, want 901", got.AssignedTo)
	}

	depts, _ := f.esc.Store.ListDepartments(ctx, false)
	var archived int64
	for _, d := range depts {
		if d.Slug == "archived" {
			archived = d.ID
		}
	}
	wantBack(t, post("/department", url.Values{"department_id": {itoa(archived)}}), "department", show)
	if got := f.reload(t); got.DepartmentID == nil || *got.DepartmentID != archived {
		t.Errorf("department %v, want %d", got.DepartmentID, archived)
	}

	// tag_ids is the whole set: none removes the vip tag, then it comes back.
	tags, _ := f.esc.Store.GetTicketTags(ctx, f.ticket.ID)
	vip := tags[0].ID
	wantBack(t, post("/tags", url.Values{}), "tags cleared", show)
	if got, _ := f.esc.Store.GetTicketTags(ctx, f.ticket.ID); len(got) != 0 {
		t.Errorf("tags %v, want none", got)
	}
	wantBack(t, post("/tags", url.Values{"tag_ids[]": {itoa(vip)}}), "tags set", show)
	if got, _ := f.esc.Store.GetTicketTags(ctx, f.ticket.ID); len(got) != 1 || got[0].ID != vip {
		t.Errorf("tags %v, want vip", got)
	}

	wantBack(t, post("/reply", url.Values{"body": {"Public answer"}}), "reply", show)
	wantBack(t, post("/note", url.Values{"body": {"Private aside"}}), "note", show)
	replies, _ := f.esc.Store.ListReplies(ctx, models.ReplyFilters{TicketID: f.ticket.ID})
	kinds := map[string]bool{}
	for _, r := range replies {
		kinds[r.Body] = r.IsInternal
	}
	if internal, ok := kinds["Public answer"]; !ok || internal {
		t.Errorf("reply: stored %v internal %v", ok, internal)
	}
	if internal, ok := kinds["Private aside"]; !ok || !internal {
		t.Errorf("note: stored %v internal %v", ok, internal)
	}

	// The fixture's agent follows the ticket: the first toggle unfollows.
	wantBack(t, post("/follow", nil), "unfollow", show)
	page := visit(t, h, show, uiAgent, "agent")
	if page.Props["is_following"] != false || page.Props["followers_count"].(float64) != 0 {
		t.Errorf("after unfollow: %v / %v", page.Props["is_following"], page.Props["followers_count"])
	}
	wantBack(t, post("/follow", nil), "follow", show)
	if page := visit(t, h, show, uiAgent, "agent"); page.Props["is_following"] != true {
		t.Errorf("after follow: %v", page.Props["is_following"])
	}

	// Pinning toggles an internal note; a public reply cannot be pinned.
	var note, public *models.Reply
	for _, r := range replies {
		if r.Body == "Fraud check pending" {
			note = r
		}
		if r.Body == "Public answer" {
			public = r
		}
	}
	wantBack(t, post("/replies/"+itoa(note.ID)+"/pin", nil), "unpin", show)
	if got, _ := f.esc.Store.GetReply(ctx, note.ID); got.IsPinned {
		t.Error("the note is still pinned")
	}
	wantPageErrors(t, post("/replies/"+itoa(public.ID)+"/pin", nil), "pin a reply", "Escalated/Agent/TicketShow", "reply")

	macros := services.NewMacroService(f.esc.Config.DB, nil, f.esc.Access)
	macro := &models.Macro{Name: "Note it", Actions: models.JSONText(`[{"type":"add_note","value":"From the macro"}]`), IsShared: true}
	if err := macros.Create(macro); err != nil {
		t.Fatalf("create macro: %v", err)
	}
	wantBack(t, post("/macro", url.Values{"macro_id": {itoa(macro.ID)}}), "macro", show)
	found := false
	replies, _ = f.esc.Store.ListReplies(ctx, models.ReplyFilters{TicketID: f.ticket.ID})
	for _, r := range replies {
		found = found || r.Body == "From the macro"
	}
	if !found {
		t.Error("the macro did not run")
	}
	wantPageErrors(t, post("/macro", url.Values{"macro_id": {"9999"}}), "unknown macro", "Escalated/Agent/TicketShow", "macro_id")

	wantBack(t, postPage(h, http.MethodPut, show, uiAgent, "agent", show, url.Values{"ticket_type": {"incident"}}), "update", show)
	if got := f.reload(t); got.TicketType != "incident" {
		t.Errorf("ticket_type %q, want incident", got.TicketType)
	}

	// Validation: the page again with errors; an API client gets 422.
	wantPageErrors(t, post("/status", url.Values{"status": {"bogus"}}), "bad status", "Escalated/Agent/TicketShow", "status")
	wantPageErrors(t, post("/reply", url.Values{"body": {" "}}), "empty reply", "Escalated/Agent/TicketShow", "body")
	if rec := sendAPI(h, http.MethodPost, show+"/status", uiAgent, "agent", `{"status":"bogus"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("API bad status: got %d, want 422", rec.Code)
	}
	if rec := sendAPI(h, http.MethodPost, show+"/status", uiAgent, "agent", `{"status":"closed"}`); rec.Code != http.StatusOK {
		t.Errorf("API status slug: got %d, want 200", rec.Code)
	}
	if rec := sendAPI(h, http.MethodPost, "/escalated/agent/tickets/"+itoa(f.ticket.ID)+"/status", uiAgent, "agent", `{"status":1}`); rec.Code != http.StatusOK {
		t.Errorf("API status integer by id: got %d, want 200", rec.Code)
	}

	// Files are refused rather than dropped.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("body", "with a file")
	fw, _ := mw.CreateFormFile("attachments[0]", "a.txt")
	_, _ = fw.Write([]byte("hello"))
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, show+"/reply", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("X-Inertia", "true")
	r.Header.Set(headerUser, uiAgent)
	r.Header.Set(headerRole, "agent")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	wantPageErrors(t, rec, "reply with a file", "Escalated/Agent/TicketShow", "attachments")

	// Customers cannot reach the agent writes.
	if rec := postPage(h, http.MethodPost, show+"/status", customerB, "", show, url.Values{"status": {"open"}}); !denied(rec.Code) {
		t.Errorf("customer posting an agent write: got %d", rec.Code)
	}
}

func TestAgentTicketScreenWritesOnStdlib(t *testing.T) {
	f := newUIFixture(t)
	h := f.mounts()["stdlib"]
	show := "/escalated/agent/tickets/" + url.PathEscape(f.ticket.Reference)
	for action, form := range map[string]url.Values{
		"/priority":   {"priority": {"low"}},
		"/note":       {"body": {"stdlib note"}},
		"/follow":     nil,
		"/tags":       {},
		"/department": {"department_id": {itoa(*f.ticket.DepartmentID)}},
	} {
		wantBack(t, postPage(h, http.MethodPost, show+action, uiAgent, "agent", show, form), action, show)
	}
	wantBack(t, postPage(h, http.MethodPut, show, uiAgent, "agent", show, url.Values{"subject": {"Renamed"}}), "update", show)
	if got := f.reload(t); got.Subject != "Renamed" || got.Priority != models.PriorityLow {
		t.Errorf("subject %q priority %d", got.Subject, got.Priority)
	}
}

func TestCustomerTicketScreenWrites(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			show := "/escalated/tickets/" + url.PathEscape(f.ticket.Reference)
			post := func(user, action string, form url.Values) *httptest.ResponseRecorder {
				return postPage(h, http.MethodPost, show+action, user, "", show, form)
			}

			wantBack(t, post(customerB, "/reply", url.Values{"body": {"Thanks " + name}}), "reply", show)
			wantPageErrors(t, post(customerB, "/reply", url.Values{"body": {""}}), "empty reply", "Escalated/Customer/Show", "body")

			wantBack(t, post(customerB, "/close", nil), "close", show)
			if got := f.reload(t); got.Status != models.StatusClosed {
				t.Fatalf("status %d after close, want closed", got.Status)
			}

			// The fixture's ticket was rated already: a second rating is refused
			// on the page.
			wantPageErrors(t, post(customerB, "/rate", url.Values{"rating": {"5"}}), "second rating", "Escalated/Customer/Show", "rating")

			wantBack(t, post(customerB, "/reopen", nil), "reopen", show)
			if got := f.reload(t); got.Status != models.StatusReopened {
				t.Errorf("status %d after reopen, want reopened", got.Status)
			}

			for _, action := range []string{"/reply", "/close", "/reopen", "/rate"} {
				if rec := post(customerA, action, url.Values{"body": {"x"}, "rating": {"5"}}); rec.Code != http.StatusForbidden {
					t.Errorf("another customer %s: got %d, want 403", action, rec.Code)
				}
			}
		})
	}
}

func TestCustomerRatesFromTheTicketScreen(t *testing.T) {
	f := newUIFixture(t)
	db := f.esc.Config.DB
	if _, err := db.Exec(sqldialect.Rebind(db, `DELETE FROM escalated_satisfaction_ratings WHERE ticket_id = ?`), f.ticket.ID); err != nil {
		t.Fatal(err)
	}
	h := f.mounts()["chi"]
	show := "/escalated/tickets/" + url.PathEscape(f.ticket.Reference)

	// An open ticket cannot be rated yet.
	wantPageErrors(t, postPage(h, http.MethodPost, show+"/rate", customerB, "", show, url.Values{"rating": {"5"}}), "rate open", "Escalated/Customer/Show", "rating")

	wantBack(t, postPage(h, http.MethodPost, show+"/close", customerB, "", show, nil), "close", show)
	wantBack(t, postPage(h, http.MethodPost, show+"/rate", customerB, "", show, url.Values{"rating": {"5"}, "comment": {"Great"}}), "rate", show)
	page := visit(t, h, show, customerB, "")
	if got := obj(t, obj(t, page.Props, "ticket"), "satisfaction_rating"); got["rating"].(float64) != 5 || got["comment"] != "Great" {
		t.Errorf("satisfaction_rating %v", got)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
