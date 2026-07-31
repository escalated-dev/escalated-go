package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// WebhookHandler exposes admin CRUD over outbound Webhook subscriptions plus a
// per-delivery retry trigger.
//
// Mirrors the Laravel reference (Admin\WebhookController). Like the sibling
// engine handlers (AutomationHandler, MacroHandler) it talks to *sql.DB
// directly to stay close to the WebhookDispatcher's surface.
type WebhookHandler struct {
	DB         *sql.DB
	Dispatcher *services.WebhookDispatcher
}

// NewWebhookHandler constructs the handler. Pass the same *sql.DB the
// dispatcher uses; a dispatcher is created when nil.
func NewWebhookHandler(db *sql.DB, dispatcher *services.WebhookDispatcher) *WebhookHandler {
	if dispatcher == nil {
		dispatcher = services.NewWebhookDispatcher(db, nil)
	}
	return &WebhookHandler{DB: db, Dispatcher: dispatcher}
}

// List handles GET /admin/webhooks.
func (h *WebhookHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, url, events, secret, active, created_at, updated_at
		   FROM escalated_webhooks
		  ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.Webhook{}
	for rows.Next() {
		var wh models.Webhook
		var secret sql.NullString
		if err := rows.Scan(
			&wh.ID, &wh.URL, &wh.Events, &secret, &wh.Active, &wh.CreatedAt, &wh.UpdatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if secret.Valid {
			wh.Secret = &secret.String
		}
		out = append(out, wh)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"webhooks":         out,
		"available_events": services.SupportedWebhookEvents,
	})
}

// Create handles POST /admin/webhooks.
func (h *WebhookHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string          `json:"url"`
		Events json.RawMessage `json:"events"`
		Secret *string         `json:"secret"`
		Active *bool           `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if len(in.Events) == 0 {
		http.Error(w, "events is required", http.StatusBadRequest)
		return
	}

	events := defaultJSONArray(in.Events)
	active := true
	if in.Active != nil {
		active = *in.Active
	}

	res, err := h.DB.ExecContext(
		r.Context(),
		`INSERT INTO escalated_webhooks (url, events, secret, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		in.URL, events, in.Secret, active, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// Update handles PATCH /admin/webhooks/{id}.
func (h *WebhookHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		URL    *string         `json:"url"`
		Events json.RawMessage `json:"events"`
		Secret *string         `json:"secret"`
		Active *bool           `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Build a dynamic UPDATE so omitted fields keep their existing values.
	sets := []string{}
	args := []any{}

	if in.URL != nil {
		sets = append(sets, "url = ?")
		args = append(args, *in.URL)
	}
	if len(in.Events) > 0 {
		sets = append(sets, "events = ?")
		args = append(args, []byte(in.Events))
	}
	if in.Secret != nil {
		sets = append(sets, "secret = ?")
		args = append(args, *in.Secret)
	}
	if in.Active != nil {
		sets = append(sets, "active = ?")
		args = append(args, *in.Active)
	}

	if len(sets) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_webhooks SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := h.DB.ExecContext(r.Context(), q, args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/webhooks/{id}. Its deliveries are removed too.
func (h *WebhookHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.ExecContext(
		r.Context(), `DELETE FROM escalated_webhook_deliveries WHERE webhook_id = ?`, id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := h.DB.ExecContext(
		r.Context(), `DELETE FROM escalated_webhooks WHERE id = ?`, id,
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Deliveries handles GET /admin/webhooks/{id}/deliveries — the delivery log.
func (h *WebhookHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	rows, err := h.DB.QueryContext(
		r.Context(),
		`SELECT id, webhook_id, event, payload, response_code, response_body, attempts, delivered_at, created_at, updated_at
		   FROM escalated_webhook_deliveries
		  WHERE webhook_id = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT 100`,
		id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := []models.WebhookDelivery{}
	for rows.Next() {
		var dlv models.WebhookDelivery
		var payload sql.NullString
		var respCode sql.NullInt64
		var respBody sql.NullString
		var deliveredAt sql.NullTime
		if err := rows.Scan(
			&dlv.ID, &dlv.WebhookID, &dlv.Event, &payload, &respCode, &respBody,
			&dlv.Attempts, &deliveredAt, &dlv.CreatedAt, &dlv.UpdatedAt,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if payload.Valid {
			dlv.Payload = json.RawMessage(payload.String)
		}
		if respCode.Valid {
			c := int(respCode.Int64)
			dlv.ResponseCode = &c
		}
		if respBody.Valid {
			dlv.ResponseBody = &respBody.String
		}
		if deliveredAt.Valid {
			dlv.DeliveredAt = &deliveredAt.Time
		}
		out = append(out, dlv)
	}

	writeJSON(w, http.StatusOK, map[string]any{"deliveries": out})
}

// Retry handles POST /admin/webhooks/deliveries/{id}/retry.
func (h *WebhookHandler) Retry(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var dlv models.WebhookDelivery
	var payload sql.NullString
	err = h.DB.QueryRowContext(
		r.Context(),
		`SELECT id, webhook_id, event, payload FROM escalated_webhook_deliveries WHERE id = ?`,
		id,
	).Scan(&dlv.ID, &dlv.WebhookID, &dlv.Event, &payload)
	if err == sql.ErrNoRows {
		http.Error(w, "delivery not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if payload.Valid {
		dlv.Payload = json.RawMessage(payload.String)
	}

	// Re-send off the request path so a slow endpoint never blocks the admin.
	go h.Dispatcher.RetryDelivery(dlv)

	writeJSON(w, http.StatusAccepted, map[string]any{"retried": dlv.ID})
}
