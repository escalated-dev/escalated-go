package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// --- Chat Sessions ---

func (s *SQLiteStore) CreateChatSession(ctx context.Context, cs *models.ChatSession) error {
	q := fmt.Sprintf(`INSERT INTO %s (ticket_id, status, agent_id, visitor_user_agent, visitor_ip, visitor_page_url, agent_joined_at, last_activity_at, ended_at, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, s.t("chat_sessions"))
	cs.CreatedAt = time.Now()
	res, err := s.db.ExecContext(ctx, q,
		cs.TicketID, cs.Status, cs.AgentID, cs.VisitorUserAgent, cs.VisitorIP, cs.VisitorPageURL,
		cs.AgentJoinedAt, cs.LastActivityAt, cs.EndedAt, cs.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	cs.ID = id
	return nil
}

func (s *SQLiteStore) GetChatSession(ctx context.Context, id int64) (*models.ChatSession, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, status, agent_id, visitor_user_agent, visitor_ip, visitor_page_url, agent_joined_at, last_activity_at, ended_at, created_at FROM %s WHERE id = ?`, s.t("chat_sessions"))
	cs := &models.ChatSession{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&cs.ID, &cs.TicketID, &cs.Status, &cs.AgentID, &cs.VisitorUserAgent, &cs.VisitorIP, &cs.VisitorPageURL,
		&cs.AgentJoinedAt, &cs.LastActivityAt, &cs.EndedAt, &cs.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return cs, err
}

func (s *SQLiteStore) GetChatSessionByTicket(ctx context.Context, ticketID int64) (*models.ChatSession, error) {
	q := fmt.Sprintf(`SELECT id, ticket_id, status, agent_id, visitor_user_agent, visitor_ip, visitor_page_url, agent_joined_at, last_activity_at, ended_at, created_at FROM %s WHERE ticket_id = ? ORDER BY created_at DESC LIMIT 1`, s.t("chat_sessions"))
	cs := &models.ChatSession{}
	err := s.db.QueryRowContext(ctx, q, ticketID).Scan(
		&cs.ID, &cs.TicketID, &cs.Status, &cs.AgentID, &cs.VisitorUserAgent, &cs.VisitorIP, &cs.VisitorPageURL,
		&cs.AgentJoinedAt, &cs.LastActivityAt, &cs.EndedAt, &cs.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return cs, err
}

func (s *SQLiteStore) UpdateChatSession(ctx context.Context, cs *models.ChatSession) error {
	q := fmt.Sprintf(`UPDATE %s SET status=?, agent_id=?, agent_joined_at=?, last_activity_at=?, ended_at=? WHERE id=?`, s.t("chat_sessions"))
	_, err := s.db.ExecContext(ctx, q, cs.Status, cs.AgentID, cs.AgentJoinedAt, cs.LastActivityAt, cs.EndedAt, cs.ID)
	return err
}

func (s *SQLiteStore) ListChatSessions(ctx context.Context, f models.ChatSessionFilters) ([]*models.ChatSession, error) {
	var where []string
	var args []any

	if f.Active {
		where = append(where, "status IN (?, ?)")
		args = append(args, models.ChatStatusWaiting, models.ChatStatusActive)
	}
	if f.Status != nil {
		where = append(where, "status = ?")
		args = append(args, *f.Status)
	}
	if f.AgentID != nil {
		where = append(where, "agent_id = ?")
		args = append(args, *f.AgentID)
	}

	q := fmt.Sprintf(`SELECT id, ticket_id, status, agent_id, visitor_user_agent, visitor_ip, visitor_page_url, agent_joined_at, last_activity_at, ended_at, created_at FROM %s`, s.t("chat_sessions"))
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at ASC"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*models.ChatSession
	for rows.Next() {
		cs := &models.ChatSession{}
		if err := rows.Scan(&cs.ID, &cs.TicketID, &cs.Status, &cs.AgentID, &cs.VisitorUserAgent, &cs.VisitorIP, &cs.VisitorPageURL, &cs.AgentJoinedAt, &cs.LastActivityAt, &cs.EndedAt, &cs.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, cs)
	}
	return result, rows.Err()
}

// --- Chat Routing Rules ---

func (s *SQLiteStore) CreateChatRoutingRule(ctx context.Context, r *models.ChatRoutingRule) error {
	q := fmt.Sprintf(`INSERT INTO %s (name, strategy, department_id, priority, max_concurrent_chats, is_active, created_at) VALUES (?,?,?,?,?,?,?)`, s.t("chat_routing_rules"))
	r.CreatedAt = time.Now()
	res, err := s.db.ExecContext(ctx, q,
		r.Name, r.Strategy, r.DepartmentID, r.Priority, r.MaxConcurrentChats, r.IsActive, r.CreatedAt,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	r.ID = id
	return nil
}

func (s *SQLiteStore) GetChatRoutingRule(ctx context.Context, id int64) (*models.ChatRoutingRule, error) {
	q := fmt.Sprintf(`SELECT id, name, strategy, department_id, priority, max_concurrent_chats, is_active, created_at FROM %s WHERE id = ?`, s.t("chat_routing_rules"))
	r := &models.ChatRoutingRule{}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&r.ID, &r.Name, &r.Strategy, &r.DepartmentID, &r.Priority, &r.MaxConcurrentChats, &r.IsActive, &r.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (s *SQLiteStore) ListActiveChatRoutingRules(ctx context.Context, _ *int64) ([]*models.ChatRoutingRule, error) {
	q := fmt.Sprintf(`SELECT id, name, strategy, department_id, priority, max_concurrent_chats, is_active, created_at FROM %s WHERE is_active = 1 ORDER BY priority DESC`, s.t("chat_routing_rules"))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*models.ChatRoutingRule
	for rows.Next() {
		r := &models.ChatRoutingRule{}
		if err := rows.Scan(&r.ID, &r.Name, &r.Strategy, &r.DepartmentID, &r.Priority, &r.MaxConcurrentChats, &r.IsActive, &r.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) UpdateChatRoutingRule(ctx context.Context, r *models.ChatRoutingRule) error {
	q := fmt.Sprintf(`UPDATE %s SET name=?, strategy=?, department_id=?, priority=?, max_concurrent_chats=?, is_active=? WHERE id=?`, s.t("chat_routing_rules"))
	_, err := s.db.ExecContext(ctx, q, r.Name, r.Strategy, r.DepartmentID, r.Priority, r.MaxConcurrentChats, r.IsActive, r.ID)
	return err
}

func (s *SQLiteStore) DeleteChatRoutingRule(ctx context.Context, id int64) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.t("chat_routing_rules"))
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}

func (s *SQLiteStore) CountActiveChatsForAgent(ctx context.Context, agentID int64) (int, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE agent_id = ? AND status = ?`, s.t("chat_sessions"))
	var count int
	err := s.db.QueryRowContext(ctx, q, agentID, models.ChatStatusActive).Scan(&count)
	return count, err
}
