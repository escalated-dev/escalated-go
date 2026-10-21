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
	Access *ticketaccess.Access
	DB     *sql.DB
	Logger *log.Logger
	// Tickets, when set, carries tag actions through the ticket service so
	// they record activity and announce ticket.tag_added / ticket.tag_removed.
	// Without it the runner writes the join row directly and announces nothing.
	Tickets *TicketService
}

// NewWorkflowRunner constructs a runner with the given DB and a logger (a
// default logger is used when nil).
func NewWorkflowRunner(db *sql.DB, logger *log.Logger, access ...*ticketaccess.Access) *WorkflowRunner {
	if logger == nil {
		logger = log.Default()
	}
	var policy *ticketaccess.Access
	if len(access) > 0 {
		policy = access[0]
	}
	return &WorkflowRunner{DB: db, Logger: logger, Access: policy}
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

	event = models.CanonicalEventName(event)
	workflows, err := r.activeForEvent(event)
	if err != nil {
		r.logger().Printf("escalated workflow: list active for %s: %v", event, err)
		return
	}
	if len(workflows) == 0 {
		return
	}

	td := ticketToData(t)
	td.Tags = r.tagNames(t.ID)
	for _, wf := range workflows {
		if matched := r.runOne(wf, t, td, event); matched && wf.StopOnMatch {
			break
		}
	}
}

