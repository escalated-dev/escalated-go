package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// CustomerHandler serves the customer-facing ticket portal.
type CustomerHandler struct {
	store    store.Store
	tickets  *services.TicketService
	renderer renderer.Renderer
	userID   func(r *http.Request) int64
}

// NewCustomerHandler creates a new CustomerHandler.
func NewCustomerHandler(s store.Store, ts *services.TicketService, rend renderer.Renderer, userIDFunc func(r *http.Request) int64) *CustomerHandler {
	return &CustomerHandler{
		store:    s,
		tickets:  ts,
		renderer: rend,
		userID:   userIDFunc,
	}
}

// Index shows the customer's ticket list.
func (h *CustomerHandler) Index(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)
	f := models.TicketFilters{RequesterID: &uid, Limit: 50}
	tickets, total, err := h.store.ListTickets(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, t := range tickets {
		t.PopulateComputed(nil)
	}

	_ = h.renderer.Render(w, r, "Customer/Tickets/Index", map[string]any{
		"tickets": tickets,
		"total":   total,
	})
}

// Show displays a single ticket to the customer.
func (h *CustomerHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	t, err := h.tickets.Get(r.Context(), id)
	if err != nil || t == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Only show public replies to customers
	isInternal := false
	replies, _ := h.store.ListReplies(r.Context(), models.ReplyFilters{
		TicketID: id,
		Internal: &isInternal,
	})

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

	t.PopulateComputed(replies)


	_ = h.renderer.Render(w, r, "Customer/Tickets/Show", map[string]any{
		"ticket":      t,
		"replies":     replies,
		"attachments": ticketAttachments,
	})
}

// Create handles the ticket creation form submission.
func (h *CustomerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Subject      string `json:"subject"`
		Description  string `json:"description"`
		Priority     int    `json:"priority"`
		DepartmentID *int64 `json:"department_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}

	uid := h.userID(r)
	reqType := "User"

	t, err := h.tickets.Create(r.Context(), services.CreateTicketInput{
		Subject:       in.Subject,
		Description:   in.Description,
		Priority:      in.Priority,
		RequesterType: &reqType,
		RequesterID:   &uid,
		DepartmentID:  in.DepartmentID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	t.PopulateComputed(nil)
	writeJSON(w, http.StatusCreated, map[string]any{"ticket": t})
}

// Reply handles a customer adding a reply to their ticket.
func (h *CustomerHandler) Reply(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
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

	uid := h.userID(r)
	authorType := "User"

	reply, err := h.tickets.AddReply(r.Context(), id, in.Body, &authorType, &uid, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"reply": reply})
}
