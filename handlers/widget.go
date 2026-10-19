package handlers

import (
	"database/sql"
	"encoding/json"
	"html"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// WidgetConfig holds configuration for the embeddable widget.
type WidgetConfig struct {
	Enabled        bool   `json:"enabled"`
	Title          string `json:"title"`
	Subtitle       string `json:"subtitle"`
	PrimaryColor   string `json:"primary_color"`
	LogoURL        string `json:"logo_url,omitempty"`
	WelcomeMessage string `json:"welcome_message,omitempty"`
	// Position is where the launcher sits: "bottom-right" (the default) or
	// "bottom-left".
	Position string `json:"position,omitempty"`
	// AllowedOrigins lists the other sites that may embed the widget, comma or
	// newline separated: "https://shop.example.com, https://*.example.com".
	// Entries are scheme://host[:port] (http or https, no path); "*." matches
	// any subdomain but not the apex. A bare "*" is ignored. Empty (the
	// default) grants no cross-origin access. Admins can add more through the
	// widget_allowed_origins setting. Enforced by WidgetCORS.
	AllowedOrigins  string `json:"allowed_origins,omitempty"`
	RateLimitPerMin int    `json:"-"`
}

// DefaultWidgetConfig returns a widget config with sensible defaults.
// AllowedOrigins starts from the ESCALATED_WIDGET_ALLOWED_ORIGINS environment
// variable (empty when unset).
func DefaultWidgetConfig() WidgetConfig {
	return WidgetConfig{
		Enabled:         false,
		Title:           "Help Center",
		Subtitle:        "How can we help you?",
		PrimaryColor:    "#4F46E5",
		Position:        "bottom-right",
		AllowedOrigins:  os.Getenv("ESCALATED_WIDGET_ALLOWED_ORIGINS"),
		RateLimitPerMin: 30,
	}
}

// WidgetHandler serves the public widget endpoints.
type WidgetHandler struct {
	config  WidgetConfig
	store   store.Store
	tickets *services.TicketService
	limiter *rateLimiter

	// DB is the database holding the knowledge base (escalated_articles).
	// When nil the widget has no knowledge base: /config reports
	// kb_enabled false, searches return no articles and every article is 404.
	// The routers set it from Config.DB.
	DB *sql.DB
}

// NewWidgetHandler creates a new WidgetHandler.
func NewWidgetHandler(cfg WidgetConfig, s store.Store, ts *services.TicketService) *WidgetHandler {
	return &WidgetHandler{
		config:  cfg,
		store:   s,
		tickets: ts,
		limiter: newWidgetRateLimiter(cfg),
	}
}

// widgetRateLimitWindow is the window RateLimitPerMin counts over, shared by
// the ticket and chat limiters.
const widgetRateLimitWindow = time.Minute

// newWidgetRateLimiter returns the per-client-IP limiter for the widget
// endpoints: RateLimitPerMin requests a minute, 30 when unset.
func newWidgetRateLimiter(cfg WidgetConfig) *rateLimiter {
	limit := cfg.RateLimitPerMin
	if limit <= 0 {
		limit = 30
	}
	return newRateLimiter(limit, widgetRateLimitWindow)
}

// Config handles GET /widget/config - returns public widget configuration.
//
// The top-level keys the shared widget reads (enabled, color, position,
// greeting, departments, kb_enabled, guest_tickets_enabled,
// status_lookup_enabled) sit alongside the original ones (primary_color,
// welcome_message, ...). This backend has no mailbox verification, so
// status_lookup_enabled is false and guest_verification_required is never
// sent: the widget hides its Status tab.
func (h *WidgetHandler) Config(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"enabled": false,
			"error":   "widget is disabled",
			"message": "The widget is disabled.",
		})
		return
	}

	departments := []map[string]any{}
	if list, err := h.store.ListDepartments(r.Context(), true); err == nil {
		for _, d := range list {
			if d != nil && d.IsActive {
				departments = append(departments, map[string]any{"id": d.ID, "name": d.Name})
			}
		}
	}

	position := h.config.Position
	if position == "" {
		position = "bottom-right"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":               true,
		"title":                 h.config.Title,
		"subtitle":              h.config.Subtitle,
		"primary_color":         h.config.PrimaryColor,
		"color":                 h.config.PrimaryColor,
		"position":              position,
		"logo_url":              h.config.LogoURL,
		"welcome_message":       h.config.WelcomeMessage,
		"greeting":              h.config.WelcomeMessage,
		"departments":           departments,
		"kb_enabled":            h.DB != nil,
		"guest_tickets_enabled": true,
		"status_lookup_enabled": false,
		// The widget may send host parameters (order number, plan, page
		// URL, ...) as "metadata" with a ticket or chat.
		"accepts_metadata": true,
	})
}

