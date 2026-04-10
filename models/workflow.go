package models

import (
	"encoding/json"
	"time"
)

// Workflow represents an automation rule that triggers on events.
type Workflow struct {
	ID           int64           `json:"id"`
	Name         string          `json:"name"`
	Description  *string         `json:"description,omitempty"`
	TriggerEvent string          `json:"trigger_event"`
	Conditions   json.RawMessage `json:"conditions"`
	Actions      json.RawMessage `json:"actions"`
	Position     int             `json:"position"`
	IsActive     bool            `json:"is_active"`
	StopOnMatch  bool            `json:"stop_on_match"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// WorkflowLog records the execution result of a workflow.
type WorkflowLog struct {
	ID              int64           `json:"id"`
	WorkflowID      int64           `json:"workflow_id"`
	TicketID        int64           `json:"ticket_id"`
	TriggerEvent    string          `json:"trigger_event"`
	Status          string          `json:"status"`
	ActionsExecuted json.RawMessage `json:"actions_executed"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// DelayedAction represents a workflow action scheduled for future execution.
type DelayedAction struct {
	ID         int64           `json:"id"`
	WorkflowID int64           `json:"workflow_id"`
	TicketID   int64           `json:"ticket_id"`
	ActionData json.RawMessage `json:"action_data"`
	ExecuteAt  time.Time       `json:"execute_at"`
	Executed   bool            `json:"executed"`
	CreatedAt  time.Time       `json:"created_at"`
}
