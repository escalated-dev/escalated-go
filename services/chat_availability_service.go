package services

import (
	"context"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// ChatAvailabilityService checks whether live chat is currently available.
type ChatAvailabilityService struct {
	store store.Store
}

// NewChatAvailabilityService creates a new ChatAvailabilityService.
func NewChatAvailabilityService(s store.Store) *ChatAvailabilityService {
	return &ChatAvailabilityService{store: s}
}

// ChatAvailabilityStatus holds the current chat availability info.
type ChatAvailabilityStatus struct {
	Available   bool `json:"available"`
	QueueLength int  `json:"queue_length"`
}

// IsAvailable returns true if at least one agent is available for chat.
func (as *ChatAvailabilityService) IsAvailable(ctx context.Context) (bool, error) {
	rules, err := as.store.ListActiveChatRoutingRules(ctx, nil)
	if err != nil {
		return false, err
	}

	for _, rule := range rules {
		if len(rule.AgentIDs) == 0 {
			continue
		}
		for _, agentID := range rule.AgentIDs {
			count, err := as.store.CountActiveChatsForAgent(ctx, agentID)
			if err != nil {
				continue
			}
			if count < rule.MaxConcurrentChats {
				return true, nil
			}
		}
	}

	return false, nil
}

// GetQueueLength returns the number of chat sessions waiting for an agent.
func (as *ChatAvailabilityService) GetQueueLength(ctx context.Context) (int, error) {
	waiting := models.ChatStatusWaiting
	sessions, err := as.store.ListChatSessions(ctx, models.ChatSessionFilters{Status: &waiting})
	if err != nil {
		return 0, err
	}
	return len(sessions), nil
}

// GetStatus returns the full availability status for the widget.
func (as *ChatAvailabilityService) GetStatus(ctx context.Context) (*ChatAvailabilityStatus, error) {
	available, err := as.IsAvailable(ctx)
	if err != nil {
		return nil, err
	}
	queueLen, err := as.GetQueueLength(ctx)
	if err != nil {
		return nil, err
	}
	return &ChatAvailabilityStatus{
		Available:   available,
		QueueLength: queueLen,
	}, nil
}
