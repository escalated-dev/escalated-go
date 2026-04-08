package models

import "time"

// Routing strategy constants.
const (
	StrategyRoundRobin  = "round_robin"
	StrategyLeastActive = "least_active"
	StrategyDepartment  = "department"
)

// ChatRoutingRule defines how incoming chat sessions are routed to agents.
type ChatRoutingRule struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Strategy           string    `json:"strategy"`
	DepartmentID       *int64    `json:"department_id,omitempty"`
	AgentIDs           []int64   `json:"agent_ids,omitempty"`
	Priority           int       `json:"priority"`
	MaxConcurrentChats int       `json:"max_concurrent_chats"`
	IsActive           bool      `json:"is_active"`
	CreatedAt          time.Time `json:"created_at"`
}
