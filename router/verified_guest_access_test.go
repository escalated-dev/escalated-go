package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// Verified guest access, as escalated-laravel docs/guest-access.md specifies
// it and the shared widget (useGuestVerification.js, EscalatedWidget.vue) and
// guest clients call it. Every case runs through MountChi and MountStdlib.

type guestFixture struct {
	esc    *escalated.Escalated
	mailer *codeMailer
	h      http.Handler
}

func eachGuestMount(t *testing.T, tweak func(*escalated.Config), fn func(t *testing.T, f guestFixture)) {
	t.Helper()
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			cfg := escalated.DefaultConfig()
			cfg.DB = testdb.Open(t)
			cfg.UIEnabled = false
			cfg.EnableWidget = true
			cfg.Widget.Enabled = true
			mailer := newCodeMailer()
			cfg.Mailer = mailer
			cfg.GuestAccess.Secret = "test-secret"
			// The per-IP route limits have their own tests; these send many
			// requests from one address.
			cfg.GuestRateLimit.Disabled = true
			if tweak != nil {
				tweak(&cfg)
			}
			esc, err := escalated.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var h http.Handler
			if name == "chi" {
				r := chi.NewRouter()
				router.MountChi(r, esc)
				h = r
			} else {
				mux := http.NewServeMux()
				router.MountStdlib(mux, esc)
				h = mux
			}
			fn(t, guestFixture{esc: esc, mailer: mailer, h: h})
		})
	}
}

func (f guestFixture) call(t *testing.T, method, path string, body any, headers ...string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/escalated"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.RemoteAddr = "203.0.113.50:5000"
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "RemoteAddr" {
			req.RemoteAddr = headers[i+1]
			continue
		}
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// requestCode asks for a code over HTTP and returns the proof fields.
func (f guestFixture) requestCode(t *testing.T, surface, email, purpose string) map[string]any {
	t.Helper()
	// A client of its own per code, so the 3-an-hour per-client budget does
	// not get in the way of tests that need several codes for one mailbox.
	ip := fmt.Sprintf("198.18.0.%d:1", proofClient.Add(1)%250)
	rec, out := f.call(t, http.MethodPost, surface+"/verification", map[string]any{"email": email, "purpose": purpose}, "RemoteAddr", ip)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%s/verification: %d %s", surface, rec.Code, rec.Body.String())
	}
	id, _ := out["verification_id"].(string)
	return map[string]any{"verification_id": id, "verification_code": f.mailer.code(services.NormalizeGuestEmail(email))}
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestGuestVerificationEmailsACodeOnBothSurfaces(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		for _, surface := range []string{"/api/guest", "/widget"} {
			rec, out := f.call(t, http.MethodPost, surface+"/verification", map[string]any{"email": " Pat@Example.com ", "purpose": "ticket"})
			if rec.Code != http.StatusAccepted || out["expires_in"] != float64(600) || out["message"] == nil {
				t.Fatalf("%s: %d %s", surface, rec.Code, rec.Body.String())
			}
			id, _ := out["verification_id"].(string)
			if len(id) != 36 {
				t.Errorf("verification_id = %q, want a uuid", id)
			}
			code := f.mailer.code("pat@example.com")
			if len(code) != 8 || strings.Contains(rec.Body.String(), code) {
				t.Errorf("code %q must be 8 digits and only in the email", code)
			}
			if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("privacy headers missing: %v", rec.Header())
			}
		}

		var stored string
		if err := f.esc.Config.DB.QueryRow(`SELECT code_hash FROM escalated_guest_verifications LIMIT 1`).Scan(&stored); err != nil || len(stored) != 64 {
			t.Errorf("stored code_hash = %q (%v), want a digest", stored, err)
		}

		for _, body := range []map[string]any{{"email": "nope", "purpose": "ticket"}, {"email": "a@b.co", "purpose": "other"}} {
			if rec, out := f.call(t, http.MethodPost, "/widget/verification", body); rec.Code != http.StatusUnprocessableEntity || out["errors"] == nil {
				t.Errorf("%v: %d %s", body, rec.Code, rec.Body.String())
			}
		}
	})
}

