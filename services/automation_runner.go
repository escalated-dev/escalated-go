// Package services contains business-logic services for the Escalated Go port.
package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// AutomationRunner is the time-based admin rules engine. It scans open
// tickets against active Automation rows and applies matched actions.
//
// Distinct from the event-driven Workflow engine and the agent-manual
// Macro feature. See escalated-developer-context/domain-model/
// workflows-automations-macros.md.
//
// Schedule by calling Run() from a host-app cron / scheduler — every
// 5 minutes is the portfolio convention.
type AutomationRunner struct {
	DB     *sql.DB
	Logger *log.Logger
}

// NewAutomationRunner constructs a runner with the given DB and a logger
// (a default logger is used if nil).
func NewAutomationRunner(db *sql.DB, logger *log.Logger) *AutomationRunner {
	if logger == nil {
		logger = log.Default()
	}
	return &AutomationRunner{DB: db, Logger: logger}
}

// Run evaluates all active automations and applies actions to matching
// tickets. Returns the count of (automation × ticket) action applications.
//
// Per-action and per-automation failures are logged and swallowed so a
// single bad rule does not abort the rest.
func (r *AutomationRunner) Run() (int, error) {
	rows, err := r.DB.Query(
		`SELECT id, name, description, conditions, actions, active, position, last_run_at
		   FROM escalated_automations
		  WHERE active = TRUE
		  ORDER BY position ASC, id ASC`,
	)
	if err != nil {
		return 0, fmt.Errorf("automation: list active: %w", err)
	}
	defer rows.Close()

	var automations []models.Automation
	for rows.Next() {
		var a models.Automation
		var lastRun sql.NullTime
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Description, &a.Conditions, &a.Actions,
			&a.Active, &a.Position, &lastRun,
		); err != nil {
			return 0, fmt.Errorf("automation: scan: %w", err)
		}
		if lastRun.Valid {
			a.LastRunAt = &lastRun.Time
		}
		automations = append(automations, a)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("automation: rows: %w", err)
	}

	affected := 0
	for _, automation := range automations {
		n, err := r.runOne(automation)
		if err != nil {
			r.Logger.Printf("automation #%d (%s) failed: %v", automation.ID, automation.Name, err)
			continue
		}
		affected += n
	}

	return affected, nil
}

func (r *AutomationRunner) runOne(automation models.Automation) (int, error) {
	tickets, err := r.findMatchingTickets(automation)
	if err != nil {
		return 0, err
	}

	for _, ticket := range tickets {
		r.executeActions(automation, ticket)
	}

	if _, err := r.DB.Exec(
		`UPDATE escalated_automations SET last_run_at = ?, updated_at = ? WHERE id = ?`,
		time.Now(), time.Now(), automation.ID,
	); err != nil {
		return 0, fmt.Errorf("update last_run_at: %w", err)
	}

	return len(tickets), nil
}

