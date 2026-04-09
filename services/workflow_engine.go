package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// WorkflowCondition represents a single condition to evaluate
type WorkflowCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// WorkflowConditionGroup represents AND/OR condition groups
type WorkflowConditionGroup struct {
	All []WorkflowCondition `json:"all,omitempty"`
	Any []WorkflowCondition `json:"any,omitempty"`
}

// WorkflowAction represents an action to execute
type WorkflowAction struct {
	Type             string           `json:"type"`
	Value            string           `json:"value,omitempty"`
	URL              string           `json:"url,omitempty"`
	Payload          string           `json:"payload,omitempty"`
	RemainingActions []WorkflowAction `json:"remaining_actions,omitempty"`
}

// Operators supported by the workflow engine
var Operators = []string{
	"equals", "not_equals", "contains", "not_contains", "starts_with", "ends_with",
	"greater_than", "less_than", "greater_or_equal", "less_or_equal", "is_empty", "is_not_empty",
}

// ActionTypes supported by the workflow engine
var ActionTypes = []string{
	"change_status", "assign_agent", "change_priority", "add_tag", "remove_tag",
	"set_department", "add_note", "send_webhook", "set_type", "delay",
	"add_follower", "send_notification",
}

// TriggerEvents supported by the workflow engine
var TriggerEvents = []string{
	"ticket.created", "ticket.updated", "ticket.status_changed", "ticket.assigned",
	"ticket.priority_changed", "ticket.tagged", "ticket.department_changed",
	"reply.created", "reply.agent_reply", "sla.warning", "sla.breached", "ticket.reopened",
}

// TicketData holds ticket fields for condition evaluation
type TicketData struct {
	Status       string
	Priority     string
	AssignedTo   *int
	DepartmentID *int
	Channel      string
	TicketType   string
	Subject      string
	Description  string
	Reference    string
}

// DryRunResult holds the result of a dry run
type DryRunResult struct {
	Matched bool                `json:"matched"`
	Actions []DryRunActionEntry `json:"actions"`
}

// DryRunActionEntry represents a single action preview
type DryRunActionEntry struct {
	Type         string `json:"type"`
	Value        string `json:"value"`
	WouldExecute bool   `json:"would_execute"`
}

// EvaluateConditions evaluates AND/OR condition groups against ticket data
func EvaluateConditions(group WorkflowConditionGroup, ticket TicketData) bool {
	if len(group.All) > 0 {
		for _, c := range group.All {
			if !evaluateSingle(c, ticket) {
				return false
			}
		}
		return true
	}
	if len(group.Any) > 0 {
		for _, c := range group.Any {
			if evaluateSingle(c, ticket) {
				return true
			}
		}
		return false
	}
	return true
}

func evaluateSingle(c WorkflowCondition, ticket TicketData) bool {
	actual := resolveField(c.Field, ticket)
	return applyOperator(c.Operator, actual, c.Value)
}

func resolveField(field string, ticket TicketData) string {
	switch field {
	case "status":
		return ticket.Status
	case "priority":
		return ticket.Priority
	case "subject":
		return ticket.Subject
	case "description":
		return ticket.Description
	case "channel":
		return ticket.Channel
	case "ticket_type":
		return ticket.TicketType
	case "assigned_to":
		if ticket.AssignedTo != nil {
			return strconv.Itoa(*ticket.AssignedTo)
		}
		return ""
	case "department_id":
		if ticket.DepartmentID != nil {
			return strconv.Itoa(*ticket.DepartmentID)
		}
		return ""
	default:
		return ""
	}
}

func applyOperator(operator, actual, expected string) bool {
	switch operator {
	case "equals":
		return actual == expected
	case "not_equals":
		return actual != expected
	case "contains":
		return strings.Contains(actual, expected)
	case "not_contains":
		return !strings.Contains(actual, expected)
	case "starts_with":
		return strings.HasPrefix(actual, expected)
	case "ends_with":
		return strings.HasSuffix(actual, expected)
	case "greater_than":
		a, _ := strconv.ParseFloat(actual, 64)
		e, _ := strconv.ParseFloat(expected, 64)
		return a > e
	case "less_than":
		a, _ := strconv.ParseFloat(actual, 64)
		e, _ := strconv.ParseFloat(expected, 64)
		return a < e
	case "greater_or_equal":
		a, _ := strconv.ParseFloat(actual, 64)
		e, _ := strconv.ParseFloat(expected, 64)
		return a >= e
	case "less_or_equal":
		a, _ := strconv.ParseFloat(actual, 64)
		e, _ := strconv.ParseFloat(expected, 64)
		return a <= e
	case "is_empty":
		return strings.TrimSpace(actual) == ""
	case "is_not_empty":
		return strings.TrimSpace(actual) != ""
	default:
		return false
	}
}

var interpolateRegex = regexp.MustCompile(`\{\{(\w+)\}\}`)

// InterpolateVariables replaces {{var}} placeholders with ticket data
func InterpolateVariables(text string, ticket TicketData) string {
	return interpolateRegex.ReplaceAllStringFunc(text, func(match string) string {
		varName := match[2 : len(match)-2]
		switch varName {
		case "reference":
			return ticket.Reference
		case "subject":
			return ticket.Subject
		case "status":
			return ticket.Status
		case "priority":
			return ticket.Priority
		default:
			return match
		}
	})
}

// DryRun evaluates conditions and previews actions without executing
func DryRun(conditions WorkflowConditionGroup, actions []WorkflowAction, ticket TicketData) DryRunResult {
	matched := EvaluateConditions(conditions, ticket)
	entries := make([]DryRunActionEntry, len(actions))
	for i, a := range actions {
		entries[i] = DryRunActionEntry{
			Type:         a.Type,
			Value:        InterpolateVariables(a.Value, ticket),
			WouldExecute: matched,
		}
	}
	return DryRunResult{Matched: matched, Actions: entries}
}

// Ensure fmt is used
var _ = fmt.Sprintf
