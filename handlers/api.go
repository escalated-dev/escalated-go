// Package handlers provides HTTP handlers for the Escalated ticket system.
// All handlers use standard http.HandlerFunc signatures and work with any router.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// APIHandler serves the JSON REST API endpoints.
type APIHandler struct {
	store    store.Store
	tickets  *services.TicketService
	Subjects *services.TicketSubjectService
	renderer renderer.Renderer
	userID   func(r *http.Request) models.UserID

	// Actions, OnCustomAction, and RoutePrefix are wired by the router to
	// support host-defined custom ticket actions. They may be nil/empty.
	Actions        *actions.Registry
	OnCustomAction func(ctx context.Context, e actions.CustomActionEvent) error
	RoutePrefix    string

	// CustomFields validates and stores custom_fields on create and update
	// and adds them to the ticket detail. Optional.
	CustomFields *services.CustomFieldService
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(s store.Store, ts *services.TicketService, rend renderer.Renderer, userIDFunc func(r *http.Request) models.UserID) *APIHandler {
	return &APIHandler{
		store:    s,
		tickets:  ts,
		renderer: rend,
		userID:   userIDFunc,
	}
}

// ListTickets handles GET /api/tickets
func (h *APIHandler) ListTickets(w http.ResponseWriter, r *http.Request) {
	f := models.TicketFilters{
		Search: r.URL.Query().Get("search"),
	}
	if v := r.URL.Query().Get("status"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Status = &i
		}
	}
	if v := r.URL.Query().Get("priority"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Priority = &i
		}
	}
	if v := r.URL.Query().Get("department_id"); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.DepartmentID = &i
		}
	}
	if v := r.URL.Query().Get("assigned_to"); v != "" {
		uid := models.UserID(v)
		f.AssignedTo = &uid
	}
	if r.URL.Query().Get("unassigned") == "true" {
		f.Unassigned = true
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Limit = i
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			f.Offset = i
		}
	}
	if value := r.URL.Query().Get("account_type"); value != "" {
		f.AccountType = &value
	}
	if value := r.URL.Query().Get("account_id"); value != "" {
		f.AccountID = &value
	}
	f.SortBy = r.URL.Query().Get("sort_by")
	f.SortOrder = r.URL.Query().Get("sort_order")

	tickets, total, err := h.store.ListTickets(r.Context(), f)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Populate computed fields for each ticket, and the custom status.
	p := h.statusPresenter()
	rows := make([]any, 0, len(tickets))
	for _, t := range tickets {
		t.PopulateComputed(nil)
		rows = append(rows, apiTicketPayload(r.Context(), p, t))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tickets": rows,
		"total":   total,
	})
}

// ShowTicket handles GET /api/tickets/{id}
func (h *APIHandler) ShowTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	t, err := h.tickets.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	// Load replies
	replies, _ := h.store.ListReplies(r.Context(), models.ReplyFilters{TicketID: id})
	activities, _ := h.store.ListActivities(r.Context(), id, 50)

	// Populate human-readable timestamps on activities.
	for _, a := range activities {
		a.PopulateHumanTime()
	}

	// Load attachments for the ticket
	ticketAttachments, _ := h.store.GetAttachmentsByTicketID(r.Context(), id)
	populateAttachmentURLs(ticketAttachments, "/escalated")

	// Attach per-reply attachments
	for _, rpl := range replies {
		replyAtts, _ := h.store.GetAttachmentsByReplyID(r.Context(), rpl.ID)
		populateAttachmentURLs(replyAtts, "/escalated")
		if len(replyAtts) > 0 {
			rpl.Attachments = make([]models.Attachment, len(replyAtts))
			for i, a := range replyAtts {
				rpl.Attachments[i] = *a
			}
		}
	}

	// Build computed-field options for the detail response.
	opts := models.PopulateComputedOpts{}

	// Chat session and messages
	if chatSession, _ := h.store.GetChatSessionByTicket(r.Context(), id); chatSession != nil {
		opts.ChatSession = chatSession
		if msgs, _ := h.store.ListChatMessages(r.Context(), chatSession.ID); len(msgs) > 0 {
			opts.ChatMessages = msgs
		}
	}

	// Requester ticket count
	if t.RequesterType != nil && t.RequesterID != nil {
		if count, err := h.store.CountTicketsByRequester(r.Context(), *t.RequesterType, *t.RequesterID); err == nil {
			opts.RequesterTicketCount = &count
		}
	}

	// Related tickets (merged/split links)
	if related, _ := h.store.ListRelatedTickets(r.Context(), id); len(related) > 0 {
		opts.RelatedTickets = related
	}

	t.PopulateComputedFull(replies, opts)
	t.CustomActions = h.customActionsFor(t, h.userID(r))
	populateTicketSubjects(r.Context(), h, t)

	// The ticket payload gains custom_fields: the active ticket fields with
	// this ticket's values.
	var ticket any = t
	if payload, ok := apiTicketPayload(r.Context(), h.statusPresenter(), t).(map[string]any); ok {
		payload["custom_fields"] = h.CustomFields.TicketFieldValues(r.Context(), t.ID)
		ticket = payload
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":      ticket,
		"replies":     replies,
		"activities":  activities,
		"attachments": ticketAttachments,
	})
}