func (r *AutomationRunner) findMatchingTickets(a models.Automation) ([]models.Ticket, error) {
	var conds []models.AutomationCondition
	if len(a.Conditions) > 0 {
		if err := json.Unmarshal(a.Conditions, &conds); err != nil {
			return nil, fmt.Errorf("parse conditions: %w", err)
		}
	}

	// Open ticket = not yet resolved or closed. Match Laravel's open()
	// scope semantics with timestamp checks.
	clauses := []string{"resolved_at IS NULL", "closed_at IS NULL"}
	args := []interface{}{}

	for _, c := range conds {
		switch c.Field {
		case "hours_since_created":
			threshold := hoursAgo(toInt(c.Value))
			clauses = append(clauses, fmt.Sprintf("created_at %s ?", flipOperator(c.Operator)))
			args = append(args, threshold)
		case "hours_since_updated":
			threshold := hoursAgo(toInt(c.Value))
			clauses = append(clauses, fmt.Sprintf("updated_at %s ?", flipOperator(c.Operator)))
			args = append(args, threshold)
		case "hours_since_assigned":
			threshold := hoursAgo(toInt(c.Value))
			clauses = append(clauses, "assigned_to IS NOT NULL")
			clauses = append(clauses, fmt.Sprintf("updated_at %s ?", flipOperator(c.Operator)))
			args = append(args, threshold)
		case "status":
			clauses = append(clauses, "status = ?")
			args = append(args, c.Value)
		case "priority":
			clauses = append(clauses, "priority = ?")
			args = append(args, c.Value)
		case "assigned":
			if s, _ := c.Value.(string); s == "unassigned" {
				clauses = append(clauses, "assigned_to IS NULL")
			} else if s == "assigned" {
				clauses = append(clauses, "assigned_to IS NOT NULL")
			}
		case "subject_contains":
			clauses = append(clauses, "subject LIKE ?")
			args = append(args, "%"+toString(c.Value)+"%")
		// Unknown fields skipped silently for forward-compat.
		}
	}

	q := fmt.Sprintf(
		`SELECT id, reference, subject, status, priority, assigned_to, resolved_at, closed_at, created_at, updated_at
		   FROM escalated_tickets
		  WHERE %s`,
		strings.Join(clauses, " AND "),
	)

	rows, err := r.DB.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query tickets: %w", err)
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var assignedTo sql.NullInt64
		var resolvedAt, closedAt sql.NullTime
		if err := rows.Scan(
			&t.ID, &t.Reference, &t.Subject, &t.Status, &t.Priority,
			&assignedTo, &resolvedAt, &closedAt, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ticket: %w", err)
		}
		if assignedTo.Valid {
			t.AssignedTo = &assignedTo.Int64
		}
		if resolvedAt.Valid {
			t.ResolvedAt = &resolvedAt.Time
		}
		if closedAt.Valid {
			t.ClosedAt = &closedAt.Time
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}

func (r *AutomationRunner) executeActions(a models.Automation, t models.Ticket) {
	var actions []models.AutomationAction
	if len(a.Actions) > 0 {
		if err := json.Unmarshal(a.Actions, &actions); err != nil {
			r.Logger.Printf("automation #%d: parse actions: %v", a.ID, err)
			return
		}
	}

	for _, action := range actions {
		if err := r.runAction(a, t, action); err != nil {
			r.Logger.Printf("automation #%d action %s on ticket #%d failed: %v",
				a.ID, action.Type, t.ID, err)
		}
	}
}

func (r *AutomationRunner) runAction(a models.Automation, t models.Ticket, action models.AutomationAction) error {
	switch action.Type {
	case "change_status":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET status = ?, updated_at = ? WHERE id = ?`,
			toInt(action.Value), time.Now(), t.ID,
		)
		return err
	case "change_priority":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET priority = ?, updated_at = ? WHERE id = ?`,
			toInt(action.Value), time.Now(), t.ID,
		)
		return err
	case "assign":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET assigned_to = ?, updated_at = ? WHERE id = ?`,
			toInt(action.Value), time.Now(), t.ID,
		)
		return err
	case "add_tag":
		// Find tag id by name; ignore unknown.
		var tagID int64
		err := r.DB.QueryRow(
			`SELECT id FROM escalated_tags WHERE name = ?`,
			toString(action.Value),
		).Scan(&tagID)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		// Idempotent insert into the join table.
		_, err = r.DB.Exec(
			`INSERT OR IGNORE INTO escalated_ticket_tags (ticket_id, tag_id) VALUES (?, ?)`,
			t.ID, tagID,
		)
		return err
	case "add_note":
		md, _ := json.Marshal(map[string]interface{}{
			"system_note":   true,
			"automation_id": a.ID,
		})
		_, err := r.DB.Exec(
			`INSERT INTO escalated_replies (ticket_id, body, is_internal_note, metadata, created_at, updated_at)
			 VALUES (?, ?, TRUE, ?, ?, ?)`,
			t.ID, toString(action.Value), md, time.Now(), time.Now(),
		)
		return err
	}
	// Unknown action type — skip silently.
	return nil
}

// flipOperator: "hours_since > N" means timestamp < (now - N hours), so
// the SQL operator is the inverse of the user-facing operator.
func flipOperator(op string) string {
	switch op {
	case ">":
		return "<"
	case ">=":
		return "<="
	case "<":
		return ">"
	case "<=":
		return ">="
	case "=":
		return "="
	default:
		return "<"
	}
}

func hoursAgo(hours int) time.Time {
	return time.Now().Add(-time.Duration(hours) * time.Hour)
}

func toInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		_, _ = fmt.Sscanf(x, "%d", &n)
		return n
	}
	return 0
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
