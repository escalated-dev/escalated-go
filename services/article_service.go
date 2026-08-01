package services

import (
	"database/sql"
	"log"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// ArticleService is the knowledge-base authoring layer: create/update/delete
// for articles and their categories, plus slug defaulting and publish
// timestamps.
//
// The public read path (handlers.KBHandler) queries published articles inline
// and has no write methods, so the admin surface needs its own service. This
// mirrors the Laravel Admin\ArticleController / Admin\ArticleCategoryController:
// a blank slug defaults to a slug of the title/name, and an article gains a
// published_at the moment it first transitions to "published".
//
// Table names follow the same hard-coded "escalated_" prefix the public KB
// handler and the canned-response service use.
type ArticleService struct {
	DB     *sql.DB
	Logger *log.Logger
}

// NewArticleService constructs a service with the given DB and logger (a
// default logger is used when nil).
func NewArticleService(db *sql.DB, logger *log.Logger) *ArticleService {
	if logger == nil {
		logger = log.Default()
	}
	return &ArticleService{DB: db, Logger: logger}
}

// ArticleFilters narrows an article listing. Zero values mean "no filter".
type ArticleFilters struct {
	Search     string // matches title OR body (LIKE)
	Status     string // exact status match (draft/published)
	CategoryID *int64 // exact category match
}

// ArticleCategoryWithCount is an ArticleCategory plus the number of articles
// filed under it, mirroring Laravel's withCount('articles') on the admin
// category index.
type ArticleCategoryWithCount struct {
	models.ArticleCategory
	ArticlesCount int `json:"articles_count"`
}

// articleRowScanner is satisfied by both *sql.Row and *sql.Rows.
type articleRowScanner interface {
	Scan(dest ...any) error
}

// scanAdminArticle reads one article row, including the author_id the public
// scanner omits. Nullable columns (category_id, body, author_id, published_at)
// scan through Null* wrappers so a SQL NULL maps to a zero value / nil pointer.
func scanAdminArticle(sc articleRowScanner) (models.Article, error) {
	var a models.Article
	var (
		categoryID  sql.NullInt64
		body        sql.NullString
		authorID    models.UserID // UserID.Scan maps NULL to ""
		publishedAt sql.NullTime
	)
	if err := sc.Scan(
		&a.ID, &categoryID, &a.Title, &a.Slug, &body, &a.Status, &authorID,
		&a.ViewCount, &a.HelpfulCount, &a.NotHelpfulCount, &publishedAt,
		&a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return a, err
	}
	a.Body = body.String
	if categoryID.Valid {
		v := categoryID.Int64
		a.CategoryID = &v
	}
	if !authorID.Empty() {
		aid := authorID
		a.AuthorID = &aid
	}
	if publishedAt.Valid {
		t := publishedAt.Time
		a.PublishedAt = &t
	}
	return a, nil
}

const adminArticleColumns = `id, category_id, title, slug, body, status, author_id,
	view_count, helpful_count, not_helpful_count, published_at, created_at, updated_at`

// ListArticles returns articles (drafts included) matching the filters, newest
// first. Admin-only; not restricted to published like the public path.
func (s *ArticleService) ListArticles(f ArticleFilters) ([]models.Article, error) {
	q := `SELECT ` + adminArticleColumns + ` FROM escalated_articles WHERE 1 = 1`
	var args []any
	if search := strings.TrimSpace(f.Search); search != "" {
		q += " AND (title LIKE ? OR body LIKE ?)"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.CategoryID != nil {
		q += " AND category_id = ?"
		args = append(args, *f.CategoryID)
	}
	q += " ORDER BY created_at DESC, id DESC"

	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var articles []models.Article
	for rows.Next() {
		a, err := scanAdminArticle(rows)
		if err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

// FindArticleByID returns the article with the given id or sql.ErrNoRows.
func (s *ArticleService) FindArticleByID(id int64) (*models.Article, error) {
	a, err := scanAdminArticle(s.DB.QueryRow(
		`SELECT `+adminArticleColumns+` FROM escalated_articles WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateArticle inserts an article, defaulting a blank slug from the title and
// stamping published_at when it is created already published.
func (s *ArticleService) CreateArticle(a *models.Article) error {
	if a.Slug == "" {
		a.Slug = slugify(a.Title)
	}
	if a.Status == "" {
		a.Status = models.ArticleStatusDraft
	}
	now := time.Now()
	if a.Status == models.ArticleStatusPublished && a.PublishedAt == nil {
		a.PublishedAt = &now
	}
	res, err := s.DB.Exec(
		`INSERT INTO escalated_articles
			(category_id, title, slug, body, status, author_id, published_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.CategoryID, a.Title, a.Slug, a.Body, a.Status, a.AuthorID, a.PublishedAt, now, now,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	a.ID = id
	a.CreatedAt = now
	a.UpdatedAt = now
	return nil
}

// UpdateArticle saves changes to an article. author_id is never reassigned
// (matching Laravel). published_at is stamped only on the first transition to
// published: pass the existing row so an already-set timestamp is preserved.
func (s *ArticleService) UpdateArticle(a *models.Article) error {
	if a.Slug == "" {
		a.Slug = slugify(a.Title)
	}
	now := time.Now()
	if a.Status == models.ArticleStatusPublished && a.PublishedAt == nil {
		a.PublishedAt = &now
	}
	_, err := s.DB.Exec(
		`UPDATE escalated_articles
			SET category_id = ?, title = ?, slug = ?, body = ?, status = ?,
				published_at = ?, updated_at = ?
		 WHERE id = ?`,
		a.CategoryID, a.Title, a.Slug, a.Body, a.Status, a.PublishedAt, now, a.ID,
	)
	if err != nil {
		return err
	}
	a.UpdatedAt = now
	return nil
}

// DeleteArticle removes the article with the given id.
func (s *ArticleService) DeleteArticle(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM escalated_articles WHERE id = ?`, id)
	return err
}

// CategoryExists reports whether a category with the given id exists. Used to
// validate category_id / parent_id foreign keys (the Laravel "exists" rule).
func (s *ArticleService) CategoryExists(id int64) (bool, error) {
	var one int
	err := s.DB.QueryRow(
		`SELECT 1 FROM escalated_article_categories WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// scanCategory reads one category row, mapping nullable parent_id/description.
func scanCategory(sc articleRowScanner, extra ...any) (models.ArticleCategory, error) {
	var c models.ArticleCategory
	var (
		parentID    sql.NullInt64
		description sql.NullString
	)
	dest := []any{
		&c.ID, &c.Name, &c.Slug, &parentID, &c.Position, &description,
		&c.CreatedAt, &c.UpdatedAt,
	}
	dest = append(dest, extra...)
	if err := sc.Scan(dest...); err != nil {
		return c, err
	}
	if parentID.Valid {
		v := parentID.Int64
		c.ParentID = &v
	}
	if description.Valid {
		d := description.String
		c.Description = &d
	}
	return c, nil
}

const categoryColumns = `id, name, slug, parent_id, position, description, created_at, updated_at`

// ListCategories returns every category ordered by position then name, each
// with its article count (mirrors withCount('articles')).
func (s *ArticleService) ListCategories() ([]ArticleCategoryWithCount, error) {
	rows, err := s.DB.Query(
		`SELECT ` + categoryColumns + `,
			(SELECT COUNT(*) FROM escalated_articles a WHERE a.category_id = c.id) AS articles_count
		 FROM escalated_article_categories c
		 ORDER BY position ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ArticleCategoryWithCount
	for rows.Next() {
		var count int
		c, err := scanCategory(rows, &count)
		if err != nil {
			return nil, err
		}
		out = append(out, ArticleCategoryWithCount{ArticleCategory: c, ArticlesCount: count})
	}
	return out, rows.Err()
}

// FindCategoryByID returns the category with the given id or sql.ErrNoRows.
func (s *ArticleService) FindCategoryByID(id int64) (*models.ArticleCategory, error) {
	c, err := scanCategory(s.DB.QueryRow(
		`SELECT `+categoryColumns+` FROM escalated_article_categories WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCategory inserts a category, defaulting a blank slug from the name.
func (s *ArticleService) CreateCategory(c *models.ArticleCategory) error {
	if c.Slug == "" {
		c.Slug = slugify(c.Name)
	}
	now := time.Now()
	res, err := s.DB.Exec(
		`INSERT INTO escalated_article_categories
			(name, slug, parent_id, position, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Slug, c.ParentID, c.Position, c.Description, now, now,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = id
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

// UpdateCategory saves changes to a category, defaulting a blank slug.
func (s *ArticleService) UpdateCategory(c *models.ArticleCategory) error {
	if c.Slug == "" {
		c.Slug = slugify(c.Name)
	}
	now := time.Now()
	_, err := s.DB.Exec(
		`UPDATE escalated_article_categories
			SET name = ?, slug = ?, parent_id = ?, position = ?, description = ?, updated_at = ?
		 WHERE id = ?`,
		c.Name, c.Slug, c.ParentID, c.Position, c.Description, now, c.ID,
	)
	if err != nil {
		return err
	}
	c.UpdatedAt = now
	return nil
}

// DeleteCategory removes the category with the given id.
func (s *ArticleService) DeleteCategory(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM escalated_article_categories WHERE id = ?`, id)
	return err
}

// slugify lower-cases, trims, and hyphenates a string, dropping any character
// that is not a-z, 0-9, or hyphen. Mirrors handlers.slugify (a separate
// package) and Laravel's Str::slug for our inputs.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
