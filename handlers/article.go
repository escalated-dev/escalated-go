package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// ArticleHandler exposes the admin knowledge-base authoring surface: CRUD over
// articles and their categories. The public read endpoints live on
// KBHandler (/api/kb/...); this is the admin counterpart (/admin/kb/...).
//
// Mirrors the Laravel Admin\ArticleController / Admin\ArticleCategoryController.
// Slug defaulting and the publish timestamp live in ArticleService so they hold
// regardless of caller.
type ArticleHandler struct {
	DB      *sql.DB
	Service *services.ArticleService
}

// NewArticleHandler constructs the handler.
func NewArticleHandler(db *sql.DB, svc *services.ArticleService) *ArticleHandler {
	if svc == nil {
		svc = services.NewArticleService(db, nil)
	}
	return &ArticleHandler{DB: db, Service: svc}
}

func validArticleStatus(s string) bool {
	return s == models.ArticleStatusDraft || s == models.ArticleStatusPublished
}

// AdminList handles GET /admin/kb/articles — all articles (drafts included),
// with optional ?search, ?status and ?category_id filters.
func (h *ArticleHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	f := services.ArticleFilters{
		Search: r.URL.Query().Get("search"),
		Status: r.URL.Query().Get("status"),
	}
	if cat := r.URL.Query().Get("category_id"); cat != "" {
		if id, err := strconv.ParseInt(cat, 10, 64); err == nil {
			f.CategoryID = &id
		}
	}
	articles, err := h.Service.ListArticles(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if articles == nil {
		articles = []models.Article{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": articles})
}

// Show handles GET /admin/kb/articles/{id} — a single article (draft or not).
func (h *ArticleHandler) Show(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	a, err := h.Service.FindArticleByID(id)
	if err == sql.ErrNoRows || a == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": a})
}

// Create handles POST /admin/kb/articles.
func (h *ArticleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title      string `json:"title"`
		Slug       string `json:"slug"`
		Body       string `json:"body"`
		Status     string `json:"status"`
		CategoryID *int64 `json:"category_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if in.Title == "" {
		http.Error(w, "title is required", http.StatusUnprocessableEntity)
		return
	}
	if in.Status == "" {
		in.Status = models.ArticleStatusDraft
	}
	if !validArticleStatus(in.Status) {
		http.Error(w, "status must be draft or published", http.StatusUnprocessableEntity)
		return
	}
	if !h.categoryOK(w, in.CategoryID) {
		return
	}

	author := currentAgentID(r)
	var authorPtr *models.UserID
	if !author.Empty() {
		authorPtr = &author
	}

	a := &models.Article{
		CategoryID: in.CategoryID,
		Title:      in.Title,
		Slug:       in.Slug,
		Body:       in.Body,
		Status:     in.Status,
		AuthorID:   authorPtr,
	}
	if err := h.Service.CreateArticle(a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": a.ID})
}

// Update handles PATCH /admin/kb/articles/{id}. Fields are optional; omitted
// fields keep their current value. Publishing a draft stamps published_at.
func (h *ArticleHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	a, err := h.Service.FindArticleByID(id)
	if err == sql.ErrNoRows || a == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var in struct {
		Title      *string `json:"title"`
		Slug       *string `json:"slug"`
		Body       *string `json:"body"`
		Status     *string `json:"status"`
		CategoryID *int64  `json:"category_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if in.Title != nil {
		a.Title = *in.Title
	}
	if in.Slug != nil {
		a.Slug = *in.Slug
	}
	if in.Body != nil {
		a.Body = *in.Body
	}
	if in.Status != nil {
		if !validArticleStatus(*in.Status) {
			http.Error(w, "status must be draft or published", http.StatusUnprocessableEntity)
			return
		}
		a.Status = *in.Status
	}
	if in.CategoryID != nil {
		if !h.categoryOK(w, in.CategoryID) {
			return
		}
		a.CategoryID = in.CategoryID
	}

	if err := h.Service.UpdateArticle(a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": a.ID})
}

// Delete handles DELETE /admin/kb/articles/{id}.
func (h *ArticleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.Service.DeleteArticle(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- categories ---

// ListCategories handles GET /admin/kb/categories — categories with counts.
func (h *ArticleHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.Service.ListCategories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if categories == nil {
		categories = []services.ArticleCategoryWithCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": categories})
}

// CreateCategory handles POST /admin/kb/categories.
func (h *ArticleHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string  `json:"name"`
		Slug        string  `json:"slug"`
		ParentID    *int64  `json:"parent_id"`
		Position    *int    `json:"position"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if in.Name == "" {
		http.Error(w, "name is required", http.StatusUnprocessableEntity)
		return
	}
	if !h.categoryOK(w, in.ParentID) {
		return
	}

	c := &models.ArticleCategory{
		Name:        in.Name,
		Slug:        in.Slug,
		ParentID:    in.ParentID,
		Description: in.Description,
	}
	if in.Position != nil {
		c.Position = *in.Position
	}
	if err := h.Service.CreateCategory(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": c.ID})
}

// UpdateCategory handles PATCH /admin/kb/categories/{id}.
func (h *ArticleHandler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := h.Service.FindCategoryByID(id)
	if err == sql.ErrNoRows || c == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var in struct {
		Name        *string `json:"name"`
		Slug        *string `json:"slug"`
		ParentID    *int64  `json:"parent_id"`
		Position    *int    `json:"position"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if in.Name != nil {
		c.Name = *in.Name
	}
	if in.Slug != nil {
		c.Slug = *in.Slug
	}
	if in.ParentID != nil {
		if !h.categoryOK(w, in.ParentID) {
			return
		}
		c.ParentID = in.ParentID
	}
	if in.Position != nil {
		c.Position = *in.Position
	}
	if in.Description != nil {
		c.Description = in.Description
	}

	if err := h.Service.UpdateCategory(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": c.ID})
}

// DeleteCategory handles DELETE /admin/kb/categories/{id}.
func (h *ArticleHandler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.Service.DeleteCategory(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// categoryOK validates an optional category/parent foreign key. It writes a
// 422 (and returns false) when a non-nil id does not resolve to an existing
// category, mirroring Laravel's "exists" rule. A nil id is always allowed.
func (h *ArticleHandler) categoryOK(w http.ResponseWriter, id *int64) bool {
	if id == nil {
		return true
	}
	ok, err := h.Service.CategoryExists(*id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Error(w, "category does not exist", http.StatusUnprocessableEntity)
		return false
	}
	return true
}
