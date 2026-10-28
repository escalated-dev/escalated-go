package router_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// The mobile guest API (escalated-developer-context
// domain-model/mobile-guest-api.md): verification, lookup, create, show and
// reply under {RoutePrefix}/api/mobile/guest, on the same codes, grants and
// per-IP budgets as the guest API, the guest pages and the widget.

const mobile = "/api/mobile/guest"

func mobileTicket(email string) map[string]any {
	return map[string]any{"name": "Ada", "email": email, "subject": "Billing", "description": "Charged twice"}
}

func (f guestFixture) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	db := f.esc.Config.DB
	if err := db.QueryRow(sqldialect.Rebind(db, "SELECT COUNT(*) FROM "+f.esc.Config.TablePrefix+table)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// mobileCreate files a ticket through the mobile route and returns its data.
func (f guestFixture) mobileCreate(t *testing.T, email string, extra map[string]any) map[string]any {
	t.Helper()
	proof := f.requestCode(t, mobile, email, "ticket")
	rec, out := f.call(t, http.MethodPost, mobile+"/tickets", merge(merge(mobileTicket(email), extra), proof))
	if rec.Code != http.StatusCreated {
		t.Fatalf("mobile create: %d %s", rec.Code, rec.Body.String())
	}
	data, _ := out["data"].(map[string]any)
	return data
}

func wantPrivacyHeaders(t *testing.T, what string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("%s: Cache-Control %q, want private, no-store", what, got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("%s: Referrer-Policy %q", what, got)
	}
}

func TestMobileGuestCreateWithoutProofCreatesNothing(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		rec, out := f.call(t, http.MethodPost, mobile+"/tickets", mobileTicket("ada@example.com"))
		errs, _ := out["errors"].(map[string]any)
		if rec.Code != http.StatusUnprocessableEntity || errs["verification_code"] == nil || errs["verification_id"] == nil {
			t.Fatalf("create without proof: %d %s", rec.Code, rec.Body.String())
		}
		wantPrivacyHeaders(t, "422", rec)

		// A wrong code is 422 on verification_code, and nothing is created.
		proof := f.requestCode(t, mobile, "ada@example.com", "ticket")
		wrong := merge(proof, map[string]any{"verification_code": "00000000"})
		if proof["verification_code"] == "00000000" {
			wrong["verification_code"] = "11111111"
		}
		rec, out = f.call(t, http.MethodPost, mobile+"/tickets", merge(mobileTicket("ada@example.com"), wrong))
		errs, _ = out["errors"].(map[string]any)
		if rec.Code != http.StatusUnprocessableEntity || errs["verification_code"] == nil {
			t.Fatalf("wrong code: %d %s", rec.Code, rec.Body.String())
		}
		// Another purpose's code does not create a ticket either.
		lookup := f.requestCode(t, mobile, "ada@example.com", "lookup")
		if rec, _ := f.call(t, http.MethodPost, mobile+"/tickets", merge(mobileTicket("ada@example.com"), lookup)); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("lookup code used for a ticket: %d", rec.Code)
		}
		if n := f.count(t, "tickets"); n != 0 {
			t.Errorf("tickets = %d, want 0", n)
		}
		if n := f.count(t, "contacts"); n != 0 {
			t.Errorf("contacts = %d, want 0", n)
		}

		// Field validation runs before the code is checked, so the code
		// survives an invalid form.
		rec, out = f.call(t, http.MethodPost, mobile+"/tickets", merge(merge(mobileTicket("ada@example.com"), map[string]any{"subject": "", "priority": "whenever"}), proof))
		errs, _ = out["errors"].(map[string]any)
		if rec.Code != http.StatusUnprocessableEntity || errs["subject"] == nil || errs["priority"] == nil || errs["verification_code"] != nil {
			t.Fatalf("invalid fields: %d %s", rec.Code, rec.Body.String())
		}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/tickets", merge(mobileTicket("ada@example.com"), proof)); rec.Code != http.StatusCreated {
			t.Fatalf("the code was spent by an invalid form: %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMobileGuestVerificationAndCreate(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		rec, out := f.call(t, http.MethodPost, mobile+"/verification", map[string]any{"email": " Ada@Example.com ", "purpose": "ticket"})
		if rec.Code != http.StatusAccepted || out["verification_id"] == nil || out["expires_in"] != float64(600) || out["message"] == nil {
			t.Fatalf("verification: %d %s", rec.Code, rec.Body.String())
		}
		wantPrivacyHeaders(t, "verification", rec)
		if _, leaked := out["code"]; leaked {
			t.Fatal("the code is in the response")
		}
		for _, bad := range []map[string]any{
			{"email": "not-an-email", "purpose": "ticket"},
			{"email": "ada@example.com", "purpose": "admin"},
		} {
			if rec, _ := f.call(t, http.MethodPost, mobile+"/verification", bad); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("verification %v: %d, want 422", bad, rec.Code)
			}
		}

		proof := map[string]any{"verification_id": out["verification_id"], "verification_code": f.mailer.code("ada@example.com")}
		body := merge(mobileTicket("ada@example.com"), map[string]any{
			"email": "ADA@example.com", "priority": "high",
			// Anonymous creation cannot set these.
			"status": "closed", "assigned_to": "7", "external_reference": "X-1", "metadata": map[string]any{"vip": true},
		})
		rec, out = f.call(t, http.MethodPost, mobile+"/tickets", merge(body, proof))
		if rec.Code != http.StatusCreated || out["message"] != "Ticket created." {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		wantPrivacyHeaders(t, "create", rec)
		data := out["data"].(map[string]any)
		for _, k := range []string{"id", "reference", "guest_access_token", "guest_access_expires_at", "subject", "description", "status",
			"priority", "channel", "metadata", "requester", "assignee", "department", "tags", "replies", "activities", "sla",
			"is_following", "followers_count", "resolved_at", "closed_at", "created_at", "updated_at"} {
			if _, ok := data[k]; !ok {
				t.Errorf("mobile ticket lacks %q", k)
			}
		}
		if len(data) != 23 {
			t.Errorf("mobile ticket has %d keys, want 23: %v", len(data), data)
		}
		grant, _ := data["guest_access_token"].(string)
		if len(grant) < 40 || data["guest_access_expires_at"] == nil {
			t.Fatalf("grant = %v", data)
		}
		if st := data["status"].(map[string]any); st["value"] != "open" || st["label"] != "Open" {
			t.Errorf("status %v", st)
		}
		if p := data["priority"].(map[string]any); p["value"] != "high" || p["label"] != "High" {
			t.Errorf("priority %v", p)
		}
		if data["channel"] != "web" || data["assignee"] != nil || data["department"] != nil || data["is_following"] != false || data["followers_count"] != float64(0) {
			t.Errorf("data %v", data)
		}
		if m, ok := data["metadata"].(map[string]any); !ok || len(m) != 0 {
			t.Errorf("metadata %v", data["metadata"])
		}
		if req := data["requester"].(map[string]any); req["name"] != "Ada" || req["email"] != "ada@example.com" {
			t.Errorf("requester %v", req)
		}
		for _, k := range []string{"tags", "replies", "activities"} {
			if l, ok := data[k].([]any); !ok || len(l) != 0 {
				t.Errorf("%s = %v, want []", k, data[k])
			}
		}
		sla := data["sla"].(map[string]any)
		for _, k := range []string{"first_response_due_at", "first_response_at", "first_response_breached", "resolution_due_at", "resolution_breached"} {
			if _, ok := sla[k]; !ok {
				t.Errorf("sla lacks %q", k)
			}
		}

		tk, err := f.esc.Store.GetTicketByReference(context.Background(), data["reference"].(string))
		if err != nil || tk == nil {
			t.Fatal(err)
		}
		if tk.Status != models.StatusOpen || tk.AssignedTo != nil || len(tk.Metadata) > 0 || tk.Priority != models.PriorityHigh ||
			tk.GuestEmail == nil || *tk.GuestEmail != "ada@example.com" || tk.ContactID == nil {
			t.Errorf("stored ticket %+v", tk)
		}

		// The code is spent.
		if rec, out := f.call(t, http.MethodPost, mobile+"/tickets", merge(body, proof)); rec.Code != http.StatusUnprocessableEntity || out["errors"].(map[string]any)["verification_code"] == nil {
			t.Errorf("code reused: %d", rec.Code)
		}

		// The default priority is medium.
		if p := f.mobileCreate(t, "bo@example.com", nil)["priority"].(map[string]any); p["value"] != "medium" {
			t.Errorf("default priority %v", p)
		}
	})
}