// activeForEvent loads the active workflows subscribed to event, ordered the way
// the reference engines order them (position ASC, then id ASC).
//
// A workflow saved under a legacy name of the event (ticket.replied for
// reply.created, ticket.tagged for ticket.tag_added, ...) matches too.
func (r *WorkflowRunner) activeForEvent(event string) ([]models.Workflow, error) {
	names := models.EventNamesFor(event)
	args := make([]any, len(names))
	marks := make([]string, len(names))
	for i, n := range names {
		args[i] = n
		marks[i] = "?"
	}
	rows, err := r.DB.Query(
		r.query(`SELECT id, name, description, trigger_event, conditions, actions, position, is_active, stop_on_match
		   FROM escalated_workflows
		  WHERE is_active = TRUE AND trigger_event IN (`+strings.Join(marks, ", ")+`)
		  ORDER BY position ASC, id ASC`),
		args...,
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
		// string, which will not scan straight into models.JSONText, while
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
		wf.Conditions = models.JSONText(conditions)
		wf.Actions = models.JSONText(actions)
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
		r.query(`INSERT INTO escalated_delayed_actions (workflow_id, ticket_id, action_data, execute_at, executed, created_at)
		 VALUES (?, ?, ?, ?, FALSE, ?)`),
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
		// The builder sends the slug ("resolved"); older workflows stored the
		// number. An unknown value is an error rather than a silent reopen.
		status, ok := StatusFromValue(strings.TrimSpace(a.Value))
		if !ok {
			return ErrInvalidStatus
		}
		_, err := r.DB.Exec(
			r.query(`UPDATE escalated_tickets SET status = ?, updated_at = ? WHERE id = ?`),
			status, time.Now(), t.ID,
		)
		return err
	case "change_priority":
		priority, ok := PriorityFromValue(strings.TrimSpace(a.Value))
		if !ok {
			return ErrInvalidPriority
		}
		_, err := r.DB.Exec(
			r.query(`UPDATE escalated_tickets SET priority = ?, updated_at = ? WHERE id = ?`),
			priority, time.Now(), t.ID,
		)
		return err
	case "assign_agent":
		if r.Access != nil && !r.Access.CanViewUser(context.Background(), models.UserID(a.Value), t.ID, true) {
			return fmt.Errorf("agent cannot access ticket")
		}
		v := strings.TrimSpace(a.Value)
		if v == "" || v == "0" {
			return nil
		}
		_, err := r.DB.Exec(
			r.query(`UPDATE escalated_tickets SET assigned_to = ?, updated_at = ? WHERE id = ?`),
			models.UserID(v), time.Now(), t.ID,
		)
		return err
	case "set_department":
		_, err := r.DB.Exec(
			r.query(`UPDATE escalated_tickets SET department_id = ?, updated_at = ? WHERE id = ?`),
			toInt(a.Value), time.Now(), t.ID,
		)
		return err
	case "set_type":
		_, err := r.DB.Exec(
			r.query(`UPDATE escalated_tickets SET ticket_type = ?, updated_at = ? WHERE id = ?`),
			a.Value, time.Now(), t.ID,
		)
		return err
	case "add_tag":
		return r.tagAction(t.ID, a.Value, true)
	case "remove_tag":
		return r.tagAction(t.ID, a.Value, false)
	case "add_note", "add_internal_note":
		_, err := r.DB.Exec(
			r.query(`INSERT INTO escalated_replies (ticket_id, body, is_internal, is_system, created_at, updated_at)
			 VALUES (?, ?, TRUE, TRUE, ?, ?)`),
			t.ID, InterpolateVariables(a.Value, td), time.Now(), time.Now(),
		)
		return err
	case "insert_canned_reply":
		// The reply body, resolved by the builder from the canned response; a
		// public reply, as Laravel's WorkflowEngine sends it.
		_, err := r.DB.Exec(
			r.query(`INSERT INTO escalated_replies (ticket_id, body, is_internal, is_system, created_at, updated_at)
			 VALUES (?, ?, FALSE, FALSE, ?, ?)`),
			t.ID, InterpolateVariables(a.Value, td), time.Now(), time.Now(),
		)
		return err
	case "add_follower":
		v := strings.TrimSpace(a.Value)
		if v == "" || v == "0" {
			return nil
		}
		_, err := r.DB.Exec(r.query("INSERT INTO escalated_ticket_followers (ticket_id,user_id) VALUES (?,?) ON CONFLICT DO NOTHING"), t.ID, models.UserID(v))
		return err
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
	err := r.DB.QueryRow(r.query(`SELECT id FROM escalated_tags WHERE name = ?`), name).Scan(&tagID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Tickets != nil {
		if add {
			return r.Tickets.AddTag(context.Background(), ticketID, tagID, nil)
		}
		return r.Tickets.RemoveTag(context.Background(), ticketID, tagID, nil)
	}
	if add {
		_, err = r.DB.Exec(
			r.query(`INSERT INTO escalated_ticket_tags (ticket_id, tag_id) VALUES (?, ?) ON CONFLICT DO NOTHING`),
			ticketID, tagID,
		)
		return err
	}
	_, err = r.DB.Exec(
		r.query(`DELETE FROM escalated_ticket_tags WHERE ticket_id = ? AND tag_id = ?`),
		ticketID, tagID,
	)
	return err
}

// writeLog records one escalated_workflow_logs row for a run. status is one of
// "skipped" (conditions did not match), "success", or "failed". actionsRaw is
// the workflow's action list when matched, nil (→ "[]") when skipped.
func (r *WorkflowRunner) writeLog(workflowID, ticketID int64, event, status string, actionsRaw models.JSONText, errMsg string) {
	actions := "[]"
	if len(actionsRaw) > 0 {
		actions = string(actionsRaw)
	}
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	if _, err := r.DB.Exec(
		r.query(`INSERT INTO escalated_workflow_logs (workflow_id, ticket_id, trigger_event, status, actions_executed, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`),
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
	if t.AccountType != nil {
		td.AccountType = *t.AccountType
	}
	if t.AccountID != nil {
		td.AccountID = *t.AccountID
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
	if !t.CreatedAt.IsZero() {
		td.HoursSinceCreated = time.Since(t.CreatedAt).Hours()
	}
	if !t.UpdatedAt.IsZero() {
		td.HoursSinceUpdated = time.Since(t.UpdatedAt).Hours()
	}
	return td
}

// tagNames lists the ticket's tag names, for "tags" conditions.
func (r *WorkflowRunner) tagNames(ticketID int64) []string {
	rows, err := r.DB.Query(r.query(`SELECT t.name FROM escalated_tags t
		JOIN escalated_ticket_tags tt ON tt.tag_id = t.id WHERE tt.ticket_id = ? ORDER BY t.name`), ticketID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			names = append(names, name)
		}
	}
	return names
}

func parseWorkflowConditions(raw models.JSONText) WorkflowConditionGroup {
	var g WorkflowConditionGroup
	if len(raw) == 0 {
		return g
	}
	if json.Unmarshal(raw, &g) != nil {
		// A flat list of conditions, as some older workflows were stored, is
		// treated as all (workflow admin contract).
		var list []WorkflowCondition
		if json.Unmarshal(raw, &list) == nil {
			return WorkflowConditionGroup{All: list}
		}
		return WorkflowConditionGroup{All: []WorkflowCondition{{Field: "__invalid", Operator: "equals", Value: "__never"}}}
	}
	return g
}

func parseWorkflowActions(raw models.JSONText) []WorkflowAction {
	var a []WorkflowAction
	if len(raw) == 0 {
		return a
	}
	_ = json.Unmarshal(raw, &a)
	return a
}

func (r *WorkflowRunner) query(query string) string {
	if r.Access != nil {
		query = strings.ReplaceAll(query, "escalated_", r.Access.Prefix)
	}
	return sqldialect.Rebind(r.DB, query)
}
