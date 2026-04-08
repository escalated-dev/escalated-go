package models

import "time"

// Article represents a knowledge base article used by the widget.
type Article struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Slug        string  `json:"slug"`
	Body        string  `json:"body"`
	Excerpt     string  `json:"excerpt,omitempty"`
	CategoryID  *int64  `json:"category_id,omitempty"`
	IsPublished bool    `json:"is_published"`
	ViewCount   int     `json:"view_count"`
	HelpfulYes  int     `json:"helpful_yes"`
	HelpfulNo   int     `json:"helpful_no"`
	AuthorID    *int64  `json:"author_id,omitempty"`
	Position    int     `json:"position"`
	Tags        *string `json:"tags,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
