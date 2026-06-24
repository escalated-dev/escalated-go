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

// TicketLinkHandler lists, creates, and removes typed links between
// tickets. Mirrors the Laravel TicketLinkController (self-link and
// either-direction duplicate guards). Direct-SQL, consistent with the
// escalation handler.
type TicketLinkHandler struct {
	DB *sql.DB
}

// NewTicketLinkHandler constructs the handler.
func NewTicketLinkHandler(db *sql.DB) *TicketLinkHandler {
	return &TicketLinkHandler{DB: db}
}

type linkRow struct {
	id       int64
	parent   int64
	child    int64
	linkType string
}

// List handles GET /tickets/{id}/links — links in both directions, each
// with the linked ticket's summary and the direction relative to {id}.
func (h *TicketLinkHandler) List(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, parent_ticket_id, child_ticket_id, link_type
		   FROM escalated_ticket_links
		  WHERE parent_ticket_id = ? OR child_ticket_id = ?
		  ORDER BY id`, id, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var links []linkRow
	var otherIDs []int64
	for rows.Next() {
		var l linkRow
		if err := rows.Scan(&l.id, &l.parent, &l.child, &l.linkType); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		links = append(links, l)
		if l.parent == id {
			otherIDs = append(otherIDs, l.child)
		} else {
			otherIDs = append(otherIDs, l.parent)
		}
	}

	summaries, err := h.ticketSummaries(r.Context(), otherIDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		direction, other := "child", l.parent
		if l.parent == id {
			direction, other = "parent", l.child
		}
		out = append(out, map[string]any{
			"id":        l.id,
			"link_type": l.linkType,
			"direction": direction,
			"ticket":    summaries[other],
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

func (h *TicketLinkHandler) ticketSummaries(ctx context.Context, ids []int64) (map[int64]map[string]any, error) {
	out := map[int64]map[string]any{}
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := h.DB.QueryContext(ctx,
		fmt.Sprintf(`SELECT id, reference, subject, status FROM escalated_tickets WHERE id IN (%s)`, placeholders),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var reference, subject string
		var status int
		if err := rows.Scan(&id, &reference, &subject, &status); err != nil {
			return nil, err
		}
		out[id] = map[string]any{"id": id, "reference": reference, "subject": subject, "status": status}
	}
	return out, rows.Err()
}

// Create handles POST /tickets/{id}/links — body {target_reference, link_type}.
func (h *TicketLinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		TargetReference string `json:"target_reference"`
		LinkType        string `json:"link_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !models.ValidLinkType(in.LinkType) {
		http.Error(w, "invalid link_type", http.StatusUnprocessableEntity)
		return
	}

	var targetID int64
	err = h.DB.QueryRowContext(r.Context(),
		`SELECT id FROM escalated_tickets WHERE reference = ?`, in.TargetReference).Scan(&targetID)
	if err == sql.ErrNoRows {
		http.Error(w, "target ticket not found", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if targetID == id {
		http.Error(w, "cannot link a ticket to itself", http.StatusUnprocessableEntity)
		return
	}

	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(1) FROM escalated_ticket_links
		  WHERE link_type = ?
		    AND ((parent_ticket_id = ? AND child_ticket_id = ?)
		      OR (parent_ticket_id = ? AND child_ticket_id = ?))`,
		in.LinkType, id, targetID, targetID, id).Scan(&existing); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing > 0 {
		http.Error(w, "these tickets are already linked", http.StatusUnprocessableEntity)
		return
	}

	now := time.Now()
	if _, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO escalated_ticket_links (parent_ticket_id, child_ticket_id, link_type, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, targetID, in.LinkType, now, now); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// Delete handles DELETE /tickets/{id}/links/{linkId}.
func (h *TicketLinkHandler) Delete(w http.ResponseWriter, r *http.Request) {
	linkID, err := idFromPathName(r, "linkId")
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM escalated_ticket_links WHERE id = ?`, linkID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