// widgetArticleSearchLimit caps the widget's search results.
const widgetArticleSearchLimit = 10

// SearchArticles handles GET /widget/articles?q=... - searches the published
// knowledge-base articles by title and body. Queries shorter than two
// characters, and a widget without a knowledge base, return no articles.
func (h *WidgetHandler) SearchArticles(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	articles := []map[string]any{}
	if h.DB == nil || utf8.RuneCountInString(query) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"articles": articles, "query": query})
		return
	}

	like := "%" + escapeLike(query) + "%"
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT title, slug, body
		FROM escalated_articles
		WHERE status = ? AND (title LIKE ? ESCAPE '!' OR body LIKE ? ESCAPE '!')
		ORDER BY published_at DESC, id DESC
		LIMIT `+strconv.Itoa(widgetArticleSearchLimit)), models.ArticleStatusPublished, like, like)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "article search failed", "message": "Article search failed."})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var title, slug, body string
		if err := rows.Scan(&title, &slug, &body); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "article search failed", "message": "Article search failed."})
			return
		}
		articles = append(articles, map[string]any{"title": title, "slug": slug, "excerpt": articleExcerpt(body)})
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "article search failed", "message": "Article search failed."})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"articles": articles, "query": query})
}

// ShowArticle handles GET /widget/articles/{slug} - one published article,
// with title, slug, body and category (its name, or null) at the top level.
// A numeric {id} path value from older wiring is still looked up by id.
func (h *WidgetHandler) ShowArticle(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	key := pathParam(r, "slug")
	if key == "" {
		key = pathParam(r, "id")
	}
	if key == "" || h.DB == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found", "message": "Article not found."})
		return
	}

	where, args := "a.slug = ?", []any{key}
	if id, err := strconv.ParseInt(key, 10, 64); err == nil {
		where, args = "(a.slug = ? OR a.id = ?)", []any{key, id}
	}
	args = append(args, models.ArticleStatusPublished)

	var (
		id       int64
		title    string
		slug     string
		body     string
		category sql.NullString
	)
	err := ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT a.id, a.title, a.slug, a.body, c.name
		FROM escalated_articles a
		LEFT JOIN escalated_article_categories c ON c.id = a.category_id
		WHERE `+where+` AND a.status = ?
		ORDER BY a.id ASC LIMIT 1`), args...).Scan(&id, &title, &slug, &body, &category)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found", "message": "Article not found."})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "article lookup failed", "message": "Article lookup failed."})
		return
	}

	_, _ = ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `UPDATE escalated_articles SET view_count = view_count + 1 WHERE id = ?`), id)

	var cat any
	if category.Valid {
		cat = category.String
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       id,
		"title":    title,
		"slug":     slug,
		"body":     body,
		"category": cat,
	})
}

