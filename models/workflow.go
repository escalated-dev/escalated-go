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
	Trigger      string          `json:"trigger"` // Alias for frontend compatibility
	Conditions   json.RawMessage `json:"conditions"`
	Actions      json.RawMessage `json:"actions"`
	Position     int             `json:"position"`
	IsActive     bool            `json:"is_active"`
	StopOnMatch  bool            `json:"stop_on_match"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// ComputeTrigger populates the Trigger alias from TriggerEvent.
func (w *Workflow) ComputeTrigger() {
	w.Trigger = w.TriggerEvent
}

// WorkflowLog records the execution result of a workflow.
type WorkflowLog struct {
	ID                int64           `json:"-"`
	WorkflowID        int64           `json:"-"`
	TicketID          int64           `json:"-"`
	TriggerEvent      string          `json:"-"`
	ConditionsMatched bool            `json:"-"`
	ActionsExecutedRaw json.RawMessage `json:"-"`
	ErrorMessage      *string         `json:"-"`
	StartedAt         *time.Time      `json:"-"`
	CompletedAt       *time.Time      `json:"-"`
	CreatedAt         time.Time       `json:"-"`

	// Relationship data (populated via JOIN)
	WorkflowName    *string `json:"-"`
	TicketReference *string `json:"-"`
}

// WorkflowLogJSON is the serialized form expected by the frontend.
type WorkflowLogJSON struct {
	ID              int64           `json:"id"`
	WorkflowID      int64           `json:"workflow_id"`
	TicketID        int64           `json:"ticket_id"`
	TriggerEvent    string          `json:"trigger_event"`
	Event           string          `json:"event"`
	WorkflowName    *string         `json:"workflow_name"`
	TicketReference *string         `json:"ticket_reference"`
	Matched         bool            `json:"matched"`
	ActionsExecuted int             `json:"actions_executed"`
	ActionDetails   json.RawMessage `json:"action_details"`
	DurationMs      *int64          `json:"duration_ms"`
	Status          string          `json:"status"`
	ErrorMessage    *string         `json:"error_message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// ToJSON converts a WorkflowLog to its frontend-compatible JSON representation.
func (l *WorkflowLog) ToJSON() WorkflowLogJSON {
	// Count actions in the raw JSON array
	var actions []json.RawMessage
	actionsCount := 0
	rawActions := l.ActionsExecutedRaw
	if rawActions == nil {
		rawActions = json.RawMessage("[]")
	}
	if err := json.Unmarshal(rawActions, &actions); err == nil {
		actionsCount = len(actions)
	}

	// Compute duration
	var durationMs *int64
	if l.StartedAt != nil && l.CompletedAt != nil {
		d := l.CompletedAt.Sub(*l.StartedAt).Milliseconds()
		durationMs = &d
	}

	// Compute status
	status := "success"
	if l.ErrorMessage != nil && *l.ErrorMessage != "" {
		status = "failed"
	}

	return WorkflowLogJSON{
		ID:              l.ID,
		WorkflowID:      l.WorkflowID,
		TicketID:        l.TicketID,
		TriggerEvent:    l.TriggerEvent,
		Event:           l.TriggerEvent,
		WorkflowName:    l.WorkflowName,
		TicketReference: l.TicketReference,
		Matched:         l.ConditionsMatched,
		ActionsExecuted: actionsCount,
		ActionDetails:   rawActions,
		DurationMs:      durationMs,
		Status:          status,
		ErrorMessage:    l.ErrorMessage,
		CreatedAt:       l.CreatedAt,
	}
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
