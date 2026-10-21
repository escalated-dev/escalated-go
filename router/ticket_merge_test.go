package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

// Ticket merge as escalated-laravel's TicketMergeService and
// TicketMergeController do it, through MountChi and MountStdlib.
func TestAdminTicketMerge(t *testing.T) {
	for _, mount := range []string{"chi", "stdlib"} {
		t.Run(mount, func(t *testing.T) {
			db := testdb.Open(t)
			cfg := escalated.DefaultConfig()
			cfg.DB = db
			cfg.UIEnabled = true
			admin := true
			cfg.AdminCheck = func(*http.Request) bool { return admin }
			cfg.UserIDFunc = func(*http.Request) models.UserID { return "1" }
			esc, err := escalated.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var h http.Handler
			if mount == "chi" {
				r := chi.NewRouter()
				router.MountChi(r, esc)
				h = r
			} else {
				mux := http.NewServeMux()
				router.MountStdlib(mux, esc)
				h = mux
			}
			call := func(method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
				var buf bytes.Buffer
				if body != nil {
					_ = json.NewEncoder(&buf).Encode(body)
				}
				req := httptest.NewRequest(method, "/escalated"+path, &buf)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				out := map[string]any{}
				_ = json.Unmarshal(rec.Body.Bytes(), &out)
				return rec, out
			}

			ctx := context.Background()
			ts := esc.NewTicketService()
			source, _ := ts.Create(ctx, services.CreateTicketInput{Subject: "Duplicate printer report", Description: "D"})
			target, _ := ts.Create(ctx, services.CreateTicketInput{Subject: "Printer on fire", Description: "D"})
			agent := "User"
			uid := models.UserID("2")
			if _, err := ts.AddReply(ctx, source.ID, "first", &agent, &uid, false); err != nil {
				t.Fatal(err)
			}
			if _, err := ts.AddReply(ctx, source.ID, "second", &agent, &uid, true); err != nil {
				t.Fatal(err)
			}

			// Search offers unmerged tickets by reference or subject.
			rec, out := call(http.MethodGet, "/admin/tickets/merge-search?q=fire", nil)
			list, _ := out["tickets"].([]any)
			if rec.Code != http.StatusOK || len(list) != 1 || list[0].(map[string]any)["reference"] != target.Reference {
				t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
			}
			if rec, _ := call(http.MethodGet, "/admin/tickets/merge-search", nil); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("search without q: %d", rec.Code)
			}

			// Refusals.
			if rec, _ := call(http.MethodPost, fmt.Sprintf("/admin/tickets/%d/merge", source.ID), map[string]any{"target_reference": source.Reference}); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("merge into itself: %d", rec.Code)
			}
			if rec, _ := call(http.MethodPost, fmt.Sprintf("/admin/tickets/%d/merge", source.ID), map[string]any{"target_reference": "ESC-NOPE"}); rec.Code != http.StatusNotFound {
				t.Errorf("unknown target: %d", rec.Code)
			}
			if rec, _ := call(http.MethodPost, fmt.Sprintf("/admin/tickets/%d/merge", source.ID), map[string]any{}); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("no target: %d", rec.Code)
			}
			admin = false
			if rec, _ := call(http.MethodPost, fmt.Sprintf("/admin/tickets/%d/merge", source.ID), map[string]any{"target_reference": target.Reference}); rec.Code/100 == 2 {
				t.Errorf("non-admin merged: %d", rec.Code)
			}
			admin = true

			// The merge, addressed by the source reference.
			rec, out = call(http.MethodPost, "/admin/tickets/"+source.Reference+"/merge", map[string]any{"target_reference": target.Reference})
			if rec.Code != http.StatusOK || out["target_reference"] != target.Reference {
				t.Fatalf("merge: %d %s", rec.Code, rec.Body.String())
			}

			got, _ := esc.Store.GetTicket(ctx, source.ID)
			if got.Status != models.StatusClosed || got.MergedIntoID == nil || *got.MergedIntoID != target.ID || got.ClosedAt == nil {
				t.Fatalf("source after merge: status=%d merged_into=%v closed_at=%v", got.Status, got.MergedIntoID, got.ClosedAt)
			}
			count := func(ticketID int64, where string) int {
				var n int
				if err := db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_replies WHERE ticket_id = ? AND `+where), ticketID).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			if n := count(target.ID, "is_system = FALSE"); n != 2 {
				t.Errorf("replies moved to the target = %d, want 2", n)
			}
			var note string
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT body FROM escalated_replies WHERE ticket_id = ? AND is_system = TRUE AND is_internal = TRUE`), target.ID).Scan(&note); err != nil || note != "Ticket "+source.Reference+" was merged into this ticket." {
				t.Errorf("target note = %q (%v)", note, err)
			}
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT body FROM escalated_replies WHERE ticket_id = ? AND is_system = TRUE AND is_internal = TRUE`), source.ID).Scan(&note); err != nil || note != "This ticket was merged into "+target.Reference+"." {
				t.Errorf("source note = %q (%v)", note, err)
			}
			if n := count(source.ID, "is_system = FALSE"); n != 0 {
				t.Errorf("replies left on the source = %d", n)
			}
			var acts int
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_ticket_activities WHERE action = ? AND causer_id = ?`), models.ActionTicketMerged, "1").Scan(&acts); err != nil || acts != 2 {
				t.Errorf("merge activities = %d (%v), want 2", acts, err)
			}

			// A merged ticket is no longer offered or accepted as a target.
			if _, out := call(http.MethodGet, "/admin/tickets/merge-search?q=printer", nil); len(out["tickets"].([]any)) != 1 {
				t.Errorf("merged ticket still offered: %v", out["tickets"])
			}
			if rec, _ := call(http.MethodPost, fmt.Sprintf("/admin/tickets/%d/merge", target.ID), map[string]any{"target_reference": source.Reference}); rec.Code != http.StatusNotFound {
				t.Errorf("merge into a merged ticket: %d", rec.Code)
			}
		})
	}
}
