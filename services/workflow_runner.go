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

// WorkflowRunner is the event-driven admin rules engine. On a ticket lifecycle
// event it loads every active Workflow whose trigger_event matches (in position
// order), evaluates its conditions against the ticket, executes the matched
// actions, and records a per-run row in escalated_workflow_logs. It honors a
// workflow's stop_on_match flag.
//
// Distinct from the time-based AutomationRunner and the agent-manual Macro
// feature. See escalated-developer-context/domain-model/
// workflows-automations-macros.md.
//
// It wraps the pure evaluator in workflow_engine.go (EvaluateConditions /
// InterpolateVariables), which previously had no caller — no route, handler,
// table writer or lifecycle hook — so a workflow could never fire. The
// TicketService invokes RunForEvent off the request path (a goroutine) so a
// slow or failing workflow never blocks or breaks the ticket mutation, mirroring
// how the WebhookDispatcher was wired.
//
// Semantics mirror the NestJS WorkflowRunnerService and the Laravel
// WorkflowEngine::processEvent reference.
type WorkflowRunner struct {
	DB     *sql.DB
	Logger *log.Logger
}

// NewWorkflowRunner constructs a runner with the given DB and a logger (a
// default logger is used when nil).
func NewWorkflowRunner(db *sql.DB, logger *log.Logger) *WorkflowRunner {
	if logger == nil {
		logger = log.Default()
	}
	return &WorkflowRunner{DB: db, Logger: logger}
}

// RunForEvent evaluates and runs every active workflow subscribed to event
// against the ticket. It is safe to call in a goroutine: a panic in any single
// workflow is recovered and logged so it can never take down the process or the
// ticket mutation that scheduled it.
func (r *WorkflowRunner) RunForEvent(event string, t *models.Ticket) {
	if r == nil || r.DB == nil || t == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			r.logger().Printf("escalated workflow: RunForEvent(%s) panicked: %v", event, rec)
		}
	}()

	workflows, err := r.activeForEvent(event)
	if err != nil {
		r.logger().Printf("escalated workflow: list active for %s: %v", event, err)
		return
	}
	if len(workflows) == 0 {
		return
	}

	td := ticketToData(t)
	for _, wf := range workflows {
		if matched := r.runOne(wf, t, td, event); matched && wf.StopOnMatch {
			break
		}
	}
}

// activeForEvent loads the active workflows subscribed to event, ordered the way
// the reference engines order them (position ASC, then id ASC).
func (r *WorkflowRunner) activeForEvent(event string) ([]models.Workflow, error) {
	rows, err := r.DB.Query(
		`SELECT id, name, description, trigger_event, conditions, actions, position, is_active, stop_on_match
		   FROM escalated_workflows
		  WHERE is_active = TRUE AND trigger_event = ?
		  ORDER BY position ASC, id ASC`,
		event,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Workflow
	for rows.Next() {
		var wf models.Workflow
		var desc sql.NullString
		// Scan the JSON columns through []byte: modernc SQLite returns TEXT as a
		// string, which will not scan straight into json.RawMessage, while
		// []byte accepts both a string and a []byte driver value (Postgres).
		var conditions, actions []byte
		if err := rows.Scan(
			&wf.ID, &wf.Name, &desc, &wf.TriggerEvent, &conditions, &actions,
			&wf.Position, &wf.IsActive, &wf.StopOnMatch,
		); err != nil {
			return nil, err
		}
		if desc.Valid {
			wf.Description = &desc.String
		}
		wf.Conditions = json.RawMessage(conditions)
		wf.Actions = json.RawMessage(actions)
		out = append(out, wf)
	}
	return out, rows.Err()
}

// runOne evaluates a single workflow, executes its actions when matched, and
// writes exactly one workflow_logs row. Returns whether the conditions matched
// (used to honor stop_on_match). A panic inside an action is recovered and
// stamped on a failed log row so one bad workflow never aborts the rest.
func (r *WorkflowRunner) runOne(wf models.Workflow, t *models.Ticket, td TicketData, event string) (matched bool) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger().Printf("escalated workflow #%d panicked on ticket #%d: %v", wf.ID, t.ID, rec)
			r.writeLog(wf.ID, t.ID, event, "failed", wf.Actions, fmt.Sprintf("panic: %v", rec))
		}
	}()

	matched = EvaluateConditions(parseWorkflowConditions(wf.Conditions), td)
	if !matched {
		r.writeLog(wf.ID, t.ID, event, "skipped", nil, "")
		return false
	}

	if err := r.executeActions(wf, t, td, parseWorkflowActions(wf.Actions)); err != nil {
		r.logger().Printf("escalated workflow #%d (%s) failed on ticket #%d: %v", wf.ID, wf.Name, t.ID, err)
		r.writeLog(wf.ID, t.ID, event, "failed", wf.Actions, err.Error())
		return true
	}

	r.writeLog(wf.ID, t.ID, event, "success", wf.Actions, "")
	return true
}

