package services

import "testing"

func TestEvaluateConditionsAllMatch(t *testing.T) {
	ticket := TicketData{Status: "open", Priority: "medium"}
	conditions := WorkflowConditionGroup{
		All: []WorkflowCondition{
			{Field: "status", Operator: "equals", Value: "open"},
			{Field: "priority", Operator: "equals", Value: "medium"},
		},
	}
	if !EvaluateConditions(conditions, ticket) {
		t.Error("expected all conditions to match")
	}
}

func TestEvaluateConditionsAllFail(t *testing.T) {
	ticket := TicketData{Status: "closed", Priority: "medium"}
	conditions := WorkflowConditionGroup{
		All: []WorkflowCondition{
			{Field: "status", Operator: "equals", Value: "open"},
			{Field: "priority", Operator: "equals", Value: "medium"},
		},
	}
	if EvaluateConditions(conditions, ticket) {
		t.Error("expected conditions to fail")
	}
}

func TestEvaluateConditionsAny(t *testing.T) {
	ticket := TicketData{Status: "open"}
	conditions := WorkflowConditionGroup{
		Any: []WorkflowCondition{
			{Field: "status", Operator: "equals", Value: "closed"},
			{Field: "status", Operator: "equals", Value: "open"},
		},
	}
	if !EvaluateConditions(conditions, ticket) {
		t.Error("expected any condition to match")
	}
}

func TestContainsOperator(t *testing.T) {
	ticket := TicketData{Subject: "Important billing issue"}
	conditions := WorkflowConditionGroup{
		All: []WorkflowCondition{
			{Field: "subject", Operator: "contains", Value: "billing"},
		},
	}
	if !EvaluateConditions(conditions, ticket) {
		t.Error("expected contains to match")
	}
}

func TestIsEmptyOperator(t *testing.T) {
	ticket := TicketData{Description: ""}
	conditions := WorkflowConditionGroup{
		All: []WorkflowCondition{
			{Field: "description", Operator: "is_empty", Value: ""},
		},
	}
	if !EvaluateConditions(conditions, ticket) {
		t.Error("expected is_empty to match")
	}
}

func TestInterpolateVariables(t *testing.T) {
	ticket := TicketData{Reference: "ESC-001", Subject: "Test", Status: "open"}
	result := InterpolateVariables("Ticket {{reference}} is {{status}}", ticket)
	expected := "Ticket ESC-001 is open"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestDryRun(t *testing.T) {
	ticket := TicketData{Status: "open", Reference: "ESC-001"}
	conditions := WorkflowConditionGroup{
		All: []WorkflowCondition{
			{Field: "status", Operator: "equals", Value: "open"},
		},
	}
	actions := []WorkflowAction{
		{Type: "add_note", Value: "Note for {{reference}}"},
	}
	result := DryRun(conditions, actions, ticket)
	if !result.Matched {
		t.Error("expected matched to be true")
	}
	if result.Actions[0].Value != "Note for ESC-001" {
		t.Errorf("expected interpolated value, got %q", result.Actions[0].Value)
	}
	if !result.Actions[0].WouldExecute {
		t.Error("expected would_execute to be true")
	}
}
