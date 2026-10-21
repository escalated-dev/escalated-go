package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

	// Pages serves Escalated/Admin/KnowledgeBase/Articles/Index and Form and
	// Categories/Index, and answers their writes the Inertia way. Set by the
	// router.
	Pages PageSupport
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

// formID is an optional id as forms and API clients send it: a number, a
// numeric string, or "" / null for none.
type formID struct {
	Set bool
	ID  *int64
}

// UnmarshalJSON accepts 3, "3", "" and null.
func (f *formID) UnmarshalJSON(b []byte) error {
	f.Set = true
	b = bytes.TrimSpace(b)
	if string(b) == "null" || string(b) == `""` {
		f.ID = nil
		return nil
	}
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var id int64
	switch v := raw.(type) {
	case float64:
		id = int64(v)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return fmt.Errorf("not an id: %q", v)
		}
		id = n
	default:
		return fmt.Errorf("not an id")
	}
	f.ID = &id
	return nil
}

func (h *ArticleHandler) articlesURL() string   { return h.Pages.path("/admin/kb-articles") }
func (h *ArticleHandler) categoriesURL() string { return h.Pages.path("/admin/kb-categories") }

// categoryOptions is ArticleCategory::ordered()->get(['id', 'name']).
func (h *ArticleHandler) categoryOptions() []map[string]any {
	out := []map[string]any{}
	categories, err := h.Service.ListCategories()
	if err != nil {
		return out
	}
	for _, c := range categories {
		out = append(out, map[string]any{"id": c.ID, "name": c.Name})
	}
	return out
}

// articleProps is an article as the admin pages read it.
func (h *ArticleHandler) articleProps(r *http.Request, a models.Article, names map[int64]string) map[string]any {
	var category any
	if a.CategoryID != nil {
		if name, ok := names[*a.CategoryID]; ok {
			category = map[string]any{"id": *a.CategoryID, "name": name}
		}
	}
	return map[string]any{
		"id":                a.ID,
		"title":             a.Title,
		"slug":              a.Slug,
		"body":              a.Body,
		"status":            a.Status,
		"category_id":       a.CategoryID,
		"category":          category,
		"author_id":         a.AuthorID,
		"author":            h.Pages.creator(r, a.AuthorID),
		"view_count":        a.ViewCount,
		"helpful_count":     a.HelpfulCount,
		"not_helpful_count": a.NotHelpfulCount,
		"published_at":      uiTime(a.PublishedAt),
		"created_at":        uiTimeValue(a.CreatedAt),
		"updated_at":        uiTimeValue(a.UpdatedAt),
	}
}

func (h *ArticleHandler) categoryNames() map[int64]string {
	names := map[int64]string{}
	if categories, err := h.Service.ListCategories(); err == nil {
		for _, c := range categories {
			names[c.ID] = c.Name
		}
	}
	return names
}

// AdminList handles GET /admin/kb/articles — all articles (drafts included),
// with optional ?search, ?status and ?category_id filters. A page visit gets
// Escalated/Admin/KnowledgeBase/Articles/Index, 20 a page.
func (h *ArticleHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := services.ArticleFilters{
		Search: q.Get("search"),
		Status: q.Get("status"),
	}
	if cat := q.Get("category_id"); cat != "" {
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
	if !h.Pages.wantsPage(r) {
		writeJSON(w, http.StatusOK, map[string]any{"data": articles})
		return
	}

	pg := uiPageFrom(q, 20)
	names := h.categoryNames()
	rows := []map[string]any{}
	for i := pg.offset(); i < len(articles) && i < pg.offset()+pg.PerPage; i++ {
		rows = append(rows, h.articleProps(r, articles[i], names))
	}
	filters := map[string]string{}
	for _, key := range []string{"search", "status", "category_id"} {
		if q.Has(key) {
			filters[key] = q.Get(key)
		}
	}
	h.Pages.render(w, r, "Escalated/Admin/KnowledgeBase/Articles/Index", map[string]any{
		"articles":   uiPaginator(rows, len(articles), pg, r.URL.Path),
		"categories": h.categoryOptions(),
		"filters":    filters,
	}, nil)
}

// CreateForm renders Escalated/Admin/KnowledgeBase/Articles/Form for a new
// article (GET /admin/kb-articles/create).
func (h *ArticleHandler) CreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderForm(w, r, nil, nil)
}

// EditForm renders the article form with the article
// (GET /admin/kb-articles/{id}/edit).
func (h *ArticleHandler) EditForm(w http.ResponseWriter, r *http.Request) {
	a, ok := h.articleFromPath(w, r)
	if !ok {
		return
	}
	h.renderForm(w, r, a, nil)
}