func TestMobileGuestCreateRefusesAttachments(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		proof := f.requestCode(t, mobile, "ada@example.com", "ticket")
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for k, v := range merge(mobileTicket("ada@example.com"), proof) {
			_ = mw.WriteField(k, v.(string))
		}
		fw, _ := mw.CreateFormFile("attachments[]", "a.txt")
		_, _ = fw.Write([]byte("hello"))
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/escalated"+mobile+"/tickets", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.RemoteAddr = "203.0.113.50:5000"
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "attachments") {
			t.Fatalf("multipart with a file: %d %s", rec.Code, rec.Body.String())
		}
		if n := f.count(t, "tickets"); n != 0 {
			t.Errorf("tickets = %d", n)
		}

		// The same form without a file is accepted.
		buf.Reset()
		mw = multipart.NewWriter(&buf)
		for k, v := range merge(mobileTicket("ada@example.com"), proof) {
			_ = mw.WriteField(k, v.(string))
		}
		_ = mw.Close()
		req = httptest.NewRequest(http.MethodPost, "/escalated"+mobile+"/tickets", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.RemoteAddr = "203.0.113.50:5000"
		rec = httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("multipart without files: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// A grant from any surface opens the others.
func TestMobileGuestGrantsWorkAcrossSurfaces(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		data := f.mobileCreate(t, "ada@example.com", nil)
		grant, ref := data["guest_access_token"].(string), data["reference"].(string)

		rec, out := f.call(t, http.MethodGet, mobile+"/tickets/"+grant, nil)
		if rec.Code != http.StatusOK || out["data"].(map[string]any)["guest_access_token"] != grant || out["data"].(map[string]any)["reference"] != ref {
			t.Fatalf("mobile show: %d %s", rec.Code, rec.Body.String())
		}
		wantPrivacyHeaders(t, "show", rec)
		if rec, _ := f.call(t, http.MethodGet, "/api/guest/tickets/"+grant, nil); rec.Code != http.StatusOK {
			t.Errorf("guest API with a mobile grant: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodGet, "/widget/tickets/"+ref, nil, "Authorization", "Bearer "+grant); rec.Code != http.StatusOK {
			t.Errorf("widget with a mobile grant: %d", rec.Code)
		}

		// A widget-created ticket's grant opens the mobile routes.
		proof := f.requestCode(t, "/widget", "bo@example.com", "ticket")
		rec, out = f.call(t, http.MethodPost, "/widget/tickets", merge(mobileTicket("bo@example.com"), proof))
		if rec.Code != http.StatusCreated {
			t.Fatalf("widget create: %d", rec.Code)
		}
		widgetGrant, widgetRef := out["guest_access_token"].(string), out["reference"]
		if rec, out := f.call(t, http.MethodGet, mobile+"/tickets/"+widgetGrant, nil); rec.Code != http.StatusOK || out["data"].(map[string]any)["reference"] != widgetRef {
			t.Errorf("mobile with a widget grant: %d", rec.Code)
		}

		// Mobile lookup issues a fresh grant and rotates the old one.
		lookup := f.requestCode(t, mobile, "ada@example.com", "lookup")
		rec, out = f.call(t, http.MethodPost, mobile+"/lookup", merge(map[string]any{"email": "ada@example.com", "reference": ref}, lookup))
		matches, _ := out["data"].([]any)
		if rec.Code != http.StatusOK || len(matches) != 1 {
			t.Fatalf("lookup: %d %s", rec.Code, rec.Body.String())
		}
		wantPrivacyHeaders(t, "lookup", rec)
		fresh := matches[0].(map[string]any)["guest_access_token"].(string)
		if rec, _ := f.call(t, http.MethodGet, mobile+"/tickets/"+grant, nil); rec.Code != http.StatusNotFound {
			t.Errorf("rotated grant: %d, want 404", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodGet, mobile+"/tickets/"+fresh, nil); rec.Code != http.StatusOK {
			t.Errorf("fresh grant: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/lookup", merge(map[string]any{"email": "ada@example.com", "reference": ref}, lookup)); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("lookup code reused: %d", rec.Code)
		}
		// A grant from the guest API lookup opens the mobile routes too.
		lookup = f.requestCode(t, "/api/guest", "ada@example.com", "lookup")
		_, out = f.call(t, http.MethodPost, "/api/guest/lookup", merge(map[string]any{"email": "ada@example.com", "reference": ref}, lookup))
		apiGrant := out["data"].([]any)[0].(map[string]any)["guest_access_token"].(string)
		if rec, _ := f.call(t, http.MethodGet, mobile+"/tickets/"+apiGrant, nil); rec.Code != http.StatusOK {
			t.Errorf("mobile with a guest API grant: %d", rec.Code)
		}
	})
}

func TestMobileGuestGrantOpensTheGuestPage(t *testing.T) {
	eachGuestMount(t, func(c *escalated.Config) { c.UIEnabled = true }, func(t *testing.T, f guestFixture) {
		grant := f.mobileCreate(t, "ada@example.com", nil)["guest_access_token"].(string)
		rec, out := f.call(t, http.MethodGet, "/guest/"+grant, nil, "X-Inertia", "true")
		if rec.Code != http.StatusOK || out["component"] != "Escalated/Guest/Show" {
			t.Fatalf("guest page with a mobile grant: %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestMobileGuestShowAndReplies(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		data := f.mobileCreate(t, "ada@example.com", nil)
		grant := data["guest_access_token"].(string)
		id := int64(data["id"].(float64))
		ctx := context.Background()
		ts := f.esc.NewTicketService()

		rec, out := f.call(t, http.MethodPost, mobile+"/tickets/"+grant+"/replies", map[string]any{"email": " ADA@Example.com ", "body": "First"})
		if rec.Code != http.StatusCreated || out["message"] != "Reply sent." {
			t.Fatalf("reply: %d %s", rec.Code, rec.Body.String())
		}
		wantPrivacyHeaders(t, "reply", rec)
		time.Sleep(1100 * time.Millisecond) // distinct created_at seconds on every driver
		agentType, agentID := "user", models.UserID("7")
		if _, err := ts.AddReply(ctx, id, "Staff answer", &agentType, &agentID, false); err != nil {
			t.Fatal(err)
		}
		if _, err := ts.AddReply(ctx, id, "Secret note", &agentType, &agentID, true); err != nil {
			t.Fatal(err)
		}

		rec, out = f.call(t, http.MethodPost, mobile+"/tickets/"+grant+"/replies", map[string]any{"email": "eve@example.com", "body": "x"})
		if rec.Code != http.StatusForbidden || out["message"] != "The provided email does not match this ticket." {
			t.Errorf("mismatched email: %d %s", rec.Code, rec.Body.String())
		}
		if rec, out := f.call(t, http.MethodPost, mobile+"/tickets/"+grant+"/replies", map[string]any{"email": "ada@example.com", "body": ""}); rec.Code != http.StatusUnprocessableEntity || out["errors"].(map[string]any)["body"] == nil {
			t.Errorf("empty body: %d", rec.Code)
		}

		rec, out = f.call(t, http.MethodGet, mobile+"/tickets/"+grant, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("show: %d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "Secret note") {
			t.Fatal("an internal note reached the guest")
		}
		replies := out["data"].(map[string]any)["replies"].([]any)
		if len(replies) != 2 {
			t.Fatalf("replies %v", replies)
		}
		staff, guest := replies[0].(map[string]any), replies[1].(map[string]any)
		if staff["body"] != "Staff answer" || guest["body"] != "First" {
			t.Fatalf("replies not newest first: %v", replies)
		}
		if a := staff["author"].(map[string]any); a["id"] != float64(0) || a["name"] != "Support" || a["email"] != "" {
			t.Errorf("staff author %v", a)
		}
		if a := guest["author"].(map[string]any); a["id"] != float64(0) || a["name"] != "Ada" || a["email"] != "ada@example.com" {
			t.Errorf("guest author %v", a)
		}
		for _, k := range []string{"id", "body", "is_internal_note", "is_pinned", "author", "attachments", "created_at"} {
			if _, ok := guest[k]; !ok {
				t.Errorf("reply lacks %q", k)
			}
		}
		if guest["is_internal_note"] != false {
			t.Errorf("is_internal_note %v", guest["is_internal_note"])
		}

		// A closed ticket takes no replies.
		if err := ts.ChangeStatus(ctx, id, models.StatusClosed, nil); err != nil {
			t.Fatal(err)
		}
		rec, out = f.call(t, http.MethodPost, mobile+"/tickets/"+grant+"/replies", map[string]any{"email": "ada@example.com", "body": "Again"})
		if rec.Code != http.StatusUnprocessableEntity || out["message"] != "This ticket is closed." {
			t.Errorf("closed ticket: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// Every credential that is not a live ticket grant is a 404, never 401/403.
func TestMobileGuestBadGrantsAre404(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		data := f.mobileCreate(t, "ada@example.com", nil)
		grant := data["guest_access_token"].(string)

		// A chat token.
		chat := map[string]any{"name": "Ada", "email": "ada@example.com", "message": "Hi"}
		rec, out := f.call(t, http.MethodPost, "/widget/chat/start", merge(chat, f.requestCode(t, "/widget", "ada@example.com", "chat")))
		if rec.Code != http.StatusCreated {
			t.Fatalf("chat start: %d %s", rec.Code, rec.Body.String())
		}
		chatToken := out["id"].(string)

		// A legacy permanent token.
		email := "old@example.com"
		legacy, err := f.esc.NewTicketService().Create(context.Background(), services.CreateTicketInput{Subject: "Old", Description: "D", GuestEmail: &email})
		if err != nil || legacy.GuestToken == nil {
			t.Fatal(err)
		}

		for name, token := range map[string]string{
			"chat":      chatToken,
			"legacy":    *legacy.GuestToken,
			"malformed": "not-a-grant",
			"forged":    grant[:len(grant)-2] + "AA",
			"truncated": grant[:30],
			"over-long": strings.Repeat("A", 5000),
		} {
			for _, c := range []struct{ method, path string }{
				{http.MethodGet, mobile + "/tickets/" + token},
				{http.MethodPost, mobile + "/tickets/" + token + "/replies"},
			} {
				rec, _ := f.call(t, c.method, c.path, map[string]any{"email": "ada@example.com", "body": "x"})
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s grant, %s: %d, want 404", name, c.method, rec.Code)
				}
				wantPrivacyHeaders(t, name+" 404", rec)
			}
		}

		// An expired grant.
		db := f.esc.Config.DB
		if _, err := db.Exec(sqldialect.Rebind(db, "UPDATE "+f.esc.Config.TablePrefix+"tickets SET guest_access_expires_at = ? WHERE id = ?"),
			time.Now().Add(-time.Minute).UTC(), int64(data["id"].(float64))); err != nil {
			t.Fatal(err)
		}
		if rec, _ := f.call(t, http.MethodGet, mobile+"/tickets/"+grant, nil); rec.Code != http.StatusNotFound {
			t.Errorf("expired grant: %d, want 404", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/tickets/"+grant+"/replies", map[string]any{"email": "ada@example.com", "body": "x"}); rec.Code != http.StatusNotFound {
			t.Errorf("expired grant reply: %d, want 404", rec.Code)
		}
	})
}

func TestMobileGuestChatCodeNeedsChatEnabled(t *testing.T) {
	eachGuestMount(t, nil, func(t *testing.T, f guestFixture) {
		if err := f.esc.Store.SetSetting(context.Background(), "chat_enabled", "0"); err != nil {
			t.Fatal(err)
		}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/verification", map[string]any{"email": "ada@example.com", "purpose": "chat"}); rec.Code != http.StatusForbidden {
			t.Errorf("chat code with chat off: %d, want 403", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/verification", map[string]any{"email": "ada@example.com", "purpose": "ticket"}); rec.Code != http.StatusAccepted {
			t.Errorf("ticket code with chat off: %d", rec.Code)
		}
	})
}

// The mobile routes spend the same per-IP guest budgets as the guest API and
// the widget: submissions (verification, create), replies (lookup, replies)
// and requests (every route).
func TestMobileGuestRateLimits(t *testing.T) {
	limited := func(tickets, replies, requests int) func(*escalated.Config) {
		return func(c *escalated.Config) {
			c.GuestRateLimit.Disabled = false
			c.GuestRateLimit.TicketsPerMinute = tickets
			c.GuestRateLimit.RepliesPerMinute = replies
			c.GuestRateLimit.RequestsPerMinute = requests
		}
	}
	verify := func(t *testing.T, f guestFixture, surface string) *httptest.ResponseRecorder {
		rec, _ := f.call(t, http.MethodPost, surface+"/verification", map[string]any{"email": "ada@example.com", "purpose": "ticket"}, "RemoteAddr", "198.51.100.9:1")
		return rec
	}

	eachGuestMount(t, limited(1, 100, 100), func(t *testing.T, f guestFixture) {
		if rec := verify(t, f, mobile); rec.Code != http.StatusAccepted {
			t.Fatalf("first verification: %d", rec.Code)
		}
		rec := verify(t, f, mobile)
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Fatalf("second verification: %d, want 429 with Retry-After", rec.Code)
		}
		wantPrivacyHeaders(t, "429", rec)
		rec, _ = f.call(t, http.MethodPost, mobile+"/tickets", mobileTicket("ada@example.com"), "RemoteAddr", "198.51.100.9:1")
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("create after the submission budget: %d, want 429", rec.Code)
		}
	})

	// One budget per IP across the guest API, the widget and mobile.
	for _, surface := range []string{"/api/guest", "/widget"} {
		eachGuestMount(t, limited(1, 100, 100), func(t *testing.T, f guestFixture) {
			if rec := verify(t, f, surface); rec.Code != http.StatusAccepted {
				t.Fatalf("%s verification: %d", surface, rec.Code)
			}
			if rec := verify(t, f, mobile); rec.Code != http.StatusTooManyRequests {
				t.Errorf("mobile after %s spent the budget: %d, want 429", surface, rec.Code)
			}
		})
	}

	eachGuestMount(t, limited(100, 1, 100), func(t *testing.T, f guestFixture) {
		grant := f.mobileCreate(t, "ada@example.com", nil)["guest_access_token"].(string)
		reply := map[string]any{"email": "ada@example.com", "body": "x"}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/tickets/"+grant+"/replies", reply, "RemoteAddr", "198.51.100.9:1"); rec.Code != http.StatusCreated {
			t.Fatalf("first reply: %d", rec.Code)
		}
		if rec, _ := f.call(t, http.MethodPost, mobile+"/lookup", map[string]any{}, "RemoteAddr", "198.51.100.9:1"); rec.Code != http.StatusTooManyRequests {
			t.Errorf("lookup after the reply budget: %d, want 429", rec.Code)
		}
	})

	eachGuestMount(t, limited(100, 100, 2), func(t *testing.T, f guestFixture) {
		for i, want := range []int{http.StatusNotFound, http.StatusNotFound, http.StatusTooManyRequests} {
			if rec, _ := f.call(t, http.MethodGet, mobile+"/tickets/nope", nil, "RemoteAddr", "198.51.100.9:1"); rec.Code != want {
				t.Errorf("request %d: %d, want %d", i+1, rec.Code, want)
			}
		}
	})
}
