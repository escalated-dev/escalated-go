package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
)

// is_active was declared INTEGER on escalated_escalation_rules while every
// other is_active column is BOOLEAN, and the handler binds a Go bool. SQLite
// stores either; PostgreSQL refused the bool, so on the default database an
// admin could neither create a rule nor switch one on or off.
func TestEscalationRuleCreateToggleAndRun(t *testing.T) {
	db := testdb.Open(t)
	h := NewEscalationHandler(db, nil)

	do := func(fn http.HandlerFunc, method, path, id, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if id != "" {
			req.SetPathValue("id", id)
		}
		rec := httptest.NewRecorder()
		fn(rec, req)
		return rec
	}

	rec := do(h.Create, http.MethodPost, "/admin/escalation-rules", "", `{"name":"x"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/escalation-rules: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("create response %q: id missing (%v)", rec.Body.String(), err)
	}
	id := strconv.FormatInt(created.ID, 10)

	isActive := func() bool {
		t.Helper()
		rec := do(h.List, http.MethodGet, "/admin/escalation-rules", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /admin/escalation-rules: got %d, want 200: %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Rules []models.EscalationRule `json:"rules"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		for _, r := range out.Rules {
			if r.ID == created.ID {
				return r.IsActive
			}
		}
		t.Fatalf("rule %d missing from list: %s", created.ID, rec.Body.String())
		return false
	}

	if !isActive() {
		t.Fatalf("new rule is_active = false, want true by default")
	}

	if rec := do(h.Update, http.MethodPatch, "/admin/escalation-rules/"+id, id, `{"is_active":false}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH is_active=false: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if isActive() {
		t.Fatalf("is_active = true after switching the rule off")
	}

	if rec := do(h.Update, http.MethodPatch, "/admin/escalation-rules/"+id, id, `{"is_active":true}`); rec.Code != http.StatusOK {
		t.Fatalf("PATCH is_active=true: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !isActive() {
		t.Fatalf("is_active = false after switching the rule back on")
	}

	// The evaluator filters on is_active, so it has to agree with the column type.
	if rec := do(h.Run, http.MethodPost, "/admin/escalation-rules/run", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/escalation-rules/run: got %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if rec := do(h.Create, http.MethodPost, "/admin/escalation-rules", "", `{"name":"off","is_active":false}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST inactive rule: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
}
