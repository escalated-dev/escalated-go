package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// SettingKnowledgeBaseFeedbackEnabled switches the "was this helpful?" votes
// on the reader pages, as in Laravel. On unless set off.
const SettingKnowledgeBaseFeedbackEnabled = "knowledge_base_feedback_enabled"

// KBPages serves the knowledge base reader pages (Laravel
// Customer\KnowledgeBaseController): Escalated/Customer/KnowledgeBase/Index
// and Article, and the helpfulness vote. Mount it behind the knowledge base
// enabled/public guard.
type KBPages struct {
	DB    *sql.DB
	Store store.Store
	Pages PageSupport
}

func (h *KBPages) feedbackEnabled(r *http.Request) bool {
	return settingBool(r.Context(), h.Store, SettingKnowledgeBaseFeedbackEnabled, true)
}

func (h *KBPages) categoryNames(r *http.Request) map[int64]string {
	names := map[int64]string{}
	rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB, `SELECT id, name FROM escalated_article_categories`))
	if err != nil {
		return names
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			names[id] = name
		}
	}
	return names
}

// readerArticle is a published article as the reader pages show it.
func readerArticle(a models.Article, names map[int64]string) map[string]any {
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
		"category_id":       a.CategoryID,
		"category":          category,
		"view_count":        a.ViewCount,
		"helpful_count":     a.HelpfulCount,
		"not_helpful_count": a.NotHelpfulCount,
		"published_at":      uiTime(a.PublishedAt),
		"created_at":        uiTimeValue(a.CreatedAt),
		"updated_at":        uiTimeValue(a.UpdatedAt),
	}
}

const readerArticleColumns = `id, category_id, title, slug, body, status, view_count, helpful_count, not_helpful_count, published_at, created_at, updated_at`

// Index handles GET /kb: the root categories with their published article
// counts, and the published articles (search, category), 15 a page.
func (h *KBPages) Index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	categories := []map[string]any{}
	rows, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, `SELECT c.id, c.name, c.slug, c.description, c.position,
			(SELECT COUNT(*) FROM escalated_articles a WHERE a.category_id = c.id AND a.status = ?) AS articles_count
		 FROM escalated_article_categories c
		WHERE c.parent_id IS NULL
		ORDER BY c.position ASC, c.name ASC`), models.ArticleStatusPublished)
	if err != nil {
		http.Error(w, "could not list categories", http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var id int64
		var name, slug string
		var description sql.NullString
		var position, count int
		if err := rows.Scan(&id, &name, &slug, &description, &position, &count); err != nil {
			_ = rows.Close()
			http.Error(w, "could not list categories", http.StatusInternalServerError)
			return
		}
		var desc any
		if description.Valid {
			desc = description.String
		}
		categories = append(categories, map[string]any{
			"id": id, "name": name, "slug": slug, "description": desc, "position": position, "articles_count": count,
		})
	}
	_ = rows.Close()

	where := ` WHERE status = ?`
	args := []any{models.ArticleStatusPublished}
	if search := strings.TrimSpace(q.Get("search")); search != "" {
		where += ` AND (title LIKE ? OR body LIKE ?)`
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if cat := q.Get("category"); cat != "" {
		id, err := strconv.ParseInt(cat, 10, 64)
		if err != nil {
			id = 0
		}
		where += ` AND category_id = ?`
		args = append(args, id)
	}

	var total int
	if err := ticketaccess.QueryRowContext(ctx, h.DB, sqldialect.Rebind(h.DB, `SELECT COUNT(*) FROM escalated_articles`+where), args...).Scan(&total); err != nil {
		http.Error(w, "could not list articles", http.StatusInternalServerError)
		return
	}
	pg := uiPageFrom(q, 15)
	// Read the names before opening the list: a single-connection database
	// cannot run a second query while the list's rows are open.
	names := h.categoryNames(r)
	list, err := ticketaccess.QueryContext(ctx, h.DB, sqldialect.Rebind(h.DB, fmt.Sprintf(
		`SELECT %s FROM escalated_articles%s ORDER BY published_at DESC, id DESC LIMIT %d OFFSET %d`,
		readerArticleColumns, where, pg.PerPage, pg.offset())), args...)
	if err != nil {
		http.Error(w, "could not list articles", http.StatusInternalServerError)
		return
	}
	articles := []map[string]any{}
	for list.Next() {
		a, err := scanArticle(list)
		if err != nil {
			_ = list.Close()
			http.Error(w, "could not list articles", http.StatusInternalServerError)
			return
		}
		articles = append(articles, readerArticle(a, names))
	}
	_ = list.Close()

	filters := map[string]string{}
	for _, key := range []string{"search", "category"} {
		if q.Has(key) {
			filters[key] = q.Get(key)
		}
	}
	h.Pages.render(w, r, "Escalated/Customer/KnowledgeBase/Index", map[string]any{
		"categories":      categories,
		"articles":        uiPaginator(articles, total, pg, r.URL.Path),
		"filters":         filters,
		"feedbackEnabled": h.feedbackEnabled(r),
	}, nil)
}

func (h *KBPages) published(r *http.Request, slug string) (*models.Article, error) {
	a, err := scanArticle(ticketaccess.QueryRowContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`SELECT `+readerArticleColumns+` FROM escalated_articles WHERE slug = ? AND status = ?`), slug, models.ArticleStatusPublished))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Article handles GET /kb/{slug}: a published article (its view counted), up
