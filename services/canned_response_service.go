package services

import (
	"database/sql"
	"log"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// CannedResponseService manages agent-reusable reply templates.
//
// Visibility mirrors MacroService: shared responses are visible to every
// agent, private ones only to their creator. See models.CannedResponse.
type CannedResponseService struct {
	DB     *sql.DB
	Logger *log.Logger
}

// NewCannedResponseService constructs a service with the given DB and a
// logger (a default logger is used if nil).
func NewCannedResponseService(db *sql.DB, logger *log.Logger) *CannedResponseService {
	if logger == nil {
		logger = log.Default()
	}
	return &CannedResponseService{DB: db, Logger: logger}
}

// cannedRowScanner is satisfied by both *sql.Row and *sql.Rows.
type cannedRowScanner interface {
	Scan(dest ...any) error
}

// scanCannedResponse reads one row into a CannedResponse. The nullable
// category and created_by columns are scanned through sql.NullString so a
// SQL NULL maps to a nil pointer rather than an empty value.
func scanCannedResponse(sc cannedRowScanner) (models.CannedResponse, error) {
	var c models.CannedResponse
	var category, createdBy sql.NullString
	if err := sc.Scan(
		&c.ID, &c.Title, &c.Body, &category, &c.IsShared,
		&createdBy, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return c, err
	}
	if category.Valid {
		v := category.String
		c.Category = &v
	}
	if createdBy.Valid {
		uid := models.UserID(createdBy.String)
		c.CreatedBy = &uid
	}
	return c, nil
}

// ListAll returns every canned response, ordered by title. Used by the
// admin surface, which is not visibility-scoped.
func (s *CannedResponseService) ListAll() ([]models.CannedResponse, error) {
	rows, err := s.DB.Query(
		`SELECT id, title, body, category, is_shared, created_by, created_at, updated_at
		   FROM escalated_canned_responses
		  ORDER BY title ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var responses []models.CannedResponse
	for rows.Next() {
		c, err := scanCannedResponse(rows)
		if err != nil {
			return nil, err
		}
		responses = append(responses, c)
	}
	return responses, rows.Err()
}

// ListForAgent returns responses visible to the given agent: shared
// responses plus responses they created themselves.
func (s *CannedResponseService) ListForAgent(agentID models.UserID) ([]models.CannedResponse, error) {
	rows, err := s.DB.Query(
		`SELECT id, title, body, category, is_shared, created_by, created_at, updated_at
		   FROM escalated_canned_responses
		  WHERE is_shared = TRUE OR created_by = ?
		  ORDER BY title ASC`,
		agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var responses []models.CannedResponse
	for rows.Next() {
		c, err := scanCannedResponse(rows)
		if err != nil {
			return nil, err
		}
		responses = append(responses, c)
	}
	return responses, rows.Err()
}

// FindByID returns the response with the given id or sql.ErrNoRows.
func (s *CannedResponseService) FindByID(id int64) (*models.CannedResponse, error) {
	c, err := scanCannedResponse(s.DB.QueryRow(
		`SELECT id, title, body, category, is_shared, created_by, created_at, updated_at
		   FROM escalated_canned_responses
		  WHERE id = ?`,
		id,
	))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Create inserts a new canned response and returns it with its assigned ID.
func (s *CannedResponseService) Create(c *models.CannedResponse) error {
	now := time.Now()
	res, err := s.DB.Exec(
		`INSERT INTO escalated_canned_responses (title, body, category, is_shared, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Title, c.Body, c.Category, c.IsShared, c.CreatedBy, now, now,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = id
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

// Update saves changes to the given canned response.
func (s *CannedResponseService) Update(c *models.CannedResponse) error {
	_, err := s.DB.Exec(
		`UPDATE escalated_canned_responses
		    SET title = ?, body = ?, category = ?, is_shared = ?, updated_at = ?
		  WHERE id = ?`,
		c.Title, c.Body, c.Category, c.IsShared, time.Now(), c.ID,
	)
	return err
}

// Delete removes the canned response with the given id.
func (s *CannedResponseService) Delete(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM escalated_canned_responses WHERE id = ?`, id)
	return err
}
