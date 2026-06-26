package models

import "time"

// Article status values.
const (
	ArticleStatusDraft     = "draft"
	ArticleStatusPublished = "published"
)

// Article represents a knowledge base article. Mirrors the Laravel Article
// model: draft or published, optionally filed under a category, with view and
// helpfulness counters.
type Article struct {
	ID              int64      `json:"id"`
	CategoryID      *int64     `json:"category_id,omitempty"`
	Title           string     `json:"title"`
	Slug            string     `json:"slug"`
	Body            string     `json:"body"`
	Status          string     `json:"status"`
	AuthorID        *UserID    `json:"author_id,omitempty"`
	ViewCount       int        `json:"view_count"`
	HelpfulCount    int        `json:"helpful_count"`
	NotHelpfulCount int        `json:"not_helpful_count"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// IsPublished reports whether the article is published.
func (a Article) IsPublished() bool {
	return a.Status == ArticleStatusPublished
}

// ArticleCategory represents a knowledge base article category, optionally
// nested under a parent. Mirrors the Laravel ArticleCategory model.
type ArticleCategory struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	ParentID    *int64    `json:"parent_id,omitempty"`
	Position    int       `json:"position"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
