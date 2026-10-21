package handlers

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/middleware"
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
	AllowedOrigins string `json:"allowed_origins,omitempty"`
	// RateLimitPerMin is the general per-client-IP budget for every widget
	// route, a minute. Zero means Config.GuestRateLimit.RequestsPerMinute
	// (default 60, Laravel's guest_rate_limits.requests_per_minute). Ticket
	// creation and chat start also count against the guest submission limit
	// (default 5), chat messages, ratings and lookups against the guest reply
	// limit (default 10).
	RateLimitPerMin int `json:"-"`
}

// DefaultWidgetConfig returns a widget config with sensible defaults.
// AllowedOrigins starts from the ESCALATED_WIDGET_ALLOWED_ORIGINS environment
// variable (empty when unset).
func DefaultWidgetConfig() WidgetConfig {
	return WidgetConfig{
		Enabled:        false,
		Title:          "Help Center",
		Subtitle:       "How can we help you?",
		PrimaryColor:   "#4F46E5",
		Position:       "bottom-right",
		AllowedOrigins: os.Getenv("ESCALATED_WIDGET_ALLOWED_ORIGINS"),
	}
}

// WidgetHandler serves the public widget endpoints.
type WidgetHandler struct {
	config  WidgetConfig
	store   store.Store
	tickets *services.TicketService

	// Limiter throttles the widget routes per client IP. The constructor gives
	// it an in-memory limiter keyed on RemoteAddr; the routers replace it with
	// one built from Config.GuestRateLimit, so the widget shares the guest
	// routes' client-IP extractor, store and Disabled switch.
	Limiter *middleware.GuestRateLimiter

	// DB is the database holding the knowledge base (escalated_articles).
	// When nil the widget has no knowledge base: /config reports
	// kb_enabled false, searches return no articles and every article is 404.
	// The routers set it from Config.DB.
	DB *sql.DB

	// Access checks guest email codes and grants (verified guest access, see
	// docs/guest-access.md). The routers set it. With it, ticket creation
	// needs a "ticket" code and returns an expiring grant, and the status tab
	// works through /verification, /lookup and GET /tickets/{reference}.
	// Without it (a host wiring the handler itself) creation takes no code and
	// /config reports guest_verification_required false.
	Access *services.GuestAccess

	// SignedIn reports whether the request comes from a signed-in host user.
	// A private knowledge base (knowledge_base_public off) is served only to
	// them. The routers set it; cross-origin embeds always count as guests.
	SignedIn func(r *http.Request) bool
}

// NewWidgetHandler creates a new WidgetHandler.
func NewWidgetHandler(cfg WidgetConfig, s store.Store, ts *services.TicketService) *WidgetHandler {
	return &WidgetHandler{
		config:  cfg,
		store:   s,
		tickets: ts,
		Limiter: newWidgetRateLimiter(cfg),
	}
}

// newWidgetRateLimiter is the limiter a handler built without the routers
// uses: in memory, keyed on RemoteAddr, with RateLimitPerMin as the request
// budget.
func newWidgetRateLimiter(cfg WidgetConfig) *middleware.GuestRateLimiter {
	return middleware.NewGuestRateLimiter(middleware.GuestRateLimitConfig{RequestsPerMinute: cfg.RateLimitPerMin})
}

// Config handles GET /widget/config - returns public widget configuration.
//
// The top-level keys the shared widget reads (enabled, color, position,
// greeting, departments, kb_enabled, guest_tickets_enabled,
// guest_verification_required, status_lookup_enabled) sit alongside the
// original ones (primary_color, welcome_message, ...). With verified guest
// access wired, guest_verification_required and status_lookup_enabled are
// true and the widget runs the email-code step.
func (h *WidgetHandler) Config(w http.ResponseWriter, r *http.Request) {
	if !h.rateLimit(w, r) {
		return
	}
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
		"enabled":                     true,
		"title":                       h.config.Title,
		"subtitle":                    h.config.Subtitle,
		"primary_color":               h.config.PrimaryColor,
		"color":                       h.config.PrimaryColor,
		"position":                    position,
		"logo_url":                    h.config.LogoURL,
		"welcome_message":             h.config.WelcomeMessage,
		"greeting":                    h.config.WelcomeMessage,
		"departments":                 departments,
		"kb_enabled":                  h.kbEnabledFor(r),
		"guest_tickets_enabled":       true,
		"guest_verification_required": h.Access != nil,
		"status_lookup_enabled":       h.Access != nil,
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

	if !h.widgetKBAllowed(w, r) {
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

	if !h.widgetKBAllowed(w, r) {
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
	if !h.rateLimit(w, r, middleware.GuestTicketScope) {
		return
	}

	var in struct {
		Name         string           `json:"name"`
		Email        string           `json:"email"`
		Subject      string           `json:"subject"`
		Description  string           `json:"description"`
		DepartmentID widgetOptionalID `json:"department_id"`
		Metadata     json.RawMessage  `json:"metadata"`
		guestProof
	}
	setGuestPrivacyHeaders(w)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "message": "Invalid request body."})
		return
	}

	missing := requiredWidgetFields(map[string]string{"email": in.Email, "subject": in.Subject, "description": in.Description})
	if len(missing) > 0 {
		writeWidgetValidation(w, "email, subject, and description are required", missing)
		return
	}
	if h.Access != nil {
		errs := in.validate()
		if !services.ValidGuestEmail(in.Email) {
			errs["email"] = []string{"Enter a valid email address."}
		}
		if len(errs) > 0 {
			writeGuestValidation(w, errs)
			return
		}
	}
	if !widgetDepartmentValid(r, h.store, in.DepartmentID.Ptr()) {
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

	input := services.CreateTicketInput{
		Subject:      in.Subject,
		Description:  in.Description,
		GuestName:    &in.Name,
		GuestEmail:   &in.Email,
		DepartmentID: in.DepartmentID.Ptr(),
		Metadata:     widgetMetadataJSON(metadata),
	}
	if h.Access != nil {
		t, token, expires, err := createVerifiedGuestTicket(r.Context(), h.Access, h.tickets, in.guestProof, in.Email, input)
		if !writeGuestAccessError(w, err) {
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"message":            "Ticket created successfully.",
			"reference":          t.Reference,
			"ticket_reference":   t.Reference,
			"guest_access_token": token,
			"expires_at":         expires.Format(time.RFC3339),
		})
		return
	}

	t, err := h.tickets.Create(r.Context(), input)
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
	if !h.rateLimit(w, r, middleware.GuestReplyScope) {
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

	if (h.Access != nil && !h.Access.LegacyTokensAccepted()) || !guestTokenMatches(t.GuestToken, token) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid token"})
		return
	}

	// Public replies, guest-shaped: no author ids or internal flags.
	replies := guestReplies(r.Context(), h.store, t.ID)

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