func TestGuestCodeBudgets(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		ask := func(ip string) *httptest.ResponseRecorder {
			rec, _ := f.call(t, http.MethodPost, "/api/guest/verification", map[string]any{"email": "box@example.com", "purpose": "ticket"}, "RemoteAddr", ip+":1")
			return rec
		}
		for i := 0; i < 3; i++ {
			if rec := ask("198.51.100.1"); rec.Code != http.StatusAccepted {
				t.Fatalf("code %d: %d", i+1, rec.Code)
			}
		}
		rec := ask("198.51.100.1")
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("4th code from one client: %d Retry-After=%q", rec.Code, rec.Header().Get("Retry-After"))
		}
		// Other clients still get codes, up to 10 for the mailbox.
		for i := 2; i <= 8; i++ {
			if rec := ask("198.51.100." + string(rune('0'+i))); rec.Code != http.StatusAccepted {
				t.Fatalf("client %d: %d", i, rec.Code)
			}
		}
		if rec := ask("198.51.100.9"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("11th code for the mailbox: %d, want 429", rec.Code)
		}
		if n := f.mailer.count(); n != 10 {
			t.Errorf("sent %d codes, want 10", n)
		}
	})
}

func TestWidgetVerifiedTicketAndStatus(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		ticket := map[string]any{"name": "Ada", "email": "ada@example.com", "subject": "Billing", "description": "Charged twice"}

		if rec, out := f.call(t, http.MethodPost, "/widget/tickets", ticket); rec.Code != http.StatusUnprocessableEntity || out["errors"].(map[string]any)["verification_code"] == nil {
			t.Fatalf("create without a code: %d %s", rec.Code, rec.Body.String())
		}
		wrong := map[string]any{"verification_id": f.requestCode(t, "/widget", "ada@example.com", "ticket")["verification_id"], "verification_code": "00000000"}
		if rec, _ := f.call(t, http.MethodPost, "/widget/tickets", merge(ticket, wrong)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("wrong code: %d", rec.Code)
		}
		chatCode := f.requestCode(t, "/widget", "ada@example.com", "chat")
		if rec, _ := f.call(t, http.MethodPost, "/widget/tickets", merge(ticket, chatCode)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("chat code used for a ticket: %d", rec.Code)
		}
		other := f.requestCode(t, "/widget", "eve@example.com", "ticket")
		if rec, _ := f.call(t, http.MethodPost, "/widget/tickets", merge(ticket, other)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("another mailbox's code: %d", rec.Code)
		}

		proof := f.requestCode(t, "/widget", "ada@example.com", "ticket")
		rec, out := f.call(t, http.MethodPost, "/widget/tickets", merge(ticket, proof))
		if rec.Code != http.StatusCreated {
			t.Fatalf("verified create: %d %s", rec.Code, rec.Body.String())
		}
		ref, _ := out["reference"].(string)
		grant, _ := out["guest_access_token"].(string)
		if ref == "" || len(grant) < 40 || out["expires_at"] == nil || out["guest_token"] != nil {
			t.Fatalf("create response = %v", out)
		}
		if rec, _ := f.call(t, http.MethodPost, "/widget/tickets", merge(ticket, proof)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("code reused: %d, want 422", rec.Code)
		}

		rec, out = f.call(t, http.MethodGet, "/widget/tickets/"+ref, nil, "Authorization", "Bearer "+grant)
		if rec.Code != http.StatusOK || out["reference"] != ref || out["status"] != "open" || out["replies"] == nil {
			t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("status response is cacheable")
		}
		for name, h := range map[string][]string{
			"no grant":     nil,
			"wrong ref":    {"Authorization", "Bearer " + grant},
			"forged grant": {"Authorization", "Bearer " + grant[:len(grant)-2] + "AA"},
			"truncated":    {"Authorization", "Bearer " + grant[:30]},
		} {
			path := "/widget/tickets/" + ref
			if name == "wrong ref" {
				path = "/widget/tickets/ESC-NOPE"
			}
			if rec, _ := f.call(t, http.MethodGet, path, nil, h...); rec.Code != http.StatusNotFound {
				t.Errorf("%s: %d, want 404", name, rec.Code)
			}
		}

		// Lookup rotates the grant; the earlier one stops working.
		lookup := f.requestCode(t, "/widget", "ada@example.com", "lookup")
		rec, out = f.call(t, http.MethodPost, "/widget/lookup", merge(map[string]any{"email": "ada@example.com", "reference": ref}, lookup))
		data, _ := out["data"].([]any)
		if rec.Code != http.StatusOK || len(data) != 1 {
			t.Fatalf("lookup: %d %s", rec.Code, rec.Body.String())
		}
		match := data[0].(map[string]any)
		fresh, _ := match["guest_access_token"].(string)
		if match["reference"] != ref || match["subject"] != "Billing" || fresh == "" || fresh == grant || match["expires_at"] == nil {
			t.Fatalf("lookup match = %v", match)
		}
		if rec, _ := f.call(t, http.MethodGet, "/widget/tickets/"+ref, nil, "Authorization", "Bearer "+grant); rec.Code != http.StatusNotFound {
			t.Errorf("rotated grant still opens the ticket: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodGet, "/widget/tickets/"+ref, nil, "Authorization", "Bearer "+fresh); rec.Code != http.StatusOK {
			t.Errorf("fresh grant: %d", rec.Code)
		}

		// A verified address that is not the ticket's finds nothing, and the
		// code is still spent.
		eve := f.requestCode(t, "/widget", "eve@example.com", "lookup")
		rec, out = f.call(t, http.MethodPost, "/widget/lookup", merge(map[string]any{"email": "eve@example.com", "reference": ref}, eve))
		if data, _ := out["data"].([]any); rec.Code != http.StatusOK || len(data) != 0 {
			t.Fatalf("lookup by another mailbox: %d %s", rec.Code, rec.Body.String())
		}
		if rec, _ := f.call(t, http.MethodPost, "/widget/lookup", merge(map[string]any{"email": "eve@example.com", "reference": ref}, eve)); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("lookup code reused: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodPost, "/widget/lookup", map[string]any{"email": "ada@example.com", "reference": ref}); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("lookup without a code: %d", rec.Code)
		}
	})
}

