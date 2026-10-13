package services

import (
	"context"
	"sync"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// ChatRoutingService handles routing incoming chat sessions to available agents.
type ChatRoutingService struct {
	store         store.Store
	mu            sync.Mutex
	roundRobinIdx map[int64]int
}

// NewChatRoutingService creates a new ChatRoutingService.
func NewChatRoutingService(s store.Store) *ChatRoutingService {
	return &ChatRoutingService{
		store:         s,
		roundRobinIdx: make(map[int64]int),
	}
}

// FindAvailableAgent evaluates active routing rules and returns the first
// agent ID that passes concurrent-chat limits, or nil if none available.
func (rs *ChatRoutingService) FindAvailableAgent(ctx context.Context, departmentID *int64) (*models.UserID, error) {
	rules, err := rs.store.ListActiveChatRoutingRules(ctx, departmentID)
	if err != nil {
		return nil, err
	}

	for _, rule := range rules {
		agentID, err := rs.evaluateRule(ctx, rule)
		if err != nil {
			continue
		}
		if agentID != nil {
			return agentID, nil
		}
	}

	return nil, nil
}

func (rs *ChatRoutingService) evaluateRule(ctx context.Context, rule *models.ChatRoutingRule) (*models.UserID, error) {
	if len(rule.AgentIDs) == 0 {
		return nil, nil
	}

	switch rule.Strategy {
	case models.StrategyLeastActive:
		return rs.leastActive(ctx, rule.AgentIDs, rule.MaxConcurrentChats)
	default:
		return rs.roundRobin(ctx, rule, rule.AgentIDs, rule.MaxConcurrentChats)
	}
}

func (rs *ChatRoutingService) roundRobin(ctx context.Context, rule *models.ChatRoutingRule, agentIDs []models.UserID, maxChats int) (*models.UserID, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	idx := rs.roundRobinIdx[rule.ID]
	n := len(agentIDs)

	for i := 0; i < n; i++ {
		agentID := agentIDs[(idx+i)%n]
		count, err := rs.store.CountActiveChatsForAgent(ctx, agentID)
		if err != nil {
			continue
		}
		if count < maxChats {
			rs.roundRobinIdx[rule.ID] = (idx + i + 1) % n
			return &agentID, nil
		}
	}

	return nil, nil
}

func (rs *ChatRoutingService) leastActive(ctx context.Context, agentIDs []models.UserID, maxChats int) (*models.UserID, error) {
	var bestAgent *models.UserID
	bestCount := maxChats

	for _, agentID := range agentIDs {
		count, err := rs.store.CountActiveChatsForAgent(ctx, agentID)
		if err != nil {
			continue
		}
		if count < bestCount {
			bestCount = count
			aid := agentID
			bestAgent = &aid
		}
	}

	return bestAgent, nil
}

// FindAvailableAgentForTicket filters candidates by full department/account access.
func (rs *ChatRoutingService) FindAvailableAgentForTicket(ctx context.Context, ticket *models.Ticket) (*models.UserID, error) {
	access := store.AccessFor(rs.store)
	if !access.Restricted() {
		return rs.FindAvailableAgent(ctx, ticket.DepartmentID)
	}
	rules, err := rs.store.ListActiveChatRoutingRules(ctx, ticket.DepartmentID)
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		copy := *rule
		copy.AgentIDs = nil
		for _, id := range rule.AgentIDs {
			if access.CanViewUser(ctx, id, ticket.ID, true) {
				copy.AgentIDs = append(copy.AgentIDs, id)
			}
		}
		id, err := rs.evaluateRule(ctx, &copy)
		if err == nil && id != nil {
			return id, nil
		}
	}
	return nil, nil
}
