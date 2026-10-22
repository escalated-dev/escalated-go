package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
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

	// Pages serves Escalated/Admin/Webhooks/Index, Form and DeliveryLog and
	// answers their writes the Inertia way. Set by the router.
	Pages PageSupport
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
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, url, events, secret, active, created_at, updated_at
		   FROM escalated_webhooks
		  ORDER BY created_at DESC, id DESC`),
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
		wh.Events = canonicalEventsJSON(wh.Events)
		out = append(out, wh)
	}

	if h.Pages.wantsPage(r) {
		h.renderIndex(w, r, out)
		return
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
	rerender := func(errs map[string]string) { h.renderForm(w, r, nil, errs) }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.URL == "" {
		h.Pages.fail(w, r, http.StatusBadRequest, "url is required", map[string]string{"url": requiredMessage("url")}, rerender)
		return
	}
	if len(in.Events) == 0 || !hasEvents(in.Events) {
		h.Pages.fail(w, r, http.StatusBadRequest, "events is required", map[string]string{"events": "Choose at least one event."}, rerender)
		return
	}
	if err := services.ValidateWebhookURL(r.Context(), in.URL); err != nil {
		h.Pages.fail(w, r, http.StatusUnprocessableEntity, err.Error(), map[string]string{"url": "The webhook URL must use http or https and resolve to a public address."}, rerender)
		return
	}
	if in.Secret != nil && *in.Secret == "" {
		in.Secret = nil
	}

	events := canonicalEventsJSON(defaultJSONArray(models.JSONText(in.Events)))
	active := true
	if in.Active != nil {
		active = *in.Active
	}

	res, err := ticketaccess.ExecInsertContext(r.Context(), h.DB, `INSERT INTO escalated_webhooks (url, events, secret, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		in.URL, events, in.Secret, active, time.Now(), time.Now(),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	h.Pages.succeed(w, r, h.Pages.path("/admin/webhooks"), http.StatusCreated, map[string]any{"id": id})
}

