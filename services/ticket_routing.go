package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

func routingAccount(value any) *ticketaccess.Account {
	var raw []byte
	if text, ok := value.(string); ok {
		raw = []byte(text)
	} else {
		raw, _ = json.Marshal(value)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var pair map[string]any
	if decoder.Decode(&pair) != nil {
		return nil
	}
	kind, ok := pair["type"].(string)
	if !ok || kind == "" {
		return nil
	}
	var id string
	switch value := pair["id"].(type) {
	case string:
		id = value
	case json.Number:
		id = value.String()
	default:
		return nil
	}
	if id == "" {
		return nil
	}
	return &ticketaccess.Account{Type: kind, ID: id}
}
func accountClause(value any, operator string) (string, []any) {
	account := routingAccount(value)
	if account == nil {
		return "1=0", nil
	}
	switch operator {
	case "", "equals", "=":
		return "(account_type = ? AND account_id = ?)", []any{account.Type, account.ID}
	case "not_equals", "!=":
		return "(account_type IS NULL OR account_id IS NULL OR account_type <> ? OR account_id <> ?)", []any{account.Type, account.ID}
	default:
		return "1=0", nil
	}
}

// UnmarshalJSON accepts account objects while preserving the string Value API.
func (c *WorkflowCondition) UnmarshalJSON(data []byte) error {
	var raw struct {
		Field, Operator string
		Value           json.RawMessage
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Field, c.Operator = raw.Field, raw.Operator
	if len(raw.Value) == 0 {
		c.Value = ""
		return nil
	}
	if err := json.Unmarshal(raw.Value, &c.Value); err != nil {
		if c.Field != "account" {
			return fmt.Errorf("workflow condition requires a string value")
		}
		c.Value = string(raw.Value)
	}
	return nil
}
func selectTargetedPolicy(policies []*models.SLAPolicy, ticket *models.Ticket) *models.SLAPolicy {
	var selected *models.SLAPolicy
	best := -1
	for _, policy := range policies {
		if !policy.IsActive {
			continue
		}
		if policy.DepartmentID != nil && (ticket.DepartmentID == nil || *policy.DepartmentID != *ticket.DepartmentID) {
			continue
		}
		if policy.TicketType != nil && *policy.TicketType != ticket.TicketType {
			continue
		}
		rank := -1
		if policy.DepartmentID != nil {
			rank = 2
		}
		if policy.TicketType != nil {
			if rank == 2 {
				rank = 3
			} else {
				rank = 1
			}
		}
		if rank < 0 && policy.IsDefault {
			rank = 0
		}
		if rank >= 0 && (rank > best || rank == best && (selected == nil || policy.ID < selected.ID)) {
			selected = policy
			best = rank
		}
	}
	return selected
}