// executeActions runs the workflow's actions in order. A `delay` step defers the
// remaining actions to escalated_delayed_actions and stops (mirroring the
// NestJS/Laravel executors). The first action error aborts the run so it can be
// stamped on the log, matching the reference try/catch semantics.
func (r *WorkflowRunner) executeActions(wf models.Workflow, t *models.Ticket, td TicketData, actions []WorkflowAction) error {
	for i, a := range actions {
		if a.Type == "delay" {
			return r.scheduleDelayed(wf, t, actions[i+1:], a)
		}
		if err := r.runAction(wf, t, td, a); err != nil {
			return err
		}
	}
	return nil
}

// scheduleDelayed records the actions following a `delay` step for later
// execution. A host scheduler drains escalated_delayed_actions once execute_at
// passes (follow-up, same cadence the AutomationRunner runs on).
func (r *WorkflowRunner) scheduleDelayed(wf models.Workflow, t *models.Ticket, remaining []WorkflowAction, delay WorkflowAction) error {
	data, err := json.Marshal(remaining)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = r.DB.Exec(
		`INSERT INTO escalated_delayed_actions (workflow_id, ticket_id, action_data, execute_at, executed, created_at)
		 VALUES (?, ?, ?, ?, FALSE, ?)`,
		wf.ID, t.ID, data, now.Add(time.Duration(toInt(delay.Value))*time.Minute), now,
	)
	return err
}

// runAction applies a single workflow action to the ticket. Column/table names
// match the live schema and the sibling AutomationRunner. Unknown action types
// are skipped silently for forward-compat.
func (r *WorkflowRunner) runAction(wf models.Workflow, t *models.Ticket, td TicketData, a WorkflowAction) error {
	switch a.Type {
	case "change_status":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET status = ?, updated_at = ? WHERE id = ?`,
			toInt(a.Value), time.Now(), t.ID,
		)
		return err
	case "change_priority":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET priority = ?, updated_at = ? WHERE id = ?`,
			toInt(a.Value), time.Now(), t.ID,
		)
		return err
	case "assign_agent":
		v := strings.TrimSpace(a.Value)
		if v == "" || v == "0" {
			return nil
		}
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET assigned_to = ?, updated_at = ? WHERE id = ?`,
			models.UserID(v), time.Now(), t.ID,
		)
		return err
	case "set_department":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET department_id = ?, updated_at = ? WHERE id = ?`,
			toInt(a.Value), time.Now(), t.ID,
		)
		return err
	case "set_type":
		_, err := r.DB.Exec(
			`UPDATE escalated_tickets SET ticket_type = ?, updated_at = ? WHERE id = ?`,
			a.Value, time.Now(), t.ID,
		)
		return err
	case "add_tag":
		return r.tagAction(t.ID, a.Value, true)
	case "remove_tag":
		return r.tagAction(t.ID, a.Value, false)
	case "add_note", "add_internal_note":
		_, err := r.DB.Exec(
			`INSERT INTO escalated_replies (ticket_id, body, is_internal, is_system, created_at, updated_at)
			 VALUES (?, ?, TRUE, TRUE, ?, ?)`,
			t.ID, InterpolateVariables(a.Value, td), time.Now(), time.Now(),
		)
		return err
	case "add_follower":
		v := strings.TrimSpace(a.Value)
		if v == "" || v == "0" {
			return nil
		}
		return models.AddFollower(r.DB, t.ID, models.UserID(v))
	case "send_notification":
		// Escalated has no built-in notification fan-out; log for the host to
		// deliver (mirrors the Laravel actionSendNotification stub).
		r.logger().Printf("escalated workflow #%d notification on ticket #%d: %s", wf.ID, t.ID, a.Value)
		return nil
	case "send_webhook":
		// Inline workflow webhooks are superseded by this port's dedicated
		// outbound-webhooks subsystem (signed, retried, audited). Skipped here to
		// avoid a second unsigned, SSRF-prone delivery path; logged for trace.
		r.logger().Printf("escalated workflow #%d: send_webhook skipped (use outbound webhooks) on ticket #%d", wf.ID, t.ID)
		return nil
	}
	// Unknown action type — skip silently (matches AutomationRunner).
	return nil
}