// Update handles PATCH /admin/webhooks/{id}.
func (h *WebhookHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		URL         *string         `json:"url"`
		Events      json.RawMessage `json:"events"`
		Secret      *string         `json:"secret"`
		ClearSecret bool            `json:"clear_secret"`
		Active      *bool           `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	// Build a dynamic UPDATE so omitted fields keep their existing values.
	sets := []string{}
	args := []any{}

	rerender := func(errs map[string]string) {
		wh, _ := h.find(r, id)
		h.renderForm(w, r, wh, errs)
	}
	if len(in.Events) > 0 && !hasEvents(in.Events) {
		h.Pages.fail(w, r, http.StatusBadRequest, "events is required", map[string]string{"events": "Choose at least one event."}, rerender)
		return
	}
	if in.URL != nil {
		if err := services.ValidateWebhookURL(r.Context(), *in.URL); err != nil {
			h.Pages.fail(w, r, http.StatusUnprocessableEntity, err.Error(), map[string]string{"url": "The webhook URL must use http or https and resolve to a public address."}, rerender)
			return
		}
		sets = append(sets, "url = ?")
		args = append(args, *in.URL)
	}
	if len(in.Events) > 0 {
		sets = append(sets, "events = ?")
		args = append(args, []byte(canonicalEventsJSON(models.JSONText(in.Events))))
	}
	// The form never receives the stored secret, so a blank or missing one
	// means "leave it as it is". clear_secret removes it.
	if in.ClearSecret {
		sets = append(sets, "secret = ?")
		args = append(args, nil)
	} else if in.Secret != nil && *in.Secret != "" {
		sets = append(sets, "secret = ?")
		args = append(args, *in.Secret)
	}
	if in.Active != nil {
		sets = append(sets, "active = ?")
		args = append(args, *in.Active)
	}

	if len(sets) == 0 {
		h.Pages.succeed(w, r, h.Pages.path("/admin/webhooks"), http.StatusOK, map[string]any{"id": id})
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now())
	args = append(args, id)

	q := "UPDATE escalated_webhooks SET " + joinSets(sets) + " WHERE id = ?"
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, q), args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/webhooks"), http.StatusOK, map[string]any{"id": id})
}

// Delete handles DELETE /admin/webhooks/{id}. Its deliveries are removed too.
func (h *WebhookHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `DELETE FROM escalated_webhook_deliveries WHERE webhook_id = ?`), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `DELETE FROM escalated_webhooks WHERE id = ?`), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.Pages.path("/admin/webhooks"), http.StatusNoContent, nil)
}

// Deliveries handles GET /admin/webhooks/{id}/deliveries — the delivery log.
func (h *WebhookHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if h.Pages.wantsPage(r) {
		h.renderDeliveryLog(w, r, id)
		return
	}

	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, webhook_id, event, payload, response_code, response_body, attempts, delivered_at, created_at, updated_at
		   FROM escalated_webhook_deliveries
		  WHERE webhook_id = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT 100`),
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
			dlv.Payload = models.JSONText(payload.String)
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
	err = ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, webhook_id, event, payload FROM escalated_webhook_deliveries WHERE id = ?`),
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
		dlv.Payload = models.JSONText(payload.String)
	}

	// Re-send off the request path so a slow endpoint never blocks the admin.
	go h.Dispatcher.RetryDelivery(dlv)

	if h.Pages.Renderer != nil && isInertia(r) {
		backTo(w, r, h.Pages.path("/admin/webhooks/"+strconv.FormatInt(dlv.WebhookID, 10)+"/deliveries"))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"retried": dlv.ID})
}

// canonicalEventsJSON rewrites a JSON array of event names to their canonical
// names (ADR 2026-10-21-event-vocabulary), so a legacy name posted by an older
// client is stored, and a stored one shown, as the event it means. Anything
// that is not an array of strings is returned unchanged.
func canonicalEventsJSON(raw models.JSONText) models.JSONText {
	var names []string
	if len(raw) == 0 || json.Unmarshal(raw, &names) != nil {
		return raw
	}
	out, err := json.Marshal(models.CanonicalEventNames(names))
	if err != nil {
		return raw
	}
	return models.JSONText(out)
}

// hasEvents reports whether raw is a non-empty JSON array.
func hasEvents(raw json.RawMessage) bool {
	var events []any
	return json.Unmarshal(raw, &events) == nil && len(events) > 0
}

// find loads one webhook, or nil.
func (h *WebhookHandler) find(r *http.Request, id int64) (*models.Webhook, error) {
	var wh models.Webhook
	var secret sql.NullString
	err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, url, events, secret, active, created_at, updated_at
		   FROM escalated_webhooks WHERE id = ?`), id).Scan(
		&wh.ID, &wh.URL, &wh.Events, &secret, &wh.Active, &wh.CreatedAt, &wh.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if secret.Valid {
		wh.Secret = &secret.String
	}
	wh.Events = canonicalEventsJSON(wh.Events)
	return &wh, nil
}

func webhookEvents(wh models.Webhook) []any {
	events, _ := uiJSON(wh.Events).([]any)
	if events == nil {
		events = []any{}
	}
	return events
}

// deliveryProps is a delivery as the pages read it.
func deliveryProps(d models.WebhookDelivery) map[string]any {
	return map[string]any{
		"id":            d.ID,
		"webhook_id":    d.WebhookID,
		"event":         d.Event,
		"payload":       uiJSON(d.Payload),
		"response_code": d.ResponseCode,
		"response_body": d.ResponseBody,
		"attempts":      d.Attempts,
		"delivered_at":  uiTime(d.DeliveredAt),
		"created_at":    uiTimeValue(d.CreatedAt),
	}
}

