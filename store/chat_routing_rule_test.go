package store_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

func createRule(t *testing.T, s store.Store, name string, dept *int64, agents ...models.UserID) *models.ChatRoutingRule {
	t.Helper()
	r := &models.ChatRoutingRule{
		Name:               name,
		Strategy:           models.StrategyRoundRobin,
		DepartmentID:       dept,
		AgentIDs:           agents,
		MaxConcurrentChats: 3,
		IsActive:           true,
	}
	if err := s.CreateChatRoutingRule(context.Background(), r); err != nil {
		t.Fatalf("creating rule %q: %v", name, err)
	}
	return r
}

// The routing rules' agents were never read back (and SQLite never stored
// them), so every rule a store returned had no agents: routing never assigned
// anyone and widget chat availability was always false.
func TestChatRoutingRuleAgentIDsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))

	r := createRule(t, s, "Default", nil, "42", "7", "a1b2-uuid")
	want := []models.UserID{"42", "7", "a1b2-uuid"}

	got, err := s.GetChatRoutingRule(ctx, r.ID)
	if err != nil || got == nil {
		t.Fatalf("GetChatRoutingRule: %v %v", got, err)
	}
	if !reflect.DeepEqual(got.AgentIDs, want) {
		t.Errorf("GetChatRoutingRule AgentIDs = %v, want %v", got.AgentIDs, want)
	}

	list, err := s.ListActiveChatRoutingRules(ctx, nil)
	if err != nil {
		t.Fatalf("ListActiveChatRoutingRules: %v", err)
	}
	if len(list) != 1 || !reflect.DeepEqual(list[0].AgentIDs, want) {
		t.Fatalf("ListActiveChatRoutingRules = %+v, want one rule with agents %v", list, want)
	}

	got.AgentIDs = []models.UserID{"99"}
	if err := s.UpdateChatRoutingRule(ctx, got); err != nil {
		t.Fatalf("UpdateChatRoutingRule: %v", err)
	}
	again, _ := s.GetChatRoutingRule(ctx, r.ID)
	if !reflect.DeepEqual(again.AgentIDs, []models.UserID{"99"}) {
		t.Errorf("after update AgentIDs = %v, want [99]", again.AgentIDs)
	}

	empty := createRule(t, s, "Nobody", nil)
	e, _ := s.GetChatRoutingRule(ctx, empty.ID)
	if len(e.AgentIDs) != 0 {
		t.Errorf("rule with no agents read back %v", e.AgentIDs)
	}
}

// ListActiveChatRoutingRules ignored its department argument. A department
// gets its own rules plus the global (no-department) ones, never another
// department's.
func TestListActiveChatRoutingRulesScopesToDepartment(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))

	one, two := int64(1), int64(2)
	createRule(t, s, "Global", nil, "1")
	createRule(t, s, "Sales", &one, "2")
	createRule(t, s, "Support", &two, "3")
	inactive := createRule(t, s, "Off", &one, "4")
	inactive.IsActive = false
	if err := s.UpdateChatRoutingRule(ctx, inactive); err != nil {
		t.Fatal(err)
	}

	names := func(dept *int64) []string {
		rules, err := s.ListActiveChatRoutingRules(ctx, dept)
		if err != nil {
			t.Fatalf("ListActiveChatRoutingRules: %v", err)
		}
		var out []string
		for _, r := range rules {
			out = append(out, r.Name)
		}
		sort.Strings(out)
		return out
	}

	if got := names(&one); !reflect.DeepEqual(got, []string{"Global", "Sales"}) {
		t.Errorf("department 1 rules = %v, want [Global Sales]", got)
	}
	if got := names(nil); !reflect.DeepEqual(got, []string{"Global", "Sales", "Support"}) {
		t.Errorf("all active rules = %v, want [Global Sales Support]", got)
	}

	// Department-specific rules come before the global fallback.
	rules, _ := s.ListActiveChatRoutingRules(ctx, &two)
	if len(rules) != 2 || rules[0].Name != "Support" {
		t.Errorf("department 2 rules not department-first: %+v", rules)
	}
}
