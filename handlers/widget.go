package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// WidgetConfig holds configuration for the embeddable widget.
type WidgetConfig struct {
	Enabled         bool   `json:"enabled"`
	Title           string `json:"title"`
	Subtitle        string `json:"subtitle"`
	PrimaryColor    string `json:"primary_color"`
	LogoURL         string `json:"logo_url,omitempty"`
	WelcomeMessage  string `json:"welcome_message,omitempty"`
	AllowedOrigins  string `json:"allowed_origins,omitempty"` // comma-separated
	RateLimitPerMin int    `json:"-"`
}

// DefaultWidgetConfig returns a widget config with sensible defaults.
func DefaultWidgetConfig() WidgetConfig {
	return WidgetConfig{
		Enabled:         false,
		Title:           "Help Center",
		Subtitle:        "How can we help you?",
		PrimaryColor:    "#4F46E5",
		RateLimitPerMin: 30,
	}
}

// WidgetHandler serves the public widget endpoints.
type WidgetHandler struct {
	config  WidgetConfig
	store   store.Store
	tickets *services.TicketService
	limiter *rateLimiter
}

// NewWidgetHandler creates a new WidgetHandler.
func NewWidgetHandler(cfg WidgetConfig, s store.Store, ts *services.TicketService) *WidgetHandler {
	limit := cfg.RateLimitPerMin
	if limit <= 0 {
		limit = 30
	}
	return &WidgetHandler{
		config:  cfg,
		store:   s,
		tickets: ts,
		limiter: newRateLimiter(limit, time.Minute),
	}
}

// Config handles GET /widget/config - returns public widget configuration.
func (h *WidgetHandler) Config(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title":           h.config.Title,
		"subtitle":        h.config.Subtitle,
		"primary_color":   h.config.PrimaryColor,
		"logo_url":        h.config.LogoURL,
		"welcome_message": h.config.WelcomeMessage,
	})
}

// SearchArticles handles GET /widget/articles?q=... - search knowledge base articles.
func (h *WidgetHandler) SearchArticles(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"articles": []any{}})
		return
	}

	// Use ticket search as proxy (in a full implementation, articles would have their own store method)
	writeJSON(w, http.StatusOK, map[string]any{
		"articles": []any{},
		"query":    query,
	})
}

// ShowArticle handles GET /widget/articles/{id} - get a single article.
func (h *WidgetHandler) ShowArticle(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	_, err := idFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid article id"})
		return
	}

	// Placeholder: would query article store
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
}

// CreateTicket handles POST /widget/tickets - create a ticket from the widget (no auth).
func (h *WidgetHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	var in struct {
		Name        string `json:"name"`
		Email       string `json:"email"`
		Subject     string `json:"subject"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if in.Email == "" || in.Subject == "" || in.Description == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "email, subject, and description are required"})
		return
	}

	t, err := h.tickets.Create(r.Context(), services.CreateTicketInput{
		Subject:     in.Subject,
		Description: in.Description,
		GuestName:   &in.Name,
		GuestEmail:  &in.Email,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"ticket_reference": t.Reference,
		"guest_token":      t.GuestToken,
	})
}

// LookupTicket handles GET /widget/tickets/lookup?ref=...&token=... - lookup ticket by reference and guest token.
func (h *WidgetHandler) LookupTicket(w http.ResponseWriter, r *http.Request) {
	h.setCORS(w, r)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	ref := r.URL.Query().Get("ref")
	token := r.URL.Query().Get("token")
	if ref == "" || token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ref and token are required"})
		return
	}

	t, err := h.store.GetTicketByReference(r.Context(), ref)
	if err != nil || t == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found"})
		return
	}

	if t.GuestToken == nil || *t.GuestToken != token {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid token"})
		return
	}

	// Load public replies
	isInternal := false
	replies, _ := h.store.ListReplies(r.Context(), models.ReplyFilters{
		TicketID: t.ID,
		Internal: &isInternal,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"ticket": map[string]any{
			"reference":   t.Reference,
			"subject":     t.Subject,
			"status":      t.StatusString(),
			"description": t.Description,
			"created_at":  t.CreatedAt,
		},
		"replies": replies,
	})
}

func (h *WidgetHandler) setCORS(w http.ResponseWriter, _ *http.Request) {
	origins := h.config.AllowedOrigins
	if origins == "" {
		origins = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origins)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func (h *WidgetHandler) rateLimit(w http.ResponseWriter, r *http.Request) bool {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	if !h.limiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return false
	}
	return true
}

// rateLimiter implements a simple per-IP sliding window rate limiter.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Prune old entries
	var recent []time.Time
	for _, t := range rl.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.limit {
		rl.hits[key] = recent
		return false
	}

	rl.hits[key] = append(recent, now)
	return true
}