// deliveries lists a webhook's deliveries, newest first.
func (h *WebhookHandler) deliveries(r *http.Request, webhookID int64, limit, offset int) ([]models.WebhookDelivery, error) {
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, webhook_id, event, payload, response_code, attempts, delivered_at, created_at
		   FROM escalated_webhook_deliveries
		  WHERE webhook_id = ?
		  ORDER BY created_at DESC, id DESC
		  LIMIT ? OFFSET ?`), webhookID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []models.WebhookDelivery{}
	for rows.Next() {
		var d models.WebhookDelivery
		var payload sql.NullString
		var code sql.NullInt64
		var deliveredAt sql.NullTime
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.Event, &payload, &code, &d.Attempts, &deliveredAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		if payload.Valid {
			d.Payload = models.JSONText(payload.String)
		}
		if code.Valid {
			c := int(code.Int64)
			d.ResponseCode = &c
		}
		if deliveredAt.Valid {
			d.DeliveredAt = &deliveredAt.Time
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// renderIndex renders Escalated/Admin/Webhooks/Index (Laravel
// WebhookController::index): each webhook with its delivery count and its
// latest delivery.
func (h *WebhookHandler) renderIndex(w http.ResponseWriter, r *http.Request, webhooks []models.Webhook) {
	rows := make([]map[string]any, 0, len(webhooks))
	for _, wh := range webhooks {
		latest := []map[string]any{}
		if found, err := h.deliveries(r, wh.ID, 1, 0); err == nil {
			for _, d := range found {
				latest = append(latest, deliveryProps(d))
			}
		}
		var count int
		_ = ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
			`SELECT COUNT(1) FROM escalated_webhook_deliveries WHERE webhook_id = ?`), wh.ID).Scan(&count)
		rows = append(rows, map[string]any{
			"id":               wh.ID,
			"url":              wh.URL,
			"events":           webhookEvents(wh),
			"active":           wh.Active,
			"has_secret":       wh.HasSecret(),
			"deliveries_count": count,
			"deliveries":       latest,
			"created_at":       uiTimeValue(wh.CreatedAt),
			"updated_at":       uiTimeValue(wh.UpdatedAt),
		})
	}
	h.Pages.render(w, r, "Escalated/Admin/Webhooks/Index", map[string]any{"webhooks": rows}, nil)
}

// CreateForm renders Escalated/Admin/Webhooks/Form for a new webhook
// (GET /admin/webhooks/create).
func (h *WebhookHandler) CreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, nil, nil)
}

// EditForm renders Escalated/Admin/Webhooks/Form with the webhook
// (GET /admin/webhooks/{id}/edit). A subscription saved under a legacy event
// name shows as its current name.
func (h *WebhookHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	wh, err := h.find(r, id)
	if err != nil {
		http.Error(w, "could not load the webhook", http.StatusInternalServerError)
		return
	}
	if wh == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.renderForm(w, r, wh, nil)
}

func (h *WebhookHandler) renderForm(w http.ResponseWriter, r *http.Request, wh *models.Webhook, errs map[string]string) {
	props := map[string]any{"availableEvents": services.SupportedWebhookEvents}
	if wh != nil {
		// The signing secret stays server-side; the form learns only that
		// one is set.
		props["webhook"] = map[string]any{
			"id":         wh.ID,
			"url":        wh.URL,
			"events":     webhookEvents(*wh),
			"has_secret": wh.HasSecret(),
			"active":     wh.Active,
		}
	}
	h.Pages.render(w, r, "Escalated/Admin/Webhooks/Form", props, errs)
}

// renderDeliveryLog renders Escalated/Admin/Webhooks/DeliveryLog: the webhook
// and its deliveries, 25 a page (Laravel WebhookController::deliveries).
func (h *WebhookHandler) renderDeliveryLog(w http.ResponseWriter, r *http.Request, id int64) {
	wh, err := h.find(r, id)
	if err != nil {
		http.Error(w, "could not load the webhook", http.StatusInternalServerError)
		return
	}
	if wh == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	pg := uiPageFrom(r.URL.Query(), 25)
	var total int
	_ = ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`SELECT COUNT(1) FROM escalated_webhook_deliveries WHERE webhook_id = ?`), id).Scan(&total)
	found, err := h.deliveries(r, id, pg.PerPage, pg.offset())
	if err != nil {
		http.Error(w, "could not list deliveries", http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(found))
	for _, d := range found {
		rows = append(rows, deliveryProps(d))
	}
	h.Pages.render(w, r, "Escalated/Admin/Webhooks/DeliveryLog", map[string]any{
		"webhook":    map[string]any{"id": wh.ID, "url": wh.URL, "events": webhookEvents(*wh), "active": wh.Active, "has_secret": wh.HasSecret()},
		"deliveries": uiPaginator(rows, total, pg, r.URL.Path),
	}, nil)
}