func TestGuestCodeAllowsFiveGuesses(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		ticket := map[string]any{"guest_name": "Pat", "guest_email": "pat@example.com", "subject": "S", "description": "D"}
		proof := f.requestCode(t, "/api/guest", "pat@example.com", "ticket")
		for i := 0; i < 5; i++ {
			bad := merge(proof, map[string]any{"verification_code": "99999999"})
			if proof["verification_code"] == "99999999" {
				bad["verification_code"] = "88888888"
			}
			if rec, _ := f.call(t, http.MethodPost, "/api/guest/tickets", merge(ticket, bad)); rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("guess %d: %d", i+1, rec.Code)
			}
		}
		if rec, _ := f.call(t, http.MethodPost, "/api/guest/tickets", merge(ticket, proof)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("right code after five wrong guesses: %d, want 422", rec.Code)
		}
	})
}

func TestGuestAPIVerifiedTicketRepliesAndRating(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		proof := f.requestCode(t, "/api/guest", "pat@example.com", "ticket")
		rec, out := f.call(t, http.MethodPost, "/api/guest/tickets", merge(map[string]any{
			"guest_name": "Pat", "guest_email": "pat@example.com", "subject": "Help", "description": "Please",
		}, proof))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		data := out["data"].(map[string]any)
		grant, _ := data["guest_access_token"].(string)
		if grant == "" || data["guest_token"] != grant || data["guest_access_expires_at"] == nil {
			t.Fatalf("create data = %v", data)
		}

		if rec, _ := f.call(t, http.MethodPost, "/api/guest/tickets/"+grant+"/replies", map[string]any{"body": "More detail", "email": "PAT@example.com"}); rec.Code != http.StatusCreated {
			t.Fatalf("reply: %d %s", rec.Code, rec.Body.String())
		}
		if rec, _ := f.call(t, http.MethodPost, "/api/guest/tickets/"+grant+"/replies", map[string]any{"body": "x", "email": "eve@example.com"}); rec.Code != http.StatusForbidden {
			t.Errorf("reply with another email: %d, want 403", rec.Code)
		}
		rec, out = f.call(t, http.MethodGet, "/api/guest/tickets/"+grant, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("show: %d", rec.Code)
		}
		replies := out["data"].(map[string]any)["replies"].([]any)
		if len(replies) != 1 || replies[0].(map[string]any)["body"] != "More detail" {
			t.Fatalf("replies = %v", replies)
		}
		if rec, _ := f.call(t, http.MethodGet, "/api/guest/tickets/not-a-grant", nil); rec.Code != http.StatusNotFound {
			t.Errorf("unknown token: %d", rec.Code)
		}

		ref := out["data"].(map[string]any)["reference"].(string)
		tk, _ := f.esc.Store.GetTicketByReference(context.Background(), ref)
		if err := f.esc.NewTicketService().ChangeStatus(context.Background(), tk.ID, 5, nil); err != nil {
			t.Fatal(err)
		}
		if rec, _ := f.call(t, http.MethodPost, "/api/guest/tickets/"+grant+"/rate", map[string]any{"rating": 5}); rec.Code != http.StatusCreated {
			t.Errorf("rate with grant: %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestChatStartNeedsAChatCode(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		body := map[string]any{"name": "Grace", "email": "grace@example.com", "message": "Hi"}
		if rec, _ := f.call(t, http.MethodPost, "/widget/chat/start", body); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("chat without a code: %d", rec.Code)
		}
		ticketCode := f.requestCode(t, "/widget", "grace@example.com", "ticket")
		if rec, _ := f.call(t, http.MethodPost, "/widget/chat/start", merge(body, ticketCode)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("ticket code for chat: %d", rec.Code)
		}
		rec, out := f.call(t, http.MethodPost, "/widget/chat/start", merge(body, f.requestCode(t, "/widget", "grace@example.com", "chat")))
		if rec.Code != http.StatusCreated || out["id"] == nil {
			t.Fatalf("chat with code: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// Permanent guest tokens issued before verified access keep working during
// the transition, stop once a grant is issued for their ticket, and stop
// everywhere with RejectLegacyTokens.
func TestLegacyGuestTokens(t *testing.T) {
	legacyTicket := func(t *testing.T, f guestFixture) (string, string) {
		email := "old@example.com"
		tk, err := f.esc.NewTicketService().Create(context.Background(), services.CreateTicketInput{Subject: "Old", Description: "D", GuestEmail: &email})
		if err != nil || tk.GuestToken == nil {
			t.Fatalf("legacy ticket: %v", err)
		}
		return *tk.GuestToken, tk.Reference
	}

	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		token, ref := legacyTicket(t, f)
		if rec, _ := f.call(t, http.MethodGet, "/api/guest/tickets/"+token, nil); rec.Code != http.StatusOK {
			t.Fatalf("legacy token during the transition: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodGet, "/widget/tickets/lookup?ref="+ref+"&token="+token, nil); rec.Code != http.StatusOK {
			t.Fatalf("legacy widget lookup: %d", rec.Code)
		}
		proof := f.requestCode(t, "/api/guest", "old@example.com", "lookup")
		if rec, _ := f.call(t, http.MethodPost, "/api/guest/lookup", merge(map[string]any{"email": "old@example.com", "reference": ref}, proof)); rec.Code != http.StatusOK {
			t.Fatalf("lookup: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodGet, "/api/guest/tickets/"+token, nil); rec.Code != http.StatusNotFound {
			t.Errorf("legacy token after a grant was issued: %d, want 404", rec.Code)
		}
	})

	eachGuestMount(t, func(c *escalated.Config) { c.GuestAccess.RejectLegacyTokens = true }, func(t *testing.T, f guestFixture) {
		token, ref := legacyTicket(t, f)
		if rec, _ := f.call(t, http.MethodGet, "/api/guest/tickets/"+token, nil); rec.Code != http.StatusNotFound {
			t.Errorf("legacy token with RejectLegacyTokens: %d, want 404", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodGet, "/widget/tickets/lookup?ref="+ref+"&token="+token, nil); rec.Code == http.StatusOK {
			t.Errorf("legacy widget lookup with RejectLegacyTokens: %d", rec.Code)
		}
	})
}
