package handlers

import (
	"net/http"
	"time"

	"github.com/escalated-dev/escalated-go/services"
)

// RetentionHandler exposes the admin data-retention purge action.
type RetentionHandler struct {
	service *services.RetentionService
}

// NewRetentionHandler creates a RetentionHandler.
func NewRetentionHandler(service *services.RetentionService) *RetentionHandler {
	return &RetentionHandler{service: service}
}

// Purge handles POST /admin/data-retention/purge — purge expired attachments
// and audit logs per the configured retention policy, reporting closed-ticket
// candidates. Pass ?dry_run=1 to report counts without deleting.
func (h *RetentionHandler) Purge(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dry_run") == "1" || r.URL.Query().Get("dry_run") == "true"

	report, err := h.service.PurgeExpired(r.Context(), time.Now(), dryRun)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, report)
}
