package router_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

// Widget chat tokens are expiring grants, as in escalated-laravel: only the
// token's SHA-256 is stored, it stops working the guest grant lifetime after
// the chat starts, and an unknown or expired token is a 404 on every chat path.

func chatTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func startTokenChat(t *testing.T, f *widgetContractFixture, h http.Handler) map[string]any {
	t.Helper()
	rec, out := widgetCall(t, h, http.MethodPost, "/chat/start", withProof(t, f.esc, "chat", map[string]any{
		"name": "Grace", "email": "grace@example.com", "message": "Hello",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	return out
}

func chatDBExec(t *testing.T, f *widgetContractFixture, query string, args ...any) {
	t.Helper()
	db := f.esc.Config.DB
	if _, err := db.Exec(sqldialect.Rebind(db, query), args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func chatTokenRow(t *testing.T, f *widgetContractFixture, sessionID any) (hash sql.NullString, expires sql.NullTime, guestToken sql.NullString) {
	t.Helper()
	db := f.esc.Config.DB
	p := f.esc.Config.TablePrefix
	err := db.QueryRow(sqldialect.Rebind(db, `SELECT s.visitor_token_hash, s.visitor_token_expires_at, t.guest_token FROM `+p+`chat_sessions s
		JOIN `+p+`tickets t ON t.id = s.ticket_id WHERE s.id = ?`), sessionID).Scan(&hash, &expires, &guestToken)
	if err != nil {
		t.Fatalf("chat row: %v", err)
	}
	return hash, expires, guestToken
}

func TestWidgetChatTokenExpiresAndOnlyItsHashIsStored(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			before := time.Now().Truncate(time.Second)
			out := startTokenChat(t, f, h)
			id, _ := out["id"].(string)
			expires, err := time.Parse(time.RFC3339, func() string { s, _ := out["expires_at"].(string); return s }())
			if err != nil {
				t.Fatalf("expires_at = %#v", out["expires_at"])
			}
			if expires.Before(before.Add(24*time.Hour)) || expires.After(time.Now().Add(24*time.Hour)) {
				t.Fatalf("expires_at = %v, want 24 hours from now", expires)
			}
			if out["guest_token"] != id {
				t.Fatalf("guest_token = %#v, want the chat token", out["guest_token"])
			}

			hash, stored, guestToken := chatTokenRow(t, f, out["session_id"])
			if hash.String != chatTokenHash(id) {
				t.Fatalf("visitor_token_hash = %q, want the token's SHA-256", hash.String)
			}
			if !stored.Valid || !stored.Time.Equal(expires) {
				t.Fatalf("visitor_token_expires_at = %v, want %v", stored, expires)
			}
			if guestToken.Valid {
				t.Fatalf("chat ticket keeps guest_token %q; a chat token must not open ticket routes", guestToken.String)
			}
			if rec, _ := widgetCall(t, h, http.MethodGet, "/chat/"+id+"/messages", nil); rec.Code != http.StatusOK {
				t.Fatalf("messages: %d", rec.Code)
			}
		})
	}
}

func TestWidgetChatTokenIsRefusedOnceExpired(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			out := startTokenChat(t, f, h)
			id, _ := out["id"].(string)
			chatDBExec(t, f, `UPDATE `+f.esc.Config.TablePrefix+`chat_sessions SET visitor_token_expires_at = ? WHERE id = ?`,
				time.Now().UTC().Add(-time.Minute), out["session_id"])

			for _, call := range []struct{ method, path string }{
				{http.MethodGet, "/messages"},
				{http.MethodPost, "/messages"},
				{http.MethodPost, "/typing"},
				{http.MethodPost, "/end"},
				{http.MethodPost, "/rate"},
			} {
				rec, _ := widgetCall(t, h, call.method, "/chat/"+id+call.path, map[string]any{"body": "x", "rating": 5})
				if rec.Code != http.StatusNotFound {
					t.Fatalf("%s %s after expiry: %d, want 404", call.method, call.path, rec.Code)
				}
			}
			ref, _ := out["ticket_reference"].(string)
			rec, _ := widgetCall(t, h, http.MethodPost, "/chat/sessions/"+ref+"/messages", map[string]any{"body": "x"}, "X-Guest-Token", id)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("legacy route after expiry: %d, want 404", rec.Code)
			}
		})
	}
}

func TestWidgetChatStartedBeforeTokensExpiredKeepsWorking(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			out := startTokenChat(t, f, h)
			id, _ := out["id"].(string)
			p := f.esc.Config.TablePrefix
			// The shape such a chat has after the upgrade: the plain token on
			// the ticket, nothing on the session.
			chatDBExec(t, f, `UPDATE `+p+`chat_sessions SET visitor_token_hash = NULL, visitor_token_expires_at = NULL WHERE id = ?`, out["session_id"])
			chatDBExec(t, f, `UPDATE `+p+`tickets SET guest_token = ? WHERE id = (SELECT ticket_id FROM `+p+`chat_sessions WHERE id = ?)`, id, out["session_id"])
			before := time.Now().Truncate(time.Second)

			if rec, _ := widgetCall(t, h, http.MethodGet, "/chat/"+id+"/messages", nil); rec.Code != http.StatusOK {
				t.Fatalf("messages: %d %s", rec.Code, rec.Body.String())
			}
			if rec, _ := widgetCall(t, h, http.MethodPost, "/chat/"+id+"/messages", map[string]any{"body": "Still here"}); rec.Code/100 != 2 {
				t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
			}

			hash, expires, guestToken := chatTokenRow(t, f, out["session_id"])
			if hash.String != chatTokenHash(id) || !expires.Valid || expires.Time.Before(before.Add(24*time.Hour)) {
				t.Fatalf("legacy chat not upgraded: hash=%v expires=%v", hash, expires)
			}
			if guestToken.Valid {
				t.Fatalf("legacy chat ticket keeps guest_token %q", guestToken.String)
			}
		})
	}
}
