package router_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// Only /agent and /admin were wrapped in middleware. The JSON API, the customer
// ticket routes and attachment downloads were reachable by anyone: the API
// listed every ticket, returned internal notes and wrote status and priority;
// a customer could open or reply to any ticket by id; any attachment could be
// downloaded by id. These tests pin who may reach each surface, through both
// routers.

const (
	headerUser = "X-Test-User"
	headerRole = "X-Test-Role"

	customerA = "101"
	customerB = "202"
)

type guardFixture struct {
	db                 *sql.DB
	esc                *escalated.Escalated
	ticketOfB          int64
	publicAttachment   int64
	internalAttachment int64
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	db := testdb.Open(t)

	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.UIEnabled = true
	cfg.AgentCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "agent" }
	cfg.AdminCheck = func(r *http.Request) bool { return r.Header.Get(headerRole) == "admin" }
	cfg.UserIDFunc = func(r *http.Request) models.UserID { return models.UserID(r.Header.Get(headerUser)) }

	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}

	ctx := context.Background()
	tickets := services.NewTicketService(esc.Store)
	userType := "User"
	requester := models.UserID(customerB)
	ticket, err := tickets.Create(ctx, services.CreateTicketInput{
		Subject:       "Ticket of customer B",
		Description:   "Only B and agents may see this",
		RequesterType: &userType,
		RequesterID:   &requester,
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	agent := models.UserID("900")
	note, err := tickets.AddReply(ctx, ticket.ID, "internal note for agents", &userType, &agent, true)
	if err != nil {
		t.Fatalf("add internal note: %v", err)
	}

	dir := t.TempDir()
	attach := func(replyID *int64, name, body string) int64 {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write attachment: %v", err)
		}
		a := &models.Attachment{
			TicketID:         ticket.ID,
			ReplyID:          replyID,
			OriginalFilename: name,
			MimeType:         "text/plain",
			Size:             int64(len(body)),
			StoragePath:      path,
		}
		if err := esc.Store.CreateAttachment(ctx, a); err != nil {
			t.Fatalf("create attachment: %v", err)
		}
		return a.ID
	}

	return &guardFixture{
		db:                 db,
		esc:                esc,
		ticketOfB:          ticket.ID,
		publicAttachment:   attach(nil, "public.txt", "public file"),
		internalAttachment: attach(&note.ID, "internal.txt", "internal file"),
	}
}

func (f *guardFixture) mounts() map[string]http.Handler {
	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	mux := http.NewServeMux()
	router.MountStdlib(mux, f.esc)
	return map[string]http.Handler{"chi": r, "stdlib": mux}
}