// statusPresenter resolves custom statuses for the API's ticket payloads.
func (h *APIHandler) statusPresenter() *uiPresenter {
	p := &uiPresenter{store: h.store}
	if h.tickets != nil {
		p.statuses = h.tickets.Statuses
	}
	return p
}

// apiTicketPayload is the ticket's JSON with custom_status and
// status_category added (the agent API is staff-only).
func apiTicketPayload(ctx context.Context, p *uiPresenter, t *models.Ticket) any {
	b, err := json.Marshal(t)
	if err != nil {
		return t
	}
	payload := map[string]any{}
	if json.Unmarshal(b, &payload) != nil {
		return t
	}
	p.staffStatus(ctx, t, payload)
	return payload
}

// customActionsFor serializes the visible custom actions for a ticket, adding
// url + method (pointing at the API surface).
func (h *APIHandler) customActionsFor(t *models.Ticket, userID models.UserID) []map[string]any {
	if h.Actions == nil {
		return nil
	}
	prefix := h.RoutePrefix
	if prefix == "" {
		prefix = "/escalated"
	}
	out := h.Actions.ForTicket(t, userID)
	for _, a := range out {
		a["url"] = fmt.Sprintf("%s/api/tickets/%d/actions/%v", prefix, t.ID, a["key"])
		a["method"] = "post"
	}
	return out
}

// CustomAction handles POST /api/tickets/{id}/actions/{action}.
func (h *APIHandler) CustomAction(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}
	if h.Actions == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "custom action not found"})
		return
	}

	t, err := h.tickets.Get(r.Context(), id)
	if err != nil || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	key := r.PathValue("action")
	uid := h.userID(r)
	action, ok := h.Actions.Find(key)
	if !ok || !h.Actions.Visible(action, t, uid) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "custom action not found"})
		return
	}
	if !h.Actions.Enabled(action, t, uid) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "custom action is not enabled"})
		return
	}

	var in struct {
		Payload map[string]any `json:"payload"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)

	authorType := "User"
	_, _ = h.tickets.AddReply(r.Context(), id, fmt.Sprintf("Custom action %q was triggered.", key), &authorType, &uid, true)

	if h.OnCustomAction != nil {
		_ = h.OnCustomAction(r.Context(), actions.CustomActionEvent{
			Ticket:   t,
			Action:   key,
			UserID:   uid,
			Payload:  in.Payload,
			Metadata: action.Metadata,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"message": "Custom action dispatched.", "action": key})
}

// CreateTicket handles POST /api/tickets
func (h *APIHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Subject      string `json:"subject"`
		Description  string `json:"description"`
		Priority     int    `json:"priority"`
		TicketType   string `json:"ticket_type"`
		DepartmentID *int64 `json:"department_id"`

		CustomFields map[string]any `json:"custom_fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Subject == "" || in.Description == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "subject and description are required"})
		return
	}

	// Custom field values are validated before the ticket is written, so a
	// failed create leaves no ticket behind.
	fieldErrs := map[string]string{}
	values, err := validateTicketCustomFields(r, h.CustomFields, in.CustomFields, true, fieldErrs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read the custom fields"})
		return
	}
	if len(fieldErrs) > 0 {
		validationErrors(w, r, fieldErrs, nil)
		return
	}

	uid := h.userID(r)
	var reqType *string
	var reqID *models.UserID
	if !uid.Empty() {
		rt := "User"
		reqType = &rt
		reqID = &uid
	}

	t, err := h.tickets.Create(r.Context(), services.CreateTicketInput{
		Subject:       in.Subject,
		Description:   in.Description,
		Priority:      in.Priority,
		TicketType:    in.TicketType,
		RequesterType: reqType,
		RequesterID:   reqID,
		DepartmentID:  in.DepartmentID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.CustomFields.SaveTicketValues(r.Context(), t.ID, values); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save the custom fields"})
		return
	}

	t.PopulateComputed(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"ticket": t})
}