// TicketStatus handles GET /widget/tickets/{reference} with
// Authorization: Bearer <grant>: the ticket's status and its latest 20 public
// replies (Laravel WidgetController::ticketStatus). The grant must belong to
// the ticket the reference names; anything else is 404.
func (h *WidgetHandler) TicketStatus(w http.ResponseWriter, r *http.Request) {
	setGuestPrivacyHeaders(w)
	if !h.config.Enabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "widget is disabled"})
		return
	}
	if !h.rateLimit(w, r, middleware.GuestReplyScope) {
		return
	}
	notFound := func() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ticket not found", "message": "The link has expired. Verify your email for a new one."})
	}
	token := bearerToken(r)
	if h.Access == nil || token == "" {
		notFound()
		return
	}
	id, err := h.Access.ResolveTicketID(r.Context(), token)
	if err != nil {
		notFound()
		return
	}
	t, err := h.store.GetTicket(r.Context(), id)
	if err != nil || t == nil || subtle.ConstantTimeCompare([]byte(t.Reference), []byte(pathParam(r, "reference"))) != 1 {
		notFound()
		return
	}

	replies := guestReplies(r.Context(), h.store, t.ID)
	// Latest first, at most 20, as Laravel sends them.
	for i, j := 0, len(replies)-1; i < j; i, j = i+1, j-1 {
		replies[i], replies[j] = replies[j], replies[i]
	}
	if len(replies) > 20 {
		replies = replies[:20]
	}
	var department any
	if t.DepartmentID != nil {
		if d, err := h.store.GetDepartment(r.Context(), *t.DepartmentID); err == nil && d != nil {
			department = d.Name
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reference":    t.Reference,
		"subject":      t.Subject,
		"status":       t.StatusString(),
		"status_label": widgetStatusLabel(t.StatusString()),
		"created_at":   t.CreatedAt,
		"department":   department,
		"replies":      replies,
	})
}

// widgetStatusLabel turns a status slug into its label ("in_progress" ->
// "In Progress").
func widgetStatusLabel(slug string) string {
	words := strings.Split(slug, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func (h *WidgetHandler) rateLimit(w http.ResponseWriter, r *http.Request, scopes ...middleware.GuestScope) bool {
	return widgetAllow(w, r, h.Limiter, scopes...)
}

// widgetAllow counts the request against the general widget budget and then
// any extra scopes, answering 429 with Retry-After when one is exhausted.
func widgetAllow(w http.ResponseWriter, r *http.Request, l *middleware.GuestRateLimiter, scopes ...middleware.GuestScope) bool {
	return l.Allow(w, r, append([]middleware.GuestScope{middleware.GuestRequestScope}, scopes...)...)
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

// widgetOptionalID is an optional id from a widget request. embed.js forwards
// data-department="3" as the string "3", so a numeric string is accepted as
// well as a number; null and "" mean absent. Anything else fails decoding.
type widgetOptionalID struct {
	v *int64
}

// Ptr returns the id, or nil when it was absent.
func (o widgetOptionalID) Ptr() *int64 { return o.v }

func (o *widgetOptionalID) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(string(b))
	if raw == "null" {
		o.v = nil
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		raw = strings.TrimSpace(s)
		if raw == "" {
			o.v = nil
			return nil
		}
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("department_id must be an integer: %w", err)
	}
	o.v = &id
	return nil
}

// kbEnabledFor reports whether the widget should offer its knowledge base to
// this caller: there is one, it is enabled, and it is public or they are
// signed in.
func (h *WidgetHandler) kbEnabledFor(r *http.Request) bool {
	if h.DB == nil {
		return false
	}
	kb := KBSettingsFromStore(r.Context(), h.store)
	return kb.KnowledgeBaseEnabled && (kb.Public || h.signedIn(r))
}
