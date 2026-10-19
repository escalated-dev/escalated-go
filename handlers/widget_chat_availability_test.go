package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// GET /widget/chat/availability reported false whenever the routing rules had
// agents, because the stores never loaded them, so the shared widget never
// showed its Chat tab. It also ignored department_id.
func TestWidgetChatAvailability_ReportsOnlineAgent(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	cfg := DefaultWidgetConfig()
	cfg.Enabled = true
	h := NewWidgetChatHandler(cfg, s, nil, services.NewChatAvailabilityService(s))

	get := func(query string) bool {
		t.Helper()
		rec := httptest.NewRecorder()
		h.Availability(rec, httptest.NewRequest(http.MethodGet, "/widget/chat/availability"+query, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Available bool `json:"available"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Available
	}

	if get("") {
		t.Fatal("available with no agents")
	}

	sales := int64(1)
	if err := s.CreateChatRoutingRule(context.Background(), &models.ChatRoutingRule{
		Name: "Sales", Strategy: models.StrategyRoundRobin, DepartmentID: &sales,
		AgentIDs: []models.UserID{"42"}, MaxConcurrentChats: 3, IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}

	if !get("") {
		t.Error("unavailable with agent 42 free")
	}
	if !get("?department_id=1") {
		t.Error("department 1 unavailable with its agent free")
	}
	if get("?department_id=2") {
		t.Error("department 2 has no agents but reported available")
	}
	if get("?departmentId=2") {
		t.Error("legacy departmentId not honoured")
	}
}