// UpdateTicket handles PATCH /api/tickets/{id}
func (h *APIHandler) UpdateTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	t, err := h.store.GetTicket(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Validate everything before writing anything, so a bad field never leaves
	// a half-applied update behind.
	errs := map[string][]string{}
	// status and priority take the number or the slug ("resolved", "high").
	enumField := func(key string, parse func(any) (int, bool), msg string) *int {
		raw, ok := updates[key]
		if !ok || raw == nil {
			return nil
		}
		v, valid := parse(raw)
		if !valid {
			errs[key] = []string{msg}
			return nil
		}
		return &v
	}
	strField := func(key string) *string {
		raw, ok := updates[key]
		if !ok || raw == nil {
			return nil
		}
		v, isStr := raw.(string)
		if !isStr {
			errs[key] = []string{"The " + key + " must be a string."}
			return nil
		}
		return &v
	}
	// status also takes a custom status's slug.
	var status *int
	var customStatus *models.TicketStatus
	if raw, ok := updates["status"]; ok && raw != nil {
		v, custom, valid, err := h.tickets.ResolveStatus(r.Context(), raw)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
			return
		}
		if !valid {
			errs["status"] = []string{"The selected status is invalid."}
		} else {
			status, customStatus = &v, custom
		}
	}
	priority := enumField("priority", services.PriorityFromValue, "The selected priority is invalid.")
	subject, description, ticketType := strField("subject"), strField("description"), strField("ticket_type")
	var departmentID *int64
	if raw, ok := updates["department_id"]; ok && raw != nil {
		f, isNum := raw.(float64)
		if !isNum || f != float64(int64(f)) {
			errs["department_id"] = []string{"The selected department is invalid."}
		} else if d, err := h.store.GetDepartment(r.Context(), int64(f)); err != nil || d == nil {
			errs["department_id"] = []string{"The selected department is invalid."}
		} else {
			v := int64(f)
			departmentID = &v
		}
	}
	// custom_fields: an object keyed by field slug; only the keys sent are
	// checked, and omitting it changes nothing.
	var fieldValues services.FieldValues
	if raw, sent := updates["custom_fields"]; sent && raw != nil {
		input, isObject := raw.(map[string]any)
		if !isObject {
			errs["custom_fields"] = []string{"The custom fields must be an object."}
		} else {
			fieldErrs := map[string]string{}
			values, err := validateTicketCustomFields(r, h.CustomFields, input, false, fieldErrs)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not read the custom fields"})
				return
			}
			for k, v := range fieldErrs {
				errs[k] = []string{v}
			}
			fieldValues = values
		}
	}
	if len(errs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "The given data was invalid.", "errors": errs})
		return
	}

	// Each change goes through the ticket service, so it records activity and
	// fires the same webhooks and workflows (status_changed, resolved,
	// priority_changed, department_changed, updated) as the agent screens.
	var causer *models.UserID
	if h.userID != nil {
		if uid := h.userID(r); !uid.Empty() {
			causer = &uid
		}
	}
	ctx := r.Context()
	if _, err := h.tickets.UpdateDetails(ctx, id, subject, description, ticketType, causer); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
		return
	}
	if customStatus != nil {
		if err := h.tickets.SetCustomStatus(ctx, id, customStatus, causer); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
			return
		}
	} else if status != nil && (*status != t.Status || t.TicketStatusID != nil) {
		// A built-in status replaces a custom one even when it is the anchor.
		if err := h.tickets.ChangeStatus(ctx, id, *status, causer); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
			return
		}
	}
	if priority != nil {
		if err := h.tickets.ChangePriority(ctx, id, *priority, causer); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
			return
		}
	}
	if departmentID != nil {
		if err := h.tickets.ChangeDepartment(ctx, id, *departmentID, causer); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
			return
		}
	}
	if err := h.CustomFields.SaveTicketValues(ctx, id, fieldValues); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not update ticket"})
		return
	}

	t, err = h.store.GetTicket(ctx, id)
	if err != nil || t == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not load ticket"})
		return
	}

	t.PopulateComputed(nil)
	writeJSON(w, http.StatusOK, map[string]any{"ticket": t})
}

