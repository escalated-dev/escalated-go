package services

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// CapacityService tracks per-agent concurrent-ticket capacity. Mirrors the
// Laravel CapacityService: capacities are lazily created (default ceiling
// 10) the first time an agent is referenced, then incremented/decremented
// as tickets are assigned and resolved.
type CapacityService struct {
	DB *sql.DB
}

// NewCapacityService constructs the service.
func NewCapacityService(db *sql.DB) *CapacityService {
	return &CapacityService{DB: db}
}

// CanAcceptTicket reports whether the agent can accept another ticket on
// the channel (lazily creating the capacity row).
func (s *CapacityService) CanAcceptTicket(userID models.UserID, channel string) (bool, error) {
	c, err := s.firstOrCreate(userID, channel)
	if err != nil {
		return false, err
	}
	return c.HasCapacity(), nil
}

// IncrementLoad raises the agent's current load by one.
func (s *CapacityService) IncrementLoad(userID models.UserID, channel string) error {
	c, err := s.firstOrCreate(userID, channel)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(
		`UPDATE escalated_agent_capacity SET current_count = current_count + 1, updated_at = ? WHERE id = ?`,
		time.Now(), c.ID,
	)
	return err
}

// DecrementLoad lowers the agent's current load by one (never below zero).
func (s *CapacityService) DecrementLoad(userID models.UserID, channel string) error {
	c, err := s.firstOrCreate(userID, channel)
	if err != nil {
		return err
	}
	if c.CurrentCount <= 0 {
		return nil
	}
	_, err = s.DB.Exec(
		`UPDATE escalated_agent_capacity SET current_count = current_count - 1, updated_at = ? WHERE id = ?`,
		time.Now(), c.ID,
	)
	return err
}

// UpdateMaxConcurrent sets the ceiling for a capacity row.
func (s *CapacityService) UpdateMaxConcurrent(id int64, maxConcurrent int) error {
	_, err := s.DB.Exec(
		`UPDATE escalated_agent_capacity SET max_concurrent = ?, updated_at = ? WHERE id = ?`,
		maxConcurrent, time.Now(), id,
	)
	return err
}

// AllCapacities returns every capacity row, ordered by agent then channel.
func (s *CapacityService) AllCapacities() ([]models.AgentCapacity, error) {
	rows, err := s.DB.Query(
		`SELECT id, user_id, channel, max_concurrent, current_count, created_at, updated_at
		   FROM escalated_agent_capacity
		  ORDER BY user_id ASC, channel ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("capacity: list: %w", err)
	}
	defer rows.Close()

	var out []models.AgentCapacity
	for rows.Next() {
		c, err := scanCapacity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *CapacityService) firstOrCreate(userID models.UserID, channel string) (models.AgentCapacity, error) {
	if channel == "" {
		channel = "default"
	}

	row := s.DB.QueryRow(
		`SELECT id, user_id, channel, max_concurrent, current_count, created_at, updated_at
		   FROM escalated_agent_capacity WHERE user_id = ? AND channel = ?`,
		string(userID), channel,
	)
	c, err := scanCapacity(row)
	if err == nil {
		return c, nil
	}
	if err != sql.ErrNoRows {
		return models.AgentCapacity{}, err
	}

	now := time.Now()
	res, err := s.DB.Exec(
		`INSERT INTO escalated_agent_capacity (user_id, channel, max_concurrent, current_count, created_at, updated_at)
		 VALUES (?, ?, 10, 0, ?, ?)`,
		string(userID), channel, now, now,
	)
	if err != nil {
		return models.AgentCapacity{}, err
	}
	id, _ := res.LastInsertId()
	return models.AgentCapacity{
		ID:            id,
		UserID:        userID,
		Channel:       channel,
		MaxConcurrent: 10,
		CurrentCount:  0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCapacity(r rowScanner) (models.AgentCapacity, error) {
	var c models.AgentCapacity
	var userID string
	if err := r.Scan(
		&c.ID, &userID, &c.Channel, &c.MaxConcurrent, &c.CurrentCount, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return models.AgentCapacity{}, err
	}
	c.UserID = models.UserID(userID)
	return c, nil
}