func (h *ArticleHandler) renderForm(w http.ResponseWriter, r *http.Request, a *models.Article, errs map[string]string) {
	props := map[string]any{"categories": h.categoryOptions()}
	if a != nil {
		props["article"] = h.articleProps(r, *a, h.categoryNames())
	}
	h.Pages.render(w, r, "Escalated/Admin/KnowledgeBase/Articles/Form", props, errs)
}

func (h *ArticleHandler) articleFromPath(w http.ResponseWriter, r *http.Request) (*models.Article, bool) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return nil, false
	}
	a, err := h.Service.FindArticleByID(id)
	if err == sql.ErrNoRows || (err == nil && a == nil) {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	return a, true
}

// Show handles GET /admin/kb/articles/{id} — a single article (draft or not).
func (h *ArticleHandler) Show(w http.ResponseWriter, r *http.Request) {
	a, ok := h.articleFromPath(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": a})
}

// Create handles POST /admin/kb/articles (and /admin/kb-articles).
func (h *ArticleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title      string `json:"title"`
		Slug       string `json:"slug"`
		Body       string `json:"body"`
		Status     string `json:"status"`
		CategoryID formID `json:"category_id"`
	}
	rerender := func(errs map[string]string) { h.renderForm(w, r, nil, errs) }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		h.Pages.fail(w, r, http.StatusUnprocessableEntity, "title is required", map[string]string{"title": requiredMessage("title")}, rerender)
		return
	}
	if in.Status == "" {
		in.Status = models.ArticleStatusDraft
	}
	if !validArticleStatus(in.Status) {
		h.Pages.fail(w, r, http.StatusUnprocessableEntity, "status must be draft or published", map[string]string{"status": "The selected status is invalid."}, rerender)
		return
	}
	if msg, code := h.categoryCheck(in.CategoryID.ID); msg != "" {
		h.Pages.fail(w, r, code, msg, map[string]string{"category_id": "The selected category is invalid."}, rerender)
		return
	}

	author := h.Pages.currentUser(r)
	var authorPtr *models.UserID
	if !author.Empty() {
		authorPtr = &author
	}

	a := &models.Article{
		CategoryID: in.CategoryID.ID,
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
	h.Pages.succeed(w, r, h.articlesURL(), http.StatusCreated, map[string]any{"id": a.ID})
}

