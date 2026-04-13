package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/escalated-dev/escalated-go/store"
)

// AttachmentHandler serves attachment download endpoints.
type AttachmentHandler struct {
	store       store.Store
	routePrefix string
}

// NewAttachmentHandler creates a new AttachmentHandler.
func NewAttachmentHandler(s store.Store, routePrefix string) *AttachmentHandler {
	return &AttachmentHandler{
		store:       s,
		routePrefix: routePrefix,
	}
}

// Download handles GET /attachments/{id}/download — streams the file from disk.
func (h *AttachmentHandler) Download(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid attachment id"})
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

// AttachmentURL returns the download URL for an attachment given its ID.
func AttachmentURL(routePrefix string, id int64) string {
	return fmt.Sprintf("%s/attachments/%d/download", routePrefix, id)
}
