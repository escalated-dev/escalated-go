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
func (rs *ChatRoutingService) FindAvailableAgent(ctx context.Context, departmentID *int64) (*int64, error) {
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

func (rs *ChatRoutingService) evaluateRule(ctx context.Context, rule *models.ChatRoutingRule) (*int64, error) {
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

func (rs *ChatRoutingService) roundRobin(ctx context.Context, rule *models.ChatRoutingRule, agentIDs []int64, maxChats int) (*int64, error) {
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

func (rs *ChatRoutingService) leastActive(ctx context.Context, agentIDs []int64, maxChats int) (*int64, error) {
	var bestAgent *int64
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