// Update handles PATCH /admin/kb/articles/{id} (and PUT/PATCH
// /admin/kb-articles/{id}). Fields are optional; omitted fields keep their
// current value. Publishing a draft stamps published_at.
func (h *ArticleHandler) Update(w http.ResponseWriter, r *http.Request) {
	a, ok := h.articleFromPath(w, r)
	if !ok {
		return
	}
	original := *a

	var in struct {
		Title      *string `json:"title"`
		Slug       *string `json:"slug"`
		Body       *string `json:"body"`
		Status     *string `json:"status"`
		CategoryID formID  `json:"category_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	rerender := func(errs map[string]string) { h.renderForm(w, r, &original, errs) }

	if in.Title != nil {
		if strings.TrimSpace(*in.Title) == "" {
			h.Pages.fail(w, r, http.StatusUnprocessableEntity, "title is required", map[string]string{"title": requiredMessage("title")}, rerender)
			return
		}
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
			h.Pages.fail(w, r, http.StatusUnprocessableEntity, "status must be draft or published", map[string]string{"status": "The selected status is invalid."}, rerender)
			return
		}
		a.Status = *in.Status
	}
	if in.CategoryID.Set {
		if msg, code := h.categoryCheck(in.CategoryID.ID); msg != "" {
			h.Pages.fail(w, r, code, msg, map[string]string{"category_id": "The selected category is invalid."}, rerender)
			return
		}
		a.CategoryID = in.CategoryID.ID
	}

	if err := h.Service.UpdateArticle(a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.articlesURL(), http.StatusOK, map[string]any{"id": a.ID})
}

// Delete handles DELETE /admin/kb/articles/{id} (and /admin/kb-articles/{id}).
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
	h.Pages.succeed(w, r, h.articlesURL(), http.StatusNoContent, nil)
}

// --- categories ---

// ListCategories handles GET /admin/kb/categories — categories with counts. A
// page visit gets Escalated/Admin/KnowledgeBase/Categories/Index.
func (h *ArticleHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.Service.ListCategories()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if categories == nil {
		categories = []services.ArticleCategoryWithCount{}
	}
	if h.Pages.wantsPage(r) {
		h.renderCategories(w, r, categories, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": categories})
}

func (h *ArticleHandler) renderCategories(w http.ResponseWriter, r *http.Request, categories []services.ArticleCategoryWithCount, errs map[string]string) {
	if categories == nil {
		categories, _ = h.Service.ListCategories()
	}
	rows := []map[string]any{}
	for _, c := range categories {
		rows = append(rows, map[string]any{
			"id":             c.ID,
			"name":           c.Name,
			"slug":           c.Slug,
			"parent_id":      c.ParentID,
			"position":       c.Position,
			"description":    c.Description,
			"articles_count": c.ArticlesCount,
			"created_at":     uiTimeValue(c.CreatedAt),
			"updated_at":     uiTimeValue(c.UpdatedAt),
		})
	}
	h.Pages.render(w, r, "Escalated/Admin/KnowledgeBase/Categories/Index", map[string]any{"categories": rows}, errs)
}

// categoryInput is what the category form and API clients send. The form
// sends parent_id as "" when there is none.
type categoryInput struct {
	Name        *string `json:"name"`
	Slug        *string `json:"slug"`
	ParentID    formID  `json:"parent_id"`
	Position    *int    `json:"position"`
	Description *string `json:"description"`
}

// CreateCategory handles POST /admin/kb/categories (and /admin/kb-categories).
func (h *ArticleHandler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryInput
	rerender := func(errs map[string]string) { h.renderCategories(w, r, nil, errs) }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		h.Pages.fail(w, r, http.StatusUnprocessableEntity, "name is required", map[string]string{"name": requiredMessage("name")}, rerender)
		return
	}
	if msg, code := h.categoryCheck(in.ParentID.ID); msg != "" {
		h.Pages.fail(w, r, code, msg, map[string]string{"parent_id": "The selected parent is invalid."}, rerender)
		return
	}

	c := &models.ArticleCategory{
		Name:        *in.Name,
		ParentID:    in.ParentID.ID,
		Description: in.Description,
	}
	if in.Slug != nil {
		c.Slug = *in.Slug
	}
	if in.Position != nil {
		c.Position = *in.Position
	}
	if err := h.Service.CreateCategory(c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Pages.succeed(w, r, h.categoriesURL(), http.StatusCreated, map[string]any{"id": c.ID})
}

// UpdateCategory handles PATCH /admin/kb/categories/{id} (and PUT/PATCH
// /admin/kb-categories/{id}).
func (h *ArticleHandler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	c, err := h.Service.FindCategoryByID(id)
	if err == sql.ErrNoRows || (err == nil && c == nil) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var in categoryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	rerender := func(errs map[string]string) { h.renderCategories(w, r, nil, errs) }

	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			h.Pages.fail(w, r, http.StatusUnprocessableEntity, "name is required", map[string]string{"name": requiredMessage("name")}, rerender)
			return
		}
		c.Name = *in.Name
	}
	if in.Slug != nil {
		c.Slug = *in.Slug
	}
	if in.ParentID.Set {
		if in.ParentID.ID != nil && *in.ParentID.ID == c.ID {
			h.Pages.fail(w, r, http.StatusUnprocessableEntity, "a category cannot be its own parent", map[string]string{"parent_id": "A category cannot be its own parent."}, rerender)
			return
		}
		if msg, code := h.categoryCheck(in.ParentID.ID); msg != "" {
			h.Pages.fail(w, r, code, msg, map[string]string{"parent_id": "The selected parent is invalid."}, rerender)
			return
		}
		c.ParentID = in.ParentID.ID
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
	h.Pages.succeed(w, r, h.categoriesURL(), http.StatusOK, map[string]any{"id": c.ID})
}

// DeleteCategory handles DELETE /admin/kb/categories/{id} (and
// /admin/kb-categories/{id}).
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
	h.Pages.succeed(w, r, h.categoriesURL(), http.StatusNoContent, nil)
}

// categoryCheck validates an optional category/parent foreign key, mirroring
// Laravel's "exists" rule: a nil id is allowed; an id with no category answers
// 422.
func (h *ArticleHandler) categoryCheck(id *int64) (string, int) {
	if id == nil {
		return "", 0
	}
	ok, err := h.Service.CategoryExists(*id)
	if err != nil {
		return err.Error(), http.StatusInternalServerError
	}
	if !ok {
		return "category does not exist", http.StatusUnprocessableEntity
	}
	return "", 0
}