// to five others from its category, and whether votes are on.
func (h *KBPages) Article(w http.ResponseWriter, r *http.Request) {
	a, err := h.published(r, pathParam(r, "slug"))
	if err != nil {
		http.Error(w, "could not load the article", http.StatusInternalServerError)
		return
	}
	if a == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`UPDATE escalated_articles SET view_count = view_count + 1 WHERE id = ?`), a.ID); err == nil {
		a.ViewCount++
	}

	related := []map[string]any{}
	if a.CategoryID != nil {
		rows, err := ticketaccess.QueryContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
			`SELECT id, title, slug FROM escalated_articles WHERE status = ? AND category_id = ? AND id <> ? ORDER BY published_at DESC, id DESC LIMIT 5`),
			models.ArticleStatusPublished, *a.CategoryID, a.ID)
		if err == nil {
			for rows.Next() {
				var id int64
				var title, slug string
				if rows.Scan(&id, &title, &slug) == nil {
					related = append(related, map[string]any{"id": id, "title": title, "slug": slug})
				}
			}
			_ = rows.Close()
		}
	}

	h.Pages.render(w, r, "Escalated/Customer/KnowledgeBase/Article", map[string]any{
		"article":         readerArticle(*a, h.categoryNames(r)),
		"related":         related,
		"feedbackEnabled": h.feedbackEnabled(r),
	}, nil)
}

// Feedback handles POST /kb/{slug}/feedback with helpful: true or false.
func (h *KBPages) Feedback(w http.ResponseWriter, r *http.Request) {
	if !h.feedbackEnabled(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	slug := pathParam(r, "slug")
	a, err := h.published(r, slug)
	if err != nil {
		http.Error(w, "could not load the article", http.StatusInternalServerError)
		return
	}
	if a == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	var helpful bool
	switch strings.ToLower(strings.TrimSpace(in.Get("helpful"))) {
	case "1", "true":
		helpful = true
	case "0", "false":
		helpful = false
	default:
		validationErrors(w, r, map[string]string{"helpful": requiredMessage("helpful")}, func(errs map[string]string) {
			h.Pages.render(w, r, "Escalated/Customer/KnowledgeBase/Article", map[string]any{
				"article":         readerArticle(*a, h.categoryNames(r)),
				"related":         []any{},
				"feedbackEnabled": true,
			}, errs)
		})
		return
	}
	column := "not_helpful_count"
	if helpful {
		column = "helpful_count"
	}
	if _, err := ticketaccess.ExecContext(r.Context(), h.DB, sqldialect.Rebind(h.DB,
		`UPDATE escalated_articles SET `+column+` = `+column+` + 1 WHERE id = ?`), a.ID); err != nil {
		http.Error(w, "could not record the vote", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.Pages.path("/kb/"+url.PathEscape(slug)))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}
