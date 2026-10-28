package router_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// The guest pages (Laravel Guest\TicketController) on the verified guest
// flow: the form posts the email code it got through verification_url, and
// the ticket opens by the expiring grant it is created with.
func TestGuestPages(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t, func(cfg *escalated.Config) {
				cfg.Mailer = newCodeMailer()
				cfg.GuestAccess.Secret = "guest-pages-test-secret-0123456789abcdef"
			})
			h := f.mounts()[name]

			create := visit(t, h, "/escalated/guest/create", "", "")
			if create.Component != "Escalated/Guest/Create" {
				t.Fatalf("component %q", create.Component)
			}
			hasKeys(t, "props", create.Props, "departments", "priorities", "verification_url", "lookup_url")
			if create.Props["verification_url"] != "/escalated/api/guest/verification" || create.Props["lookup_url"] != "/escalated/api/guest/lookup" {
				t.Errorf("urls %v %v", create.Props["verification_url"], create.Props["lookup_url"])
			}

			// A wrong code is refused on the form.
			bad := withProof(t, f.esc, "ticket", map[string]any{"guest_email": "gina@example.com"})
			form := url.Values{"guest_name": {"Gina"}, "guest_email": {"gina@example.com"}, "subject": {"Lost access"}, "description": {"I cannot sign in"},
				"priority": {"high"}, "department_id": {""}, "verification_id": {bad["verification_id"].(string)}, "verification_code": {"00000000"}}
			wantPageErrors(t, postPage(h, http.MethodPost, "/escalated/guest", "", "", "/escalated/guest/create", form), "wrong code", "Escalated/Guest/Create", "verification_code")
			missing := url.Values{"guest_email": {"not-an-email"}}
			wantPageErrors(t, postPage(h, http.MethodPost, "/escalated/guest", "", "", "/escalated/guest/create", missing), "invalid form", "Escalated/Guest/Create", "guest_email")

			proof := withProof(t, f.esc, "ticket", map[string]any{"guest_email": "gina@example.com"})
			form.Set("verification_id", proof["verification_id"].(string))
			form.Set("verification_code", proof["verification_code"].(string))
			rec := postPage(h, http.MethodPost, "/escalated/guest", "", "", "/escalated/guest/create", form)
			if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/escalated/guest/") {
				t.Fatalf("store: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
			}
			if rec.Header().Get("Cache-Control") != "private, no-store" {
				t.Errorf("guest responses must not be cached: %q", rec.Header().Get("Cache-Control"))
			}
			show := rec.Header().Get("Location")
			token := strings.TrimPrefix(show, "/escalated/guest/")

			page := visit(t, h, show, "", "")
			if page.Component != "Escalated/Guest/Show" {
				t.Fatalf("show component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "ticket", "token")
			if page.Props["token"] != token {
				t.Errorf("token %v", page.Props["token"])
			}
			ticket := obj(t, page.Props, "ticket")
			hasKeys(t, "ticket", ticket, "reference", "subject", "description", "status", "priority", "channel", "department",
				"attachments", "replies", "satisfaction_rating", "guest_access_expires_at", "resolved_at", "closed_at", "created_at", "updated_at")
			if ticket["status"] != "open" || ticket["priority"] != "high" || ticket["guest_access_expires_at"] == nil {
				t.Errorf("ticket %v", ticket)
			}
			if _, ok := ticket["id"]; ok {
				t.Error("the guest page carries the internal id")
			}

			wantBack(t, postPage(h, http.MethodPost, show+"/reply", "", "", show, url.Values{"body": {"Any update?"}}), "reply", show)
			replies := list(t, obj(t, visit(t, h, show, "", "").Props, "ticket"), "replies")
			if len(replies) != 1 {
				t.Fatalf("replies %v", replies)
			}
			reply := replies[0].(map[string]any)
			hasKeys(t, "reply", reply, "id", "body", "is_internal_note", "is_pinned", "author", "attachments", "created_at")
			if obj(t, reply, "author")["name"] != "Gina" {
				t.Errorf("reply author %v", reply["author"])
			}
			wantPageErrors(t, postPage(h, http.MethodPost, show+"/reply", "", "", show, url.Values{"body": {""}}), "empty reply", "Escalated/Guest/Show", "body")

			// Rating needs a resolved ticket; then it is taken once.
			wantPageErrors(t, postPage(h, http.MethodPost, show+"/rate", "", "", show, url.Values{"rating": {"5"}}), "rate open", "Escalated/Guest/Show", "rating")
			var id int64
			db := f.esc.Config.DB
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT id FROM escalated_tickets WHERE subject = ?`), "Lost access").Scan(&id); err != nil {
				t.Fatal(err)
			}
			agent := models.UserID(uiAgent)
			if err := f.esc.NewTicketService().ChangeStatus(context.Background(), id, models.StatusResolved, &agent); err != nil {
				t.Fatal(err)
			}
			wantBack(t, postPage(h, http.MethodPost, show+"/rate", "", "", show, url.Values{"rating": {"4"}, "comment": {"Thanks"}}), "rate", show)
			if got := obj(t, obj(t, visit(t, h, show, "", "").Props, "ticket"), "satisfaction_rating"); got["rating"].(float64) != 4 {
				t.Errorf("rating %v", got)
			}

			// Closed tickets take no replies; a bad token opens nothing.
			if err := f.esc.NewTicketService().ChangeStatus(context.Background(), id, models.StatusClosed, &agent); err != nil {
				t.Fatal(err)
			}
			wantPageErrors(t, postPage(h, http.MethodPost, show+"/reply", "", "", show, url.Values{"body": {"Hello?"}}), "closed", "Escalated/Guest/Show", "body")
			if rec := send(h, http.MethodGet, "/escalated/guest/not-a-real-token", "", "", ""); rec.Code != http.StatusNotFound {
				t.Errorf("bad token: %d", rec.Code)
			}
		})
	}
}
