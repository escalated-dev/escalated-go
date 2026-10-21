package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
)

// AuditLogHandler serves the admin audit log page (Laravel
// AuditLogController): GET /admin/audit-log with the user_id, action,
// auditable_type, date_from and date_to filters, 50 entries a page.
type AuditLogHandler struct {
	Store    *services.SQLAuditLogStore
	Renderer renderer.Renderer
	// Users, when set, names the people in the log and fills the user filter.
	Users UserDirectory
}

// auditLogView is one entry as the shared AuditLogEntry component reads it.
type auditLogView struct {
	ID            int64           `json:"id"`
	Action        string          `json:"action"`
	AuditableType string          `json:"auditable_type"`
	AuditableID   *int64          `json:"auditable_id"`
	UserID        *models.UserID  `json:"user_id"`
	User          map[string]any  `json:"user"`
	OldValues     json.RawMessage `json:"old_values"`
	NewValues     json.RawMessage `json:"new_values"`
	IPAddress     *string         `json:"ip_address"`
	UserAgent     *string         `json:"user_agent"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Index handles GET /admin/audit-log.
func (h *AuditLogHandler) Index(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	f := services.AuditLogFilter{
		PerformerID: q.Get("user_id"),
		Action:      q.Get("action"),
		EntityType:  q.Get("auditable_type"),
		Page:        page,
		PerPage:     50,
	}
	if d, err := time.ParseInLocation("2006-01-02", q.Get("date_from"), time.Local); err == nil {
		f.From = &d
	}
	if d, err := time.ParseInLocation("2006-01-02", q.Get("date_to"), time.Local); err == nil {
		end := d.AddDate(0, 0, 1)
		f.To = &end
	}

	logs, total, err := h.Store.List(r.Context(), f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Could not load the audit log."})
		return
	}
	types, _ := h.Store.EntityTypes(r.Context())

	names := map[models.UserID]string{}
	name := func(id models.UserID) map[string]any {
		if h.Users == nil {
			return nil
		}
		if n, ok := names[id]; ok {
			if n == "" {
				return nil
			}
			return map[string]any{"id": id, "name": n}
		}
		names[id] = ""
		if n, err := strconv.ParseInt(string(id), 10, 64); err == nil {
			if u, err := h.Users.GetUser(r.Context(), n); err == nil && u != nil {
				names[id] = u.Name
				return map[string]any{"id": id, "name": u.Name}
			}
		}
		return nil
	}

	data := make([]auditLogView, 0, len(logs))
	for _, l := range logs {
		v := auditLogView{
			ID: l.ID, Action: l.Action, AuditableType: l.EntityType, AuditableID: l.EntityID,
			UserID: l.PerformerID, IPAddress: l.IPAddress, UserAgent: l.UserAgent, CreatedAt: l.CreatedAt,
			OldValues: rawOrNull(l.OldValues), NewValues: rawOrNull(l.NewValues),
		}
		if l.PerformerID != nil {
			v.User = name(*l.PerformerID)
		}
		data = append(data, v)
	}
	lastPage := (total + 49) / 50
	if lastPage < 1 {
		lastPage = 1
	}

	users := []map[string]any{}
	if h.Users != nil {
		if p, err := h.Users.ListUsers(r.Context(), "", 1, 20); err == nil {
			for _, u := range p.Data {
				users = append(users, map[string]any{"id": u.ID, "name": u.Name})
			}
		}
	}

	_ = h.Renderer.Render(w, r, "Escalated/Admin/AuditLog/Index", map[string]any{
		"logs": map[string]any{
			"data": data, "current_page": page, "last_page": lastPage, "per_page": 50, "total": total,
			"links": []any{},
		},
		"filters": map[string]string{
			"user_id": f.PerformerID, "action": f.Action, "auditable_type": f.EntityType,
			"date_from": q.Get("date_from"), "date_to": q.Get("date_to"),
		},
		"users":         users,
		"actions":       []string{"created", "updated", "deleted"},
		"resourceTypes": types,
	})
}

func rawOrNull(v models.JSONText) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(v)
}
