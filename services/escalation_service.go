package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// EscalationService is the time-based escalation rules engine. It scans
// open tickets against active EscalationRule rows and applies matched
// actions. Mirrors the Laravel EscalationService.
//
// Distinct from AutomationRunner (general time-based automations) and the
// event-driven WorkflowEngine. Schedule by calling EvaluateRules() from a
// host-app cron — every 5 minutes is the portfolio convention.
type EscalationService struct {
	Access *ticketaccess.Access
	DB     *sql.DB
	Logger *log.Logger

	// Tickets, when set, carries status, priority, assignment and department
	// actions through the ticket service, so they record activity and fire
	// webhooks and workflows (Laravel routes these through TicketService).
	// The routers set it; when nil the actions write the row directly.
	Tickets *TicketService
}

// NewEscalationService constructs a service with the given DB and a logger
// (a default logger is used if nil).
func NewEscalationService(db *sql.DB, logger *log.Logger, access ...*ticketaccess.Access) *EscalationService {
	if logger == nil {
		logger = log.Default()
	}
	var policy *ticketaccess.Access
	if len(access) > 0 {
		policy = access[0]
	}
	return &EscalationService{DB: db, Logger: logger, Access: policy}
}

// EvaluateRules evaluates all active escalation rules and applies their
// actions to matching open tickets. Returns the count of (rule × ticket)
// applications. Per-rule failures are logged and swallowed so one bad rule
// does not abort the rest.
func (s *EscalationService) EvaluateRules() (int, error) {
	rows, err := s.DB.Query(
		s.query(`SELECT id, name, conditions, actions
		   FROM escalated_escalation_rules
		  WHERE is_active = TRUE
		  ORDER BY sort_order ASC, id ASC`),
	)
	if err != nil {
		return 0, fmt.Errorf("escalation: list active: %w", err)
	}
	defer rows.Close()

	var rules []models.EscalationRule
	for rows.Next() {
		var rule models.EscalationRule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Conditions, &rule.Actions); err != nil {
			return 0, fmt.Errorf("escalation: scan: %w", err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("escalation: rows: %w", err)
	}

	affected := 0
	for _, rule := range rules {
		n, err := s.evaluateOne(rule)
		if err != nil {
			s.Logger.Printf("escalation rule #%d (%s) failed: %v", rule.ID, rule.Name, err)
			continue
		}
		affected += n
	}

	return affected, nil
}

func (s *EscalationService) evaluateOne(rule models.EscalationRule) (int, error) {
	ids, err := s.findMatchingTicketIDs(rule)
	if err != nil {
		return 0, err
	}

	for _, id := range ids {
		s.executeActions(rule, id)
	}

	return len(ids), nil
}

func (s *EscalationService) findMatchingTicketIDs(rule models.EscalationRule) ([]int64, error) {
	var conds []models.EscalationCondition
	if len(rule.Conditions) > 0 {
		if err := json.Unmarshal(rule.Conditions, &conds); err != nil {
			return nil, fmt.Errorf("parse conditions: %w", err)
		}
	}

	clauses, args := escalationClauses(conds)
	q := fmt.Sprintf(
		`SELECT id FROM escalated_tickets WHERE %s`,
		strings.Join(clauses, " AND "),
	)

	rows, err := s.DB.Query(s.query(q), args...)
	if err != nil {
		return nil, fmt.Errorf("query tickets: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan ticket id: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// escalationClauses builds the SQL WHERE clauses and args for an escalation
// rule's conditions, scoped to open tickets (not resolved or closed). Pure
// (no DB) so it can be unit-tested. Status / priority are integers in the
// Go port, so their values are coerced via toInt.
func escalationClauses(conds []models.EscalationCondition) ([]string, []interface{}) {
	clauses := []string{"resolved_at IS NULL", "closed_at IS NULL"}
	args := []interface{}{}

	for _, c := range conds {
		switch c.Field {
		case "account":
			clause, bindings := accountClause(c.Value, c.Operator)
			clauses = append(clauses, clause)
			args = append(args, bindings...)
		case "ticket_type":
			clauses = append(clauses, "ticket_type = ?")
			args = append(args, toString(c.Value))
		case "status":
			clauses = append(clauses, "status = ?")
			args = append(args, toInt(c.Value))
		case "priority":
			clauses = append(clauses, "priority = ?")
			args = append(args, toInt(c.Value))
		case "assigned":
			if s, _ := c.Value.(string); s == "unassigned" {
				clauses = append(clauses, "assigned_to IS NULL")
			} else {
				clauses = append(clauses, "assigned_to IS NOT NULL")
			}
		case "age_hours":
			clauses = append(clauses, "created_at <= ?")
			args = append(args, hoursAgo(toInt(c.Value)))
		case "no_response_hours":
			clauses = append(clauses, "first_response_at IS NULL")
			clauses = append(clauses, "created_at <= ?")
			args = append(args, hoursAgo(toInt(c.Value)))
		case "sla_breached":
			clauses = append(clauses, "sla_breached = 1")
		case "department_id":
			clauses = append(clauses, "department_id = ?")
			args = append(args, toInt(c.Value))
			// Unknown fields skipped silently for forward-compat.
		}
	}

	return clauses, args
}

func (s *EscalationService) executeActions(rule models.EscalationRule, ticketID int64) {
	var actions []models.EscalationAction
	if len(rule.Actions) > 0 {
		if err := json.Unmarshal(rule.Actions, &actions); err != nil {
			s.Logger.Printf("escalation rule #%d: parse actions: %v", rule.ID, err)
			return
		}
	}

	for _, action := range actions {
		if err := s.runAction(action, ticketID); err != nil {
			s.Logger.Printf("escalation rule #%d action %s on ticket #%d failed: %v",
				rule.ID, action.Type, ticketID, err)
		}
	}
}

func (s *EscalationService) runAction(action models.EscalationAction, ticketID int64) error {
	if s.Tickets != nil {
		ctx := context.Background()
		switch action.Type {
		case "escalate":
			return s.Tickets.ChangeStatus(ctx, ticketID, models.StatusEscalated, nil)
		case "change_priority":
			priority, ok := PriorityFromValue(action.Value)
			if !ok {
				return ErrInvalidPriority
			}
			return s.Tickets.ChangePriority(ctx, ticketID, priority, nil)
		case "assign_to":
			return s.Tickets.Assign(ctx, ticketID, models.UserID(toString(action.Value)), nil)
		case "change_department":
			return s.Tickets.ChangeDepartment(ctx, ticketID, int64(toInt(action.Value)), nil)
		}
	}
	switch action.Type {
	case "escalate":
		_, err := s.DB.Exec(
			s.query(`UPDATE escalated_tickets SET status = ?, updated_at = ? WHERE id = ?`),
			models.StatusEscalated, time.Now(), ticketID,
		)
		return err
	case "change_priority":
		_, err := s.DB.Exec(
			s.query(`UPDATE escalated_tickets SET priority = ?, updated_at = ? WHERE id = ?`),
			toInt(action.Value), time.Now(), ticketID,
		)
		return err
	case "assign_to":
		if s.Access != nil && !s.Access.CanViewUser(context.Background(), models.UserID(toString(action.Value)), ticketID, true) {
			return fmt.Errorf("agent cannot access ticket")
		}
		_, err := s.DB.Exec(
			s.query(`UPDATE escalated_tickets SET assigned_to = ?, updated_at = ? WHERE id = ?`),
			models.UserID(toString(action.Value)), time.Now(), ticketID,
		)
		return err
	case "change_department":
		_, err := s.DB.Exec(
			s.query(`UPDATE escalated_tickets SET department_id = ?, updated_at = ? WHERE id = ?`),
			toInt(action.Value), time.Now(), ticketID,
		)
		return err
	}
	// Unknown action type — skip silently.
	return nil
}

func (s *EscalationService) query(query string) string {
	if s.Access != nil {
		query = strings.ReplaceAll(query, "escalated_", s.Access.Prefix)
	}
	return sqldialect.Rebind(s.DB, query)
}
