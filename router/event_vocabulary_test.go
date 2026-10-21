package router_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services"
)

func newVocabularyEsc(t *testing.T) (*escalated.Escalated, *sql.DB) {
	t.Helper()
	db := testdb.Open(t)
	cfg := escalated.DefaultConfig()
	cfg.DB = db
	cfg.UIEnabled = true
	cfg.AdminCheck = func(_ *http.Request) bool { return true }
	cfg.AgentCheck = func(_ *http.Request) bool { return true }
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return esc, db
}

func vocabularyServe(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func eachMount(t *testing.T, fn func(t *testing.T, h http.Handler, esc *escalated.Escalated, db *sql.DB)) {
	t.Run("chi", func(t *testing.T) {
		esc, db := newVocabularyEsc(t)
		r := chi.NewRouter()
		router.MountChi(r, esc)
		fn(t, r, esc, db)
	})
	t.Run("stdlib", func(t *testing.T) {
		esc, db := newVocabularyEsc(t)
		mux := http.NewServeMux()
		router.MountStdlib(mux, esc)
		fn(t, mux, esc, db)
	})
}

// The admin forms store a legacy event name as the canonical name and show a
// stored legacy name as its canonical name.
func TestAdminFormsStoreAndShowCanonicalEventNames(t *testing.T) {
	eachMount(t, func(t *testing.T, h http.Handler, _ *escalated.Escalated, db *sql.DB) {
		rec := vocabularyServe(t, h, http.MethodPost, "/escalated/admin/workflows",
			`{"name":"Legacy","trigger_event":"ticket.tagged","conditions":{"all":[]},"actions":[]}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create workflow: %d %s", rec.Code, rec.Body.String())
		}
		var stored string
		if err := db.QueryRow(`SELECT trigger_event FROM escalated_workflows`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != "ticket.tag_added" {
			t.Errorf("stored trigger_event = %q, want ticket.tag_added", stored)
		}
		if _, err := db.Exec(`UPDATE escalated_workflows SET trigger_event = 'ticket.replied'`); err != nil {
			t.Fatal(err)
		}
		rec = vocabularyServe(t, h, http.MethodGet, "/escalated/admin/workflows", "")
		var list struct {
			Workflows []struct {
				TriggerEvent string `json:"trigger_event"`
				Trigger      string `json:"trigger"`
			} `json:"workflows"`
			AvailableEvents []string `json:"available_events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Workflows) != 1 || list.Workflows[0].TriggerEvent != "reply.created" || list.Workflows[0].Trigger != "reply.created" {
			t.Errorf("listed workflow = %+v, want reply.created", list.Workflows)
		}
		for _, e := range list.AvailableEvents {
			if e == "ticket.tagged" || e == "reply.agent_reply" {
				t.Errorf("builder offers %q", e)
			}
		}

		rec = vocabularyServe(t, h, http.MethodPost, "/escalated/admin/webhooks",
			`{"url":"https://93.184.216.34/hook","events":["ticket.tagged","internal_note.added","reply.created"]}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create webhook: %d %s", rec.Code, rec.Body.String())
		}
		var events string
		if err := db.QueryRow(`SELECT events FROM escalated_webhooks`).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events != `["ticket.tag_added","note.created","reply.created"]` {
			t.Errorf("stored events = %s", events)
		}
	})
}

// A macro's add_tag, applied over HTTP, announces ticket.tag_added, and a
// workflow saved under the legacy ticket.tagged name runs on it.
func TestMacroAddTagFiresLegacyTaggedWorkflow(t *testing.T) {
	eachMount(t, func(t *testing.T, h http.Handler, esc *escalated.Escalated, db *sql.DB) {
		ctx := context.Background()
		tk, err := esc.NewTicketService().Create(ctx, services.CreateTicketInput{Subject: "S", Description: "D"})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if _, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_tags (name, slug, created_at, updated_at) VALUES (?, ?, ?, ?)`, "vip", "vip", now, now); err != nil {
			t.Fatal(err)
		}
		res, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_macros (name, actions) VALUES (?, ?)`, "Tag VIP", `[{"type":"add_tag","value":"vip"}]`)
		if err != nil {
			t.Fatal(err)
		}
		macroID, _ := res.LastInsertId()
		wfRes, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"legacy", "ticket.tagged", `{}`, `[]`, true, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		wfID, _ := wfRes.LastInsertId()

		rec := vocabularyServe(t, h, http.MethodPost,
			"/escalated/agent/tickets/"+strconv.FormatInt(tk.ID, 10)+"/macros/"+strconv.FormatInt(macroID, 10)+"/apply", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("apply macro: %d %s", rec.Code, rec.Body.String())
		}

		var n int
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_workflow_logs WHERE workflow_id = ? AND trigger_event = ?`), wfID, "ticket.tag_added").Scan(&n)
			if n > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if n != 1 {
			t.Errorf("legacy ticket.tagged workflow ran %d time(s) on the macro's tag, want 1", n)
		}
	})
}

// CheckSLA is the entry point a host scheduler calls: it marks a breach and
// runs sla.breached workflows.
func TestCheckSLAFiresBreachedWorkflows(t *testing.T) {
	esc, db := newVocabularyEsc(t)
	ctx := context.Background()
	tk, err := esc.NewTicketService().Create(ctx, services.CreateTicketInput{Subject: "S", Description: "D"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := esc.Store.GetTicket(ctx, tk.ID)
	past := time.Now().Add(-time.Hour)
	got.SLAResolutionDueAt = &past
	if err := esc.Store.UpdateTicket(ctx, got); err != nil {
		t.Fatal(err)
	}
	wfRes, err := sqldialect.ExecInsert(db, `INSERT INTO escalated_workflows (name, trigger_event, conditions, actions, is_active, stop_on_match, position) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"breach", "sla.breached", `{}`, `[]`, true, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	wfID, _ := wfRes.LastInsertId()

	breaches, _, err := esc.CheckSLA(ctx, 0)
	if err != nil || breaches != 1 {
		t.Fatalf("CheckSLA = %d, %v; want 1 breach", breaches, err)
	}
	var n int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && n == 0 {
		_ = db.QueryRow(sqldialect.Rebind(db, `SELECT COUNT(*) FROM escalated_workflow_logs WHERE workflow_id = ?`), wfID).Scan(&n)
		time.Sleep(10 * time.Millisecond)
	}
	if n != 1 {
		t.Errorf("sla.breached workflow ran %d time(s), want 1", n)
	}
}