// CreateTicket handles POST /widget/tickets - create a ticket from the widget (no auth).
func (h *WidgetHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r) {
		return
	}

	var in struct {
		Name         string          `json:"name"`
		Email        string          `json:"email"`
		Subject      string          `json:"subject"`
		Description  string          `json:"description"`
		DepartmentID *int64          `json:"department_id"`
		Metadata     json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "message": "Invalid request body."})
		return
	}

	missing := requiredWidgetFields(map[string]string{"email": in.Email, "subject": in.Subject, "description": in.Description})
	if len(missing) > 0 {
		writeWidgetValidation(w, "email, subject, and description are required", missing)
		return
	}
	if !widgetDepartmentValid(r, h.store, in.DepartmentID) {
		writeWidgetValidation(w, "the selected department is invalid", map[string][]string{
			"department_id": {"The selected department is invalid."},
		})
		return
	}
	metadata, err := sanitizeWidgetMetadata(in.Metadata, WidgetCrossOrigin(r.Context()))
	if err != nil {
		writeWidgetValidation(w, err.Error(), map[string][]string{"metadata": {err.Error()}})
		return
	}

	t, err := h.tickets.Create(r.Context(), services.CreateTicketInput{
		Subject:      in.Subject,
		Description:  in.Description,
		GuestName:    &in.Name,
		GuestEmail:   &in.Email,
		DepartmentID: in.DepartmentID,
		Metadata:     widgetMetadataJSON(metadata),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message":          "Ticket created.",
		"reference":        t.Reference,
		"ticket_reference": t.Reference,
		"guest_token":      t.GuestToken,
	})
}

// LookupTicket handles GET /widget/tickets/lookup?ref=...&token=... - lookup ticket by reference and guest token.
func (h *WidgetHandler) LookupTicket(w http.ResponseWriter, r *http.Request) {
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

	if !guestTokenMatches(t.GuestToken, token) {
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

func (h *WidgetHandler) rateLimit(w http.ResponseWriter, r *http.Request) bool {
	return widgetAllow(w, r, h.limiter)
}

// widgetAllow applies a widget limiter to the request's client IP (the port
// is ignored, so a new connection does not reset the count) and answers 429
// when it is over the limit.
func widgetAllow(w http.ResponseWriter, r *http.Request, l *rateLimiter) bool {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if !l.allow(ip) {
		w.Header().Set("Retry-After", strconv.Itoa(int(l.window/time.Second)))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded", "message": "Too many requests."})
		return false
	}
	return true
}

// requiredWidgetFields returns a validation error for each blank field.
func requiredWidgetFields(fields map[string]string) map[string][]string {
	errs := map[string][]string{}
	for field, value := range fields {
		if strings.TrimSpace(value) == "" {
			errs[field] = []string{"The " + field + " field is required."}
		}
	}
	return errs
}

// writeWidgetValidation answers 422 in the shape the widget reads
// ({message, errors: {field: [msg]}}), keeping the original "error" key.
func writeWidgetValidation(w http.ResponseWriter, msg string, errs map[string][]string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
		"error":   msg,
		"message": msg,
		"errors":  errs,
	})
}

// widgetDepartmentValid reports whether an optional department_id names an
// existing, active department. Absent (or null) is valid.
func widgetDepartmentValid(r *http.Request, s store.Store, id *int64) bool {
	if id == nil {
		return true
	}
	d, err := s.GetDepartment(r.Context(), *id)
	return err == nil && d != nil && d.IsActive
}

// pathParam reads a path value from a stdlib pattern or a chi route.
func pathParam(r *http.Request, name string) string {
	if v := r.PathValue(name); v != "" {
		return v
	}
	return chi.URLParam(r, name)
}

// escapeLike escapes LIKE wildcards in user input, with '!' as the escape
// character (portable across SQLite and PostgreSQL).
func escapeLike(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

var widgetTagPattern = regexp.MustCompile(`<[^>]*>`)

// articleExcerpt is the article body as plain text, cut to 160 characters.
func articleExcerpt(body string) string {
	text := strings.Join(strings.Fields(html.UnescapeString(widgetTagPattern.ReplaceAllString(body, " "))), " ")
	if utf8.RuneCountInString(text) <= 160 {
		return text
	}
	return strings.TrimSpace(string([]rune(text)[:160])) + "..."
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
