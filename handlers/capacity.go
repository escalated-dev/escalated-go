package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/escalated-dev/escalated-go/services"
)

// CapacityHandler exposes the admin view of per-agent capacity and ceiling
// adjustment. Mirrors the Laravel CapacityController.
type CapacityHandler struct {
	Service *services.CapacityService
}

// NewCapacityHandler constructs the handler.
func NewCapacityHandler(db *sql.DB) *CapacityHandler {
	return &CapacityHandler{Service: services.NewCapacityService(db)}
}

// List handles GET /admin/capacity.
func (h *CapacityHandler) List(w http.ResponseWriter, _ *http.Request) {
	caps, err := h.Service.AllCapacities()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]map[string]any, 0, len(caps))
	for _, c := range caps {
		out = append(out, map[string]any{
			"id":              c.ID,
			"user_id":         c.UserID,
			"channel":         c.Channel,
			"max_concurrent":  c.MaxConcurrent,
			"current_count":   c.CurrentCount,
			"load_percentage": c.LoadPercentage(),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"capacities": out})
}

// Update handles PATCH /admin/capacity/{id} — adjust the ceiling.
func (h *CapacityHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var in struct {
		MaxConcurrent int `json:"max_concurrent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.MaxConcurrent < 1 || in.MaxConcurrent > 999 {
		http.Error(w, "max_concurrent must be an integer between 1 and 999", http.StatusBadRequest)
		return
	}

	if err := h.Service.UpdateMaxConcurrent(id, in.MaxConcurrent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}
