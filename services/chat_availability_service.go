package services

import (
	"context"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// ChatEnabledSetting is the settings key that switches live chat off. Unset
// (or any value other than a false one) leaves chat on, so hosts that never
// set it keep working.
const ChatEnabledSetting = "chat_enabled"

// DefaultMaxConcurrentChats is the per-agent chat ceiling used when a routing
// rule does not set one, matching the reference implementation.
const DefaultMaxConcurrentChats = 5

// ChatAvailabilityService checks whether live chat is currently available.
//
// Chat is available when it is not switched off by the chat_enabled setting
// and at least one agent named by an active routing rule that applies (the
// department's own rules plus the global ones, or every active rule when no
// department is given) is online and below the rule's concurrent-chat limit.
type ChatAvailabilityService struct {
	store store.Store

	// AgentOnline, when set, reports whether an agent is online and accepting
	// chats. Hosts that track presence should set it. When nil, every agent a
	// routing rule names counts as accepting chats.
	AgentOnline func(ctx context.Context, agentID models.UserID) (bool, error)
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

// IsAvailable returns true if at least one agent is available for chat in any
// department.
func (as *ChatAvailabilityService) IsAvailable(ctx context.Context) (bool, error) {
	return as.IsAvailableForDepartment(ctx, nil)
}

// IsAvailableForDepartment returns true if at least one agent is available for
// chat in the given department (nil means any department).
func (as *ChatAvailabilityService) IsAvailableForDepartment(ctx context.Context, departmentID *int64) (bool, error) {
	enabled, err := as.chatEnabled(ctx)
	if err != nil || !enabled {
		return false, err
	}

	rules, err := as.store.ListActiveChatRoutingRules(ctx, departmentID)
	if err != nil {
		return false, err
	}

	checked := map[models.UserID]bool{}
	for _, rule := range rules {
		if departmentID != nil && rule.DepartmentID != nil && *rule.DepartmentID != *departmentID {
			continue
		}
		maxChats := rule.MaxConcurrentChats
		if maxChats <= 0 {
			maxChats = DefaultMaxConcurrentChats
		}
		for _, agentID := range rule.AgentIDs {
			if agentID == "" {
				continue
			}
			online, seen := checked[agentID]
			if !seen {
				online = as.agentOnline(ctx, agentID)
				checked[agentID] = online
			}
			if !online {
				continue
			}
			count, err := as.store.CountActiveChatsForAgent(ctx, agentID)
			if err != nil {
				continue
			}
			if count < maxChats {
				return true, nil
			}
		}
	}

	return false, nil
}

func (as *ChatAvailabilityService) agentOnline(ctx context.Context, agentID models.UserID) bool {
	if as.AgentOnline == nil {
		return true
	}
	online, err := as.AgentOnline(ctx, agentID)
	return err == nil && online
}

// ChatEnabled reports whether live chat is switched on (the chat_enabled
// setting; unset means on). A settings read error counts as on, so a database
// hiccup does not refuse chat codes.
func (as *ChatAvailabilityService) ChatEnabled(ctx context.Context) bool {
	enabled, err := as.chatEnabled(ctx)
	return err != nil || enabled
}

func (as *ChatAvailabilityService) chatEnabled(ctx context.Context) (bool, error) {
	v, err := as.store.GetSetting(ctx, ChatEnabledSetting)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return false, nil
	}
	return true, nil
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
	return as.GetStatusForDepartment(ctx, nil)
}

// GetStatusForDepartment returns the availability status for the widget,
// scoped to a department when departmentID is non-nil.
func (as *ChatAvailabilityService) GetStatusForDepartment(ctx context.Context, departmentID *int64) (*ChatAvailabilityStatus, error) {
	available, err := as.IsAvailableForDepartment(ctx, departmentID)
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
