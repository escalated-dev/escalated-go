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

	if rec := send(mounts["chi"], http.MethodPost, path, customerB, "", `{"rating":5}`); rec.Code != http.StatusCreated {
		t.Fatalf("requester rating: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if rec := send(mounts["stdlib"], http.MethodPost, path, customerB, "", `{"rating":4}`); rec.Code != http.StatusUnprocessableEntity {
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
