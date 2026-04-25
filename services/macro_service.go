package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// macroToInt and macroToString are helpers identical in shape to
// automation_runner.go's toInt/toString. They are duplicated under
// distinct names so the two services compile independently when their
// feature branches land separately. Once both have merged, the helpers
// can be deduplicated into a shared file.
func macroToInt(v interface{}) int {
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

func macroToString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// MacroService manages and applies agent macros.
//
// Distinct from Workflow (admin event-driven) and Automation (admin
// time-based). See escalated-developer-context/domain-model/
// workflows-automations-macros.md.
type MacroService struct {
	DB     *sql.DB
	Logger *log.Logger
}

// NewMacroService constructs a service with the given DB and a logger
// (a default logger is used if nil).
func NewMacroService(db *sql.DB, logger *log.Logger) *MacroService {
	if logger == nil {
		logger = log.Default()
	}
	return &MacroService{DB: db, Logger: logger}
}

// ListForAgent returns macros visible to the given agent: shared macros
// plus macros they created themselves.
func (s *MacroService) ListForAgent(agentID int64) ([]models.Macro, error) {
	rows, err := s.DB.Query(
		`SELECT id, name, description, actions, is_shared, created_by, created_at, updated_at
		   FROM escalated_macros
		  WHERE is_shared = TRUE OR created_by = ?
		  ORDER BY name ASC`,
		agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var macros []models.Macro
	for rows.Next() {
		var m models.Macro
		var createdBy sql.NullInt64
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Description, &m.Actions, &m.IsShared,
			&createdBy, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			m.CreatedBy = &createdBy.Int64
		}
		macros = append(macros, m)
	}
	return macros, rows.Err()
}

// FindByID returns the macro with the given id or sql.ErrNoRows.
func (s *MacroService) FindByID(id int64) (*models.Macro, error) {
	var m models.Macro
	var createdBy sql.NullInt64
	err := s.DB.QueryRow(
		`SELECT id, name, description, actions, is_shared, created_by, created_at, updated_at
		   FROM escalated_macros
		  WHERE id = ?`,
		id,
	).Scan(
		&m.ID, &m.Name, &m.Description, &m.Actions, &m.IsShared,
		&createdBy, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if createdBy.Valid {
		m.CreatedBy = &createdBy.Int64
	}
	return &m, nil
}

// Create inserts a new macro and returns it with its assigned ID.
func (s *MacroService) Create(m *models.Macro) error {
	now := time.Now()
	res, err := s.DB.Exec(
		`INSERT INTO escalated_macros (name, description, actions, is_shared, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Description, m.Actions, m.IsShared, m.CreatedBy, now, now,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	m.ID = id
	m.CreatedAt = now
	m.UpdatedAt = now
	return nil
}

// Update saves changes to the given macro.
func (s *MacroService) Update(m *models.Macro) error {
	_, err := s.DB.Exec(
		`UPDATE escalated_macros
		    SET name = ?, description = ?, actions = ?, is_shared = ?, updated_at = ?
		  WHERE id = ?`,
		m.Name, m.Description, m.Actions, m.IsShared, time.Now(), m.ID,
	)
	return err
}

// Delete removes the macro with the given id.
func (s *MacroService) Delete(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM escalated_macros WHERE id = ?`, id)
	return err
}

// Apply executes each action in the macro against the given ticket id,
// authored by the given agent. Per-action failures are logged and do
// not abort the rest of the bundle.
func (s *MacroService) Apply(macro *models.Macro, ticketID int64, agentID int64) error {
	var actions []models.MacroAction
	if len(macro.Actions) > 0 {
		if err := json.Unmarshal(macro.Actions, &actions); err != nil {
			return fmt.Errorf("parse actions: %w", err)
		}
	}

	for _, action := range actions {
		if err := s.runAction(action, ticketID, agentID); err != nil {
			s.Logger.Printf("macro #%d action %s on ticket #%d (agent %d) failed: %v",
				macro.ID, action.Type, ticketID, agentID, err)
		}
	}
	return nil
}

func (s *MacroService) runAction(action models.MacroAction, ticketID int64, agentID int64) error {
	switch action.Type {
	case "change_status", "set_status":
		_, err := s.DB.Exec(
			`UPDATE escalated_tickets SET status = ?, updated_at = ? WHERE id = ?`,
			macroToInt(action.Value), time.Now(), ticketID,
		)
		return err
	case "change_priority", "set_priority":
		_, err := s.DB.Exec(
			`UPDATE escalated_tickets SET priority = ?, updated_at = ? WHERE id = ?`,
			macroToInt(action.Value), time.Now(), ticketID,
		)
		return err
	case "assign":
		_, err := s.DB.Exec(
			`UPDATE escalated_tickets SET assigned_to = ?, updated_at = ? WHERE id = ?`,
			macroToInt(action.Value), time.Now(), ticketID,
		)
		return err
	case "add_tag":
		var tagID int64
		err := s.DB.QueryRow(
			`SELECT id FROM escalated_tags WHERE name = ?`,
			macroToString(action.Value),
		).Scan(&tagID)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = s.DB.Exec(
			`INSERT OR IGNORE INTO escalated_ticket_tags (ticket_id, tag_id) VALUES (?, ?)`,
			ticketID, tagID,
		)
		return err
	case "add_reply":
		_, err := s.DB.Exec(
			`INSERT INTO escalated_replies (ticket_id, author_id, body, is_internal_note, created_at, updated_at)
			 VALUES (?, ?, ?, FALSE, ?, ?)`,
			ticketID, agentID, macroToString(action.Value), time.Now(), time.Now(),
		)
		return err
	case "add_note":
		_, err := s.DB.Exec(
			`INSERT INTO escalated_replies (ticket_id, author_id, body, is_internal_note, created_at, updated_at)
			 VALUES (?, ?, ?, TRUE, ?, ?)`,
			ticketID, agentID, macroToString(action.Value), time.Now(), time.Now(),
		)
		return err
	case "insert_canned_reply":
		// Frontend resolves the canned response template before POSTing;
		// stored value is the resolved text body.
		_, err := s.DB.Exec(
			`INSERT INTO escalated_replies (ticket_id, author_id, body, is_internal_note, created_at, updated_at)
			 VALUES (?, ?, ?, FALSE, ?, ?)`,
			ticketID, agentID, macroToString(action.Value), time.Now(), time.Now(),
		)
		return err
	}
	// Unknown action type — skip silently for forward-compat.
	return nil
}
