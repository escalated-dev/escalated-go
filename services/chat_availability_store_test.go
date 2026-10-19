package services_test

import (
	"context"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

func availabilityRule(t *testing.T, s store.Store, dept *int64, maxChats int, agents ...models.UserID) {
	t.Helper()
	r := &models.ChatRoutingRule{
		Name:               "rule",
		Strategy:           models.StrategyRoundRobin,
		DepartmentID:       dept,
		AgentIDs:           agents,
		MaxConcurrentChats: maxChats,
		IsActive:           true,
	}
	if err := s.CreateChatRoutingRule(context.Background(), r); err != nil {
		t.Fatalf("creating rule: %v", err)
	}
}

func activeChat(t *testing.T, s store.Store, agent models.UserID) {
	t.Helper()
	ctx := context.Background()
	tk := &models.Ticket{Subject: "chat", Description: "hi", Status: models.StatusLive, Priority: models.PriorityMedium, TicketType: "question"}
	if err := s.CreateTicket(ctx, tk); err != nil {
		t.Fatalf("creating ticket: %v", err)
	}
	a := agent
	if err := s.CreateChatSession(ctx, &models.ChatSession{TicketID: tk.ID, Status: models.ChatStatusActive, AgentID: &a}); err != nil {
		t.Fatalf("creating chat session: %v", err)
	}
}

func available(t *testing.T, svc *services.ChatAvailabilityService, dept *int64) bool {
	t.Helper()
	st, err := svc.GetStatusForDepartment(context.Background(), dept)
	if err != nil {
		t.Fatalf("GetStatusForDepartment: %v", err)
	}
	return st.Available
}

// With an agent on an active routing rule and no chats, chat is available. On
// main this was false: the stores never loaded the rule's agents.
func TestChatAvailability_AgentOnRuleIsAvailable(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	availabilityRule(t, s, nil, 2, "42")

	ok, err := services.NewChatAvailabilityService(s).IsAvailable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("IsAvailable = false with an agent free to chat, want true")
	}
}

func TestChatAvailability_NoAgentsIsUnavailable(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	svc := services.NewChatAvailabilityService(s)
	if available(t, svc, nil) {
		t.Fatal("available with no routing rules")
	}
	availabilityRule(t, s, nil, 2) // a rule naming nobody
	if available(t, svc, nil) {
		t.Fatal("available with a rule that names no agents")
	}
}

func TestChatAvailability_AgentsAtCapacityAreUnavailable(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	availabilityRule(t, s, nil, 1, "42")
	svc := services.NewChatAvailabilityService(s)
	activeChat(t, s, "42")
	if available(t, svc, nil) {
		t.Fatal("available although the only agent is at capacity")
	}
}

// A rule with no max_concurrent_chats uses the reference default of 5 rather
// than refusing every chat.
func TestChatAvailability_ZeroMaxUsesDefault(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	availabilityRule(t, s, nil, 0, "42")
	if !available(t, services.NewChatAvailabilityService(s), nil) {
		t.Fatal("unavailable with max_concurrent_chats 0, want the default of 5")
	}
}

func TestChatAvailability_ScopedToDepartment(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	sales, support := int64(1), int64(2)
	availabilityRule(t, s, &sales, 2, "42")
	svc := services.NewChatAvailabilityService(s)

	if !available(t, svc, &sales) {
		t.Error("department with a free agent reported unavailable")
	}
	if available(t, svc, &support) {
		t.Error("department with no rule (and no global rule) reported available")
	}

	availabilityRule(t, s, nil, 2, "7")
	if !available(t, svc, &support) {
		t.Error("department should fall back to the global rule's agents")
	}
}

// Hosts that track presence say which agents are online; an offline agent does
// not make chat available.
func TestChatAvailability_AgentOnlineHook(t *testing.T) {
	s := testdb.Store(t, testdb.Open(t))
	availabilityRule(t, s, nil, 2, "42", "7")
	svc := services.NewChatAvailabilityService(s)
	online := map[models.UserID]bool{}
	svc.AgentOnline = func(_ context.Context, id models.UserID) (bool, error) { return online[id], nil }

	if available(t, svc, nil) {
		t.Fatal("available with every agent offline")
	}
	online["7"] = true
	if !available(t, svc, nil) {
		t.Fatal("unavailable with agent 7 online")
	}
}

// chat_enabled set to false turns chat off, as in the reference; unset keeps
// the existing behaviour.
func TestChatAvailability_ChatDisabledSetting(t *testing.T) {
	ctx := context.Background()
	s := testdb.Store(t, testdb.Open(t))
	availabilityRule(t, s, nil, 2, "42")
	svc := services.NewChatAvailabilityService(s)

	if err := s.SetSetting(ctx, services.ChatEnabledSetting, "0"); err != nil {
		t.Fatal(err)
	}
	if available(t, svc, nil) {
		t.Fatal("available with chat_enabled=0")
	}
	if err := s.SetSetting(ctx, services.ChatEnabledSetting, "1"); err != nil {
		t.Fatal(err)
	}
	if !available(t, svc, nil) {
		t.Fatal("unavailable with chat_enabled=1")
	}
}
