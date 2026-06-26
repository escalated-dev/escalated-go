package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/models"
)

// KBHandler serves the public knowledge-base read endpoints under /api/kb.
// Only published articles are exposed.
type KBHandler struct {
	DB *sql.DB
}

// NewKBHandler creates a KBHandler.
func NewKBHandler(db *sql.DB) *KBHandler {
	return &KBHandler{DB: db}
}

// ListArticles handles GET /api/kb/articles — published articles, with
// optional ?search (title/body) and ?category filters.
func (h *KBHandler) ListArticles(w http.ResponseWriter, r *http.Request) {
	q := `SELECT id, category_id, title, slug, body, status, view_count, helpful_count, not_helpful_count, published_at, created_at, updated_at
		FROM escalated_articles WHERE status = ?`
	args := []any{models.ArticleStatusPublished}

	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		q += " AND (title LIKE ? OR body LIKE ?)"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if cat := r.URL.Query().Get("category"); cat != "" {
		q += " AND category_id = ?"
		args = append(args, cat)
	}
	q += " ORDER BY published_at DESC, id DESC"

	rows, err := h.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	articles := []models.Article{}
	for rows.Next() {
		a, err := scanArticle(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		articles = append(articles, a)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": articles})
}

// ListCategories handles GET /api/kb/categories.
func (h *KBHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, name, slug, parent_id, position, description, created_at, updated_at
		FROM escalated_article_categories ORDER BY position ASC, name ASC`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	categories := []models.ArticleCategory{}
	for rows.Next() {
		var c models.ArticleCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.ParentID, &c.Position, &c.Description, &c.CreatedAt, &c.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": categories})
}

// ShowArticle handles GET /api/kb/articles/{slug} — published article by slug;
// records a view.
func (h *KBHandler) ShowArticle(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		slug = chi.URLParam(r, "slug")
	}
	if slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug is required"})
		return
	}

	row := h.DB.QueryRowContext(r.Context(),
		`SELECT id, category_id, title, slug, body, status, view_count, helpful_count, not_helpful_count, published_at, created_at, updated_at
		FROM escalated_articles WHERE slug = ? AND status = ?`, slug, models.ArticleStatusPublished)

	a, err := scanArticle(row)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "article not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE escalated_articles SET view_count = view_count + 1 WHERE id = ?`, a.ID); err == nil {
		a.ViewCount++
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": a})
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanArticle(s scanner) (models.Article, error) {
	var a models.Article
	err := s.Scan(&a.ID, &a.CategoryID, &a.Title, &a.Slug, &a.Body, &a.Status,
		&a.ViewCount, &a.HelpfulCount, &a.NotHelpfulCount, &a.PublishedAt, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}