// CreateReply handles POST /api/tickets/{id}/replies
func (h *APIHandler) CreateReply(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	if ticket, err := h.store.GetTicket(r.Context(), id); err != nil || ticket == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	var in struct {
		Body       string `json:"body"`
		IsInternal bool   `json:"is_internal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Body == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "body is required"})
		return
	}

	uid := h.userID(r)
	var authorType *string
	var authorID *models.UserID
	if !uid.Empty() {
		at := "User"
		authorType = &at
		authorID = &uid
	}

	reply, err := h.tickets.AddReply(r.Context(), id, in.Body, authorType, authorID, in.IsInternal)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"reply": reply})
}

// SplitTicket handles POST /api/tickets/{id}/split
func (h *APIHandler) SplitTicket(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ticket id"})
		return
	}

	var in struct {
		ReplyID int64  `json:"reply_id"`
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.ReplyID == 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "reply_id is required"})
		return
	}

	uid := h.userID(r)
	var causerID *models.UserID
	if !uid.Empty() {
		causerID = &uid
	}

	newTicket, err := h.tickets.SplitTicket(r.Context(), services.SplitTicketInput{
		TicketID: id,
		ReplyID:  in.ReplyID,
		Subject:  in.Subject,
		CauserID: causerID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	newTicket.PopulateComputed(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"ticket": newTicket})
}

// ListDepartments handles GET /api/departments
func (h *APIHandler) ListDepartments(w http.ResponseWriter, r *http.Request) {
	depts, err := h.store.ListDepartments(r.Context(), true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"departments": depts})
}

// ListTags handles GET /api/tags
func (h *APIHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.store.ListTags(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

// --- helpers ---

// populateAttachmentURLs sets the computed URL field on each attachment.
func populateAttachmentURLs(attachments []*models.Attachment, routePrefix string) {
	for _, a := range attachments {
		a.URL = AttachmentURL(routePrefix, a.ID)
	}
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

// idFromPath extracts the last path segment as an int64.
// Works with both chi ({id} in URL params) and stdlib patterns.
func idFromPath(r *http.Request) (int64, error) {
	// Try standard library PathValue first (Go 1.22+)
	if v := r.PathValue("id"); v != "" {
		return strconv.ParseInt(v, 10, 64)
	}
	// Fallback: extract last path segment
	path := r.URL.Path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return strconv.ParseInt(path[i+1:], 10, 64)
			//nolint:staticcheck // intentional break
		}
	}
	return strconv.ParseInt(path, 10, 64)
}

// refFromPath extracts a reference string from the URL path.
func refFromPath(r *http.Request) string {
	if v := r.PathValue("reference"); v != "" {
		return v
	}
	if v := r.PathValue("ref"); v != "" {
		return v
	}
	path := r.URL.Path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
