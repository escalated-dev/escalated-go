package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// AttachmentHandler serves attachment download endpoints.
type AttachmentHandler struct {
	store       store.Store
	routePrefix string

	// AgentCheck, AdminCheck and UserID decide who may download. Agents and
	// admins may download any attachment. Anyone else may download only from a
	// ticket they requested, and never an attachment on an internal note. The
	// routers wire these from Config. A nil check counts as "no", so a handler
	// mounted without them refuses every download.
	AgentCheck func(r *http.Request) bool
	AdminCheck func(r *http.Request) bool
	UserID     func(r *http.Request) models.UserID
}

// NewAttachmentHandler creates a new AttachmentHandler.
func NewAttachmentHandler(s store.Store, routePrefix string) *AttachmentHandler {
	return &AttachmentHandler{
		store:       s,
		routePrefix: routePrefix,
	}
}

// Download handles GET /attachments/{id}/download — streams the file from disk
// to an agent, an admin, or the customer who requested the owning ticket.
func (h *AttachmentHandler) Download(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment id"})
		return
	}

	staff := h.isStaff(r)
	var uid models.UserID
	if h.UserID != nil {
		uid = h.UserID(r)
	}
	// Checked before the lookup, so an anonymous caller cannot even learn
	// whether an id exists.
	if !staff && uid.Empty() {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	a, err := h.store.GetAttachmentByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "attachment not found"})
		return
	}

	if !staff {
		allowed, err := h.requesterMayDownload(r, a, uid)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !allowed {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
	}

	f, err := os.Open(a.StoragePath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found on disk"})
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", a.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, a.OriginalFilename))
	w.Header().Set("Content-Length", strconv.FormatInt(a.Size, 10))

	http.ServeContent(w, r, a.OriginalFilename, a.CreatedAt, f)
}

func (h *AttachmentHandler) isStaff(r *http.Request) bool {
	return (h.AgentCheck != nil && h.AgentCheck(r)) || (h.AdminCheck != nil && h.AdminCheck(r))
}

// requesterMayDownload reports whether uid requested the attachment's ticket and
// the attachment is not on an internal note, which customers never see.
func (h *AttachmentHandler) requesterMayDownload(r *http.Request, a *models.Attachment, uid models.UserID) (bool, error) {
	t, err := h.store.GetTicket(r.Context(), a.TicketID)
	if err != nil || t == nil {
		return false, err
	}
	if !requestedBy(t, uid) {
		return false, nil
	}
	if a.ReplyID == nil {
		return true, nil
	}
	reply, err := h.store.GetReply(r.Context(), *a.ReplyID)
	if err != nil {
		return false, err
	}
	return reply != nil && !reply.IsInternal, nil
}

// AttachmentURL returns the download URL for an attachment given its ID.
func AttachmentURL(routePrefix string, id int64) string {
	return fmt.Sprintf("%s/attachments/%d/download", routePrefix, id)
}