// send issues a request as user (may be empty) with role (agent, admin or
// empty). X-Inertia makes the customer pages answer with JSON.
func send(h http.Handler, method, path, user, role, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("X-Inertia", "true")
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

func denied(code int) bool {
	return code == http.StatusUnauthorized || code == http.StatusForbidden
}

func (f *guardFixture) statusAndPriority(t *testing.T) (int, int) {
	t.Helper()
	var status, priority int
	if err := f.db.QueryRow(
		sqldialect.Rebind(f.db, `SELECT status, priority FROM escalated_tickets WHERE id = ?`), f.ticketOfB,
	).Scan(&status, &priority); err != nil {
		t.Fatalf("read ticket: %v", err)
	}
	return status, priority
}

func (f *guardFixture) repliesWithBody(t *testing.T, body string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(
		sqldialect.Rebind(f.db, `SELECT COUNT(*) FROM escalated_replies WHERE ticket_id = ? AND body = ?`), f.ticketOfB, body,
	).Scan(&n); err != nil {
		t.Fatalf("count replies: %v", err)
	}
	return n
}

func TestJSONAPIRequiresAnAgent(t *testing.T) {
	f := newGuardFixture(t)
	id := strconv.FormatInt(f.ticketOfB, 10)

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			status, priority := f.statusAndPriority(t)

			for _, c := range []struct{ method, path, body string }{
				{http.MethodGet, "/escalated/api/tickets", ""},
				{http.MethodGet, "/escalated/api/tickets/" + id, ""},
				{http.MethodPatch, "/escalated/api/tickets/" + id, `{"status":6,"priority":4}`},
				{http.MethodPost, "/escalated/api/tickets/" + id + "/replies", `{"body":"api reply"}`},
				{http.MethodPost, "/escalated/api/tickets", `{"subject":"s","description":"d"}`},
				{http.MethodGet, "/escalated/api/departments", ""},
				{http.MethodGet, "/escalated/api/tags", ""},
			} {
				if rec := send(h, c.method, c.path, "", "", c.body); !denied(rec.Code) {
					t.Errorf("anonymous %s %s: got %d, want 401 or 403: %s", c.method, c.path, rec.Code, rec.Body.String())
				}
				if rec := send(h, c.method, c.path, customerB, "", c.body); !denied(rec.Code) {
					t.Errorf("customer %s %s: got %d, want 401 or 403: %s", c.method, c.path, rec.Code, rec.Body.String())
				}
			}

			if s, p := f.statusAndPriority(t); s != status || p != priority {
				t.Errorf("status/priority changed to %d/%d by a denied PATCH, want %d/%d", s, p, status, priority)
			}
			if n := f.repliesWithBody(t, "api reply"); n != 0 {
				t.Errorf("denied API reply was written %d time(s)", n)
			}

			if rec := send(h, http.MethodGet, "/escalated/api/tickets", "", "agent", ""); rec.Code != http.StatusOK {
				t.Errorf("agent GET /api/tickets: got %d, want 200: %s", rec.Code, rec.Body.String())
			}
			rec := send(h, http.MethodGet, "/escalated/api/tickets/"+id, "", "agent", "")
			if rec.Code != http.StatusOK {
				t.Errorf("agent GET /api/tickets/%s: got %d, want 200: %s", id, rec.Code, rec.Body.String())
			} else if !strings.Contains(rec.Body.String(), "internal note for agents") {
				t.Errorf("agent ticket view is missing the internal note: %s", rec.Body.String())
			}
			if rec := send(h, http.MethodGet, "/escalated/api/tickets", "", "admin", ""); rec.Code != http.StatusOK {
				t.Errorf("admin GET /api/tickets: got %d, want 200: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// The knowledge base and the auth callbacks are public by design; guarding the
// ticket API must not take them with it.
func TestPublicAPIRoutesStayOpen(t *testing.T) {
	f := newGuardFixture(t)

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if rec := send(h, http.MethodGet, "/escalated/api/kb/articles", "", "", ""); rec.Code != http.StatusOK {
				t.Errorf("anonymous GET /api/kb/articles: got %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if rec := send(h, http.MethodPost, "/escalated/api/auth/login", "", "", `{}`); denied(rec.Code) {
				t.Errorf("anonymous POST /api/auth/login: got %d, want it to reach the handler", rec.Code)
			}
		})
	}
}

func TestCustomerTicketRoutesRequireTheRequester(t *testing.T) {
	f := newGuardFixture(t)
	id := strconv.FormatInt(f.ticketOfB, 10)
	show := "/escalated/tickets/" + id
	replies := show + "/replies"

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for _, c := range []struct{ method, path, body string }{
				{http.MethodGet, "/escalated/tickets", ""},
				{http.MethodPost, "/escalated/tickets", `{"subject":"s","description":"d"}`},
				{http.MethodGet, show, ""},
				{http.MethodPost, replies, `{"body":"anonymous reply"}`},
			} {
				if rec := send(h, c.method, c.path, "", "", c.body); !denied(rec.Code) {
					t.Errorf("anonymous %s %s: got %d, want 401 or 403: %s", c.method, c.path, rec.Code, rec.Body.String())
				}
			}
			if n := f.repliesWithBody(t, "anonymous reply"); n != 0 {
				t.Errorf("anonymous reply was written %d time(s)", n)
			}

			if rec := send(h, http.MethodGet, show, customerA, "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("customer A GET %s (B's ticket): got %d, want 403: %s", show, rec.Code, rec.Body.String())
			}
			if rec := send(h, http.MethodPost, replies, customerA, "", `{"body":"not my ticket"}`); rec.Code != http.StatusForbidden {
				t.Errorf("customer A POST %s (B's ticket): got %d, want 403: %s", replies, rec.Code, rec.Body.String())
			}
			if n := f.repliesWithBody(t, "not my ticket"); n != 0 {
				t.Errorf("customer A's reply on B's ticket was written %d time(s)", n)
			}
			if rec := send(h, http.MethodGet, "/escalated/tickets", customerA, "", ""); rec.Code != http.StatusOK {
				t.Errorf("customer A GET /tickets: got %d, want 200: %s", rec.Code, rec.Body.String())
			} else if strings.Contains(rec.Body.String(), "Ticket of customer B") {
				t.Errorf("customer A's ticket list includes B's ticket: %s", rec.Body.String())
			}

			rec := send(h, http.MethodGet, show, customerB, "", "")
			if rec.Code != http.StatusOK {
				t.Errorf("customer B GET %s: got %d, want 200: %s", show, rec.Code, rec.Body.String())
			} else if strings.Contains(rec.Body.String(), "internal note for agents") {
				t.Errorf("customer view exposes the internal note: %s", rec.Body.String())
			}
			if rec := send(h, http.MethodPost, replies, customerB, "", `{"body":"reply from B via `+name+`"}`); rec.Code != http.StatusCreated {
				t.Errorf("customer B POST %s: got %d, want 201: %s", replies, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAttachmentDownloadIsAuthorizedThroughItsTicket(t *testing.T) {
	f := newGuardFixture(t)
	url := func(id int64) string { return "/escalated/attachments/" + strconv.FormatInt(id, 10) + "/download" }
	public := url(f.publicAttachment)
	internal := url(f.internalAttachment)

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			if rec := send(h, http.MethodGet, public, "", "", ""); !denied(rec.Code) {
				t.Errorf("anonymous download: got %d, want 401 or 403: %s", rec.Code, rec.Body.String())
			}
			if rec := send(h, http.MethodGet, url(f.internalAttachment+1000), "", "", ""); !denied(rec.Code) {
				t.Errorf("anonymous download of a missing id: got %d, want 401 or 403 before any lookup", rec.Code)
			}
			if rec := send(h, http.MethodGet, public, customerA, "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("customer A downloading from B's ticket: got %d, want 403: %s", rec.Code, rec.Body.String())
			}
			if rec := send(h, http.MethodGet, internal, customerB, "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("customer B downloading an internal-note attachment: got %d, want 403: %s", rec.Code, rec.Body.String())
			}

			for _, who := range []struct{ user, role, path, want string }{
				{customerB, "", public, "public file"},
				{"", "agent", public, "public file"},
				{"", "admin", public, "public file"},
				{"", "agent", internal, "internal file"},
			} {
				rec := send(h, http.MethodGet, who.path, who.user, who.role, "")
				if rec.Code != http.StatusOK || rec.Body.String() != who.want {
					t.Errorf("download %s as user=%q role=%q: got %d %q, want 200 %q", who.path, who.user, who.role, rec.Code, rec.Body.String(), who.want)
				}
			}
		})
	}
}