// tagAction attaches (add=true) or detaches (add=false) a tag by name. Unknown
// tag names are a no-op, mirroring the AutomationRunner.
func (r *WorkflowRunner) tagAction(ticketID int64, name string, add bool) error {
	var tagID int64
	err := r.DB.QueryRow(`SELECT id FROM escalated_tags WHERE name = ?`, name).Scan(&tagID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if add {
		_, err = r.DB.Exec(
			`INSERT OR IGNORE INTO escalated_ticket_tags (ticket_id, tag_id) VALUES (?, ?)`,
			ticketID, tagID,
		)
		return err
	}
	_, err = r.DB.Exec(
		`DELETE FROM escalated_ticket_tags WHERE ticket_id = ? AND tag_id = ?`,
		ticketID, tagID,
	)
	return err
}

// writeLog records one escalated_workflow_logs row for a run. status is one of
// "skipped" (conditions did not match), "success", or "failed". actionsRaw is
// the workflow's action list when matched, nil (→ "[]") when skipped.
func (r *WorkflowRunner) writeLog(workflowID, ticketID int64, event, status string, actionsRaw json.RawMessage, errMsg string) {
	actions := "[]"
	if len(actionsRaw) > 0 {
		actions = string(actionsRaw)
	}
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	if _, err := r.DB.Exec(
		`INSERT INTO escalated_workflow_logs (workflow_id, ticket_id, trigger_event, status, actions_executed, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		workflowID, ticketID, event, status, actions, errVal, time.Now(),
	); err != nil {
		r.logger().Printf("escalated workflow: write log (workflow=%d ticket=%d): %v", workflowID, ticketID, err)
	}
}

func (r *WorkflowRunner) logger() *log.Logger {
	if r.Logger == nil {
		return log.Default()
	}
	return r.Logger
}

// ticketToData projects a persisted ticket onto the flat, string-valued
// TicketData the pure evaluator compares against. Status and priority use their
// string names to match the values authored in workflow conditions.
func ticketToData(t *models.Ticket) TicketData {
	td := TicketData{
		Status:      models.StatusName[t.Status],
		Priority:    models.PriorityName[t.Priority],
		TicketType:  t.TicketType,
		Subject:     t.Subject,
		Description: t.Description,
		Reference:   t.Reference,
	}
	if t.AssignedTo != nil {
		td.AssignedTo = string(*t.AssignedTo)
	}
	if t.Channel != nil {
		td.Channel = *t.Channel
	}
	if t.DepartmentID != nil {
		d := int(*t.DepartmentID)
		td.DepartmentID = &d
	}
	return td
}

func parseWorkflowConditions(raw json.RawMessage) WorkflowConditionGroup {
	var g WorkflowConditionGroup
	if len(raw) == 0 {
		return g
	}
	_ = json.Unmarshal(raw, &g)
	return g
}

func parseWorkflowActions(raw json.RawMessage) []WorkflowAction {
	var a []WorkflowAction
	if len(raw) == 0 {
		return a
	}
	_ = json.Unmarshal(raw, &a)
	return a
}
