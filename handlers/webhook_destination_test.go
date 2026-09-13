package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
)

// A webhook URL is a request the server makes on the admin's behalf. Accepting
// any URL let an admin (or anyone holding an admin session) point deliveries at
// loopback services, the private network or the cloud metadata endpoint.
func TestWebhookCreateAndUpdateRefuseNonPublicDestinations(t *testing.T) {
	db := testdb.Open(t)
	h := NewWebhookHandler(db, nil)

	send := func(fn http.HandlerFunc, method, id, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/admin/webhooks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if id != "" {
			req.SetPathValue("id", id)
		}
		rec := httptest.NewRecorder()
		fn(rec, req)
		return rec
	}
	count := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_webhooks`)).Scan(&n); err != nil {
			t.Fatalf("count webhooks: %v", err)
		}
		return n
	}

	for _, url := range []string{
		"http://127.0.0.1:8080/hook",
		"http://localhost/hook",
		"http://10.0.0.5/hook",
		"http://172.16.4.2/hook",
		"http://192.168.1.10/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/hook",
		"http://[::1]/hook",
		"http://[fe80::1]/hook",
		"http://[fd00::1]/hook",
		"http://[::ffff:127.0.0.1]/hook",
		"ftp://93.184.215.14/hook",
		"http:///no-host",
	} {
		body := `{"url":"` + url + `","events":["ticket.created"]}`
		if rec := send(h.Create, http.MethodPost, "", body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("POST url %s: got %d, want 422: %s", url, rec.Code, rec.Body.String())
		}
	}
	if n := count(); n != 0 {
		t.Fatalf("%d refused webhook(s) were stored", n)
	}

	// A public address is still accepted. An IP literal keeps the test off DNS.
	rec := send(h.Create, http.MethodPost, "", `{"url":"https://93.184.215.14/hook","events":["ticket.created"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST public url: got %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var id, stored string
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT CAST(id AS TEXT), url FROM escalated_webhooks`)).Scan(&id, &stored); err != nil {
		t.Fatalf("read webhook: %v", err)
	}

	if rec := send(h.Update, http.MethodPatch, id, `{"url":"http://169.254.169.254/latest/meta-data/"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("PATCH to a link-local url: got %d, want 422: %s", rec.Code, rec.Body.String())
	}
	var after string
	if err := db.QueryRow(sqldialect.Rebind(db, `SELECT url FROM escalated_webhooks`)).Scan(&after); err != nil {
		t.Fatalf("read webhook: %v", err)
	}
	if after != stored {
		t.Errorf("refused PATCH changed url to %q, want %q", after, stored)
	}
}
