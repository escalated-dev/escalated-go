package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// SideConversationHandler manages per-ticket side conversations: list,
// create (with an initial reply), append replies, and close. Mirrors the
// Laravel SideConversationController. Direct-SQL.
type SideConversationHandler struct {
	DB *sql.DB
}

// NewSideConversationHandler constructs the handler.
func NewSideConversationHandler(db *sql.DB) *SideConversationHandler {
	return &SideConversationHandler{DB: db}
}

// List handles GET /tickets/{id}/side-conversations — conversations newest
// first, each with its replies oldest first.
func (h *SideConversationHandler) List(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, ticket_id, subject, channel, status, created_by, created_at, updated_at
		   FROM escalated_side_conversations WHERE ticket_id = ? ORDER BY id DESC`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	convs := []models.SideConversation{}
	var ids []int64
	for rows.Next() {
		var c models.SideConversation
		var createdBy sql.NullString
		if err := rows.Scan(&c.ID, &c.TicketID, &c.Subject, &c.Channel, &c.Status, &createdBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if createdBy.Valid {
			uid := models.UserID(createdBy.String)
			c.CreatedBy = &uid
		}
		c.Replies = []models.SideConversationReply{}
		convs = append(convs, c)
		ids = append(ids, c.ID)
	}

	repliesByConv, err := h.repliesByConversation(r.Context(), ids)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range convs {
		if rs, ok := repliesByConv[convs[i].ID]; ok {
			convs[i].Replies = rs
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"conversations": convs})
}

func (h *SideConversationHandler) repliesByConversation(ctx context.Context, ids []int64) (map[int64][]models.SideConversationReply, error) {
	out := map[int64][]models.SideConversationReply{}
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := h.DB.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, side_conversation_id, body, author_id, created_at, updated_at
		   FROM escalated_side_conversation_replies WHERE side_conversation_id IN (%s) ORDER BY id ASC`, placeholders),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var rep models.SideConversationReply
		var authorID sql.NullString
		if err := rows.Scan(&rep.ID, &rep.SideConversationID, &rep.Body, &authorID, &rep.CreatedAt, &rep.UpdatedAt); err != nil {
			return nil, err
		}
		if authorID.Valid {
			uid := models.UserID(authorID.String)
			rep.AuthorID = &uid
		}
		out[rep.SideConversationID] = append(out[rep.SideConversationID], rep)
	}
	return out, rows.Err()
}

// Create handles POST /tickets/{id}/side-conversations — body
// {subject, channel, body}. Opens the thread with an initial reply.
func (h *SideConversationHandler) Create(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Subject string `json:"subject"`
		Channel string `json:"channel"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if in.Subject == "" || in.Body == "" || !models.ValidSideConversationChannel(in.Channel) {
		http.Error(w, "subject, body, and a valid channel (internal|email) are required", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now()
	res, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO escalated_side_conversations (ticket_id, subject, channel, status, created_at, updated_at)
		 VALUES (?, ?, ?, 'open', ?, ?)`,
		id, in.Subject, in.Channel, now, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	scID, _ := res.LastInsertId()

	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO escalated_side_conversation_replies (side_conversation_id, body, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		scID, in.Body, now, now); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": scID})
}

// Reply handles POST /tickets/{id}/side-conversations/{scId}/reply.
func (h *SideConversationHandler) Reply(w http.ResponseWriter, r *http.Request) {
	scID, err := idFromPathName(r, "scId")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Body == "" {
		http.Error(w, "body is required", http.StatusBadRequest)
		return
	}

	now := time.Now()
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO escalated_side_conversation_replies (side_conversation_id, body, created_at, updated_at)
		 VALUES (?, ?, ?, ?)`,
		scID, in.Body, now, now); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// Close handles POST /tickets/{id}/side-conversations/{scId}/close.
func (h *SideConversationHandler) Close(w http.ResponseWriter, r *http.Request) {
	scID, err := idFromPathName(r, "scId")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE escalated_side_conversations SET status = 'closed', updated_at = ? WHERE id = ?`,
		time.Now(), scID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
