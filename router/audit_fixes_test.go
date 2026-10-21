package router_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// An attachment is sender-controlled content (inbound mail keeps the part's
// MIME type). Serving it inline with that type rendered HTML/SVG on the app
// origin, and a quote in the filename broke the header. Downloads must always
// be opaque, un-sniffable attachments.
func TestAttachmentDownloadIsNeverRenderedInline(t *testing.T) {
	f := newGuardFixture(t)
	path := filepath.Join(t.TempDir(), "x.html")
	body := "<script>alert(document.domain)</script>"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &models.Attachment{
		TicketID:         f.ticketOfB,
		OriginalFilename: "../evil\"; x=\r\nscript.html",
		MimeType:         "text/html",
		Size:             int64(len(body)),
		StoragePath:      path,
	}
	if err := f.esc.Store.CreateAttachment(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	url := "/escalated/attachments/" + strconv.FormatInt(a.ID, 10) + "/download"

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec := send(h, http.MethodGet, url, customerB, "", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("download: got %d: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
				t.Errorf("Content-Type = %q, want application/octet-stream", got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			cd := rec.Header().Get("Content-Disposition")
			if !strings.HasPrefix(cd, "attachment; ") {
				t.Errorf("Content-Disposition = %q, want attachment", cd)
			}
			want := `attachment; filename="evil__ x=script.html"; filename*=UTF-8''evil%22%3B%20x%3Dscript.html`
			if cd != want {
				t.Errorf("Content-Disposition = %q, want %q", cd, want)
			}
			if strings.Contains(rec.Header().Get("Cache-Control"), "public") || rec.Header().Get("Cache-Control") == "" {
				t.Errorf("Cache-Control = %q, want private no-store", rec.Header().Get("Cache-Control"))
			}
		})
	}
}

// POST /tickets/{id}/rate used to accept a rating from any signed-in user for
// any resolved ticket, spending the requester's single rating.
func TestTicketRateIsLimitedToTheRequester(t *testing.T) {
	f := newGuardFixture(t)
	if _, err := f.db.Exec(sqldialect.Rebind(f.db, `UPDATE escalated_tickets SET status = ? WHERE id = ?`), models.StatusResolved, f.ticketOfB); err != nil {
		t.Fatal(err)
	}
	path := "/escalated/tickets/" + strconv.FormatInt(f.ticketOfB, 10) + "/rate"
	ratings := func() int {
		var n int
		if err := f.db.QueryRow(sqldialect.Rebind(f.db, `SELECT COUNT(*) FROM escalated_satisfaction_ratings WHERE ticket_id = ?`), f.ticketOfB).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	mounts := f.mounts()
	for _, name := range []string{"chi", "stdlib"} {
		h := mounts[name]
		if rec := send(h, http.MethodPost, path, customerA, "", `{"rating":1}`); rec.Code != http.StatusForbidden {
			t.Errorf("%s: customer A rating B's ticket: got %d, want 403: %s", name, rec.Code, rec.Body.String())
		}
		if rec := send(h, http.MethodPost, path, "", "", `{"rating":1}`); !denied(rec.Code) {
			t.Errorf("%s: anonymous rating: got %d, want 401/403", name, rec.Code)
		}
	}
	if n := ratings(); n != 0 {
		t.Fatalf("denied ratings were stored: %d", n)
	}

	if rec := sendAPI(mounts["chi"], http.MethodPost, path, customerB, "", `{"rating":5}`); rec.Code != http.StatusCreated {
		t.Fatalf("requester rating: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if rec := sendAPI(mounts["stdlib"], http.MethodPost, path, customerB, "", `{"rating":4}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("second rating: got %d, want 422", rec.Code)
	}
	var ratedBy models.UserID
	if err := f.db.QueryRow(sqldialect.Rebind(f.db, `SELECT rated_by_id FROM escalated_satisfaction_ratings WHERE ticket_id = ?`), f.ticketOfB).Scan(&ratedBy); err != nil {
		t.Fatal(err)
	}
	if ratedBy != customerB {
		t.Errorf("rated_by_id = %q, want %q", ratedBy, customerB)
	}
}

// PATCH /api/tickets/{id} wrote status and priority straight to the row: any
// integer, no activity, no resolved_at, no status/priority events.
func TestAPITicketPatchGoesThroughTheLifecycle(t *testing.T) {
	f := newGuardFixture(t)
	path := "/escalated/api/tickets/" + strconv.FormatInt(f.ticketOfB, 10)
	h := f.mounts()["chi"]

	status, priority := f.statusAndPriority(t)
	for _, body := range []string{`{"status":99}`, `{"priority":17}`, `{"status":"nonsense","subject":"x"}`} {
		if rec := send(h, http.MethodPatch, path, "", "agent", body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH %s: got %d, want 422: %s", body, rec.Code, rec.Body.String())
		}
	}
	if s, p := f.statusAndPriority(t); s != status || p != priority {
		t.Fatalf("invalid PATCH changed status/priority to %d/%d", s, p)
	}

	rec := send(h, http.MethodPatch, path, "", "agent", `{"status":"resolved","priority":3,"subject":"Renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: got %d: %s", rec.Code, rec.Body.String())
	}
	if s, p := f.statusAndPriority(t); s != models.StatusResolved || p != models.PriorityUrgent {
		t.Fatalf("status/priority = %d/%d, want resolved/urgent", s, p)
	}
	var resolvedAt *string
	var subject string
	if err := f.db.QueryRow(sqldialect.Rebind(f.db, `SELECT CAST(resolved_at AS TEXT), subject FROM escalated_tickets WHERE id = ?`), f.ticketOfB).Scan(&resolvedAt, &subject); err != nil {
		t.Fatal(err)
	}
	if resolvedAt == nil || subject != "Renamed" {
		t.Errorf("resolved_at = %v, subject = %q", resolvedAt, subject)
	}
	var acts int
	if err := f.db.QueryRow(sqldialect.Rebind(f.db, `SELECT COUNT(*) FROM escalated_ticket_activities WHERE ticket_id = ? AND action IN (?, ?)`),
		f.ticketOfB, models.ActionStatusChanged, models.ActionPriorityChanged).Scan(&acts); err != nil {
		t.Fatal(err)
	}
	if acts != 2 {
		t.Errorf("status/priority activities = %d, want 2", acts)
	}
}

// middleware.RequireKBEnabled/RequireKBPublic existed but were never mounted:
// the public KB API and the widget served articles with the KB switched off
// or private.
func TestKnowledgeBaseSettingsAreEnforced(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	ctx := context.Background()
	set := func(k, v string) {
		t.Helper()
		if err := f.esc.Store.SetSetting(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			set("knowledge_base_enabled", "1")
			set("knowledge_base_public", "1")
			if rec := send(h, http.MethodGet, "/escalated/api/kb/articles", "", "", ""); rec.Code != http.StatusOK {
				t.Fatalf("public KB: got %d", rec.Code)
			}

			set("knowledge_base_public", "0")
			if rec := send(h, http.MethodGet, "/escalated/api/kb/articles", "", "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("private KB, signed out: got %d, want 403", rec.Code)
			}
			if rec, _ := widgetCall(t, h, http.MethodGet, "/articles/reset-password", nil); rec.Code != http.StatusForbidden {
				t.Errorf("private KB, widget article: got %d, want 403", rec.Code)
			}
			if _, out := widgetCall(t, h, http.MethodGet, "/config", nil); out["kb_enabled"] != false {
				t.Errorf("private KB: widget kb_enabled = %v, want false", out["kb_enabled"])
			}

			set("knowledge_base_enabled", "0")
			if rec := send(h, http.MethodGet, "/escalated/api/kb/articles/reset-password", "", "", ""); rec.Code != http.StatusNotFound {
				t.Errorf("disabled KB: got %d, want 404", rec.Code)
			}
			if rec, _ := widgetCall(t, h, http.MethodGet, "/articles?q=password", nil); rec.Code != http.StatusNotFound {
				t.Errorf("disabled KB, widget search: got %d, want 404", rec.Code)
			}
		})
	}
}
