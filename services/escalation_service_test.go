package services

import (
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func TestEscalationClausesBaseOnly(t *testing.T) {
	clauses, args := escalationClauses(nil)
	if len(clauses) != 2 {
		t.Fatalf("base clauses = %v, want the 2 open-ticket guards", clauses)
	}
	if len(args) != 0 {
		t.Fatalf("base args = %v, want none", args)
	}
	joined := strings.Join(clauses, " AND ")
	if !strings.Contains(joined, "resolved_at IS NULL") || !strings.Contains(joined, "closed_at IS NULL") {
		t.Errorf("base clauses missing open guards: %q", joined)
	}
}

func TestEscalationClausesConditions(t *testing.T) {
	conds := []models.EscalationCondition{
		{Field: "status", Value: float64(4)},
		{Field: "priority", Value: float64(3)},
		{Field: "assigned", Value: "unassigned"},
		{Field: "sla_breached"},
		{Field: "age_hours", Value: float64(24)},
		{Field: "department_id", Value: float64(7)},
		{Field: "unknown_field", Value: "x"},
	}

	clauses, args := escalationClauses(conds)
	joined := strings.Join(clauses, " AND ")

	for _, want := range []string{
		"status = ?",
		"priority = ?",
		"assigned_to IS NULL",
		"sla_breached = 1",
		"created_at <= ?",
		"department_id = ?",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("clauses missing %q in %q", want, joined)
		}
	}

	if strings.Contains(joined, "unknown_field") {
		t.Errorf("unknown field should be skipped, got %q", joined)
	}

	// One arg each for status, priority, age_hours, department_id (4).
	// assigned + sla_breached add clauses but no args.
	if len(args) != 4 {
		t.Errorf("args = %v, want 4", args)
	}
}

func TestEscalationClausesAssignedNonUnassigned(t *testing.T) {
	clauses, _ := escalationClauses([]models.EscalationCondition{
		{Field: "assigned", Value: "assigned"},
	})
	if !strings.Contains(strings.Join(clauses, " AND "), "assigned_to IS NOT NULL") {
		t.Errorf("assigned (non-unassigned) should require assigned_to IS NOT NULL: %v", clauses)
	}
}
