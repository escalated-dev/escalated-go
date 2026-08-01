package services

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

func newArticleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// Single connection so the in-memory schema persists across queries.
	db.SetMaxOpenConns(1)
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// CreateArticle defaults a blank slug from the title, assigns an id, and a
// draft gets no publish timestamp. FindArticleByID round-trips every field.
func TestArticleCreateAndFind(t *testing.T) {
	svc := NewArticleService(newArticleTestDB(t), nil)

	author := models.UserID("42")
	a := &models.Article{
		Title:    "Getting Started",
		Body:     "Welcome to the docs.",
		Status:   models.ArticleStatusDraft,
		AuthorID: &author,
	}
	if err := svc.CreateArticle(a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("expected a non-zero id after create")
	}
	if a.Slug != "getting-started" {
		t.Errorf("expected slug defaulted to 'getting-started', got %q", a.Slug)
	}

	got, err := svc.FindArticleByID(a.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Title != "Getting Started" || got.Body != "Welcome to the docs." {
		t.Errorf("title/body mismatch: %+v", got)
	}
	if got.Status != models.ArticleStatusDraft {
		t.Errorf("expected draft, got %q", got.Status)
	}
	if got.PublishedAt != nil {
		t.Errorf("draft must have no published_at, got %v", got.PublishedAt)
	}
	if got.AuthorID == nil || *got.AuthorID != author {
		t.Errorf("author mismatch: %v", got.AuthorID)
	}
}

// Publishing a draft stamps published_at on the first transition and leaves it
// unchanged on subsequent updates.
func TestArticlePublishToggle(t *testing.T) {
	svc := NewArticleService(newArticleTestDB(t), nil)

	a := &models.Article{Title: "Draft Piece", Status: models.ArticleStatusDraft}
	if err := svc.CreateArticle(a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.PublishedAt != nil {
		t.Fatalf("draft must have no published_at, got %v", a.PublishedAt)
	}

	// Transition draft -> published.
	a.Status = models.ArticleStatusPublished
	if err := svc.UpdateArticle(a); err != nil {
		t.Fatalf("publish: %v", err)
	}
	published, err := svc.FindArticleByID(a.ID)
	if err != nil {
		t.Fatalf("find after publish: %v", err)
	}
	if !published.IsPublished() {
		t.Errorf("expected published status, got %q", published.Status)
	}
	if published.PublishedAt == nil {
		t.Fatal("expected published_at to be stamped on publish")
	}
	firstPublished := *published.PublishedAt

	// A later edit that is still published must not move published_at.
	published.Body = "expanded"
	if err := svc.UpdateArticle(published); err != nil {
		t.Fatalf("second update: %v", err)
	}
	again, err := svc.FindArticleByID(a.ID)
	if err != nil {
		t.Fatalf("find after second update: %v", err)
	}
	if again.PublishedAt == nil || !again.PublishedAt.Equal(firstPublished) {
		t.Errorf("published_at must be preserved: first=%v again=%v", firstPublished, again.PublishedAt)
	}
}

// An article can be filed under a category; CategoryExists validates the FK and
// ListCategories reports the article count.
func TestArticleCategoryAssign(t *testing.T) {
	svc := NewArticleService(newArticleTestDB(t), nil)

	cat := &models.ArticleCategory{Name: "Billing & Plans"}
	if err := svc.CreateCategory(cat); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if cat.ID == 0 {
		t.Fatal("expected a non-zero category id")
	}
	if cat.Slug != "billing--plans" {
		t.Errorf("expected slug defaulted from name, got %q", cat.Slug)
	}

	a := &models.Article{Title: "Invoices", Status: models.ArticleStatusDraft, CategoryID: &cat.ID}
	if err := svc.CreateArticle(a); err != nil {
		t.Fatalf("create article: %v", err)
	}
	got, err := svc.FindArticleByID(a.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != cat.ID {
		t.Errorf("expected category_id %d, got %v", cat.ID, got.CategoryID)
	}

	if ok, err := svc.CategoryExists(cat.ID); err != nil || !ok {
		t.Errorf("CategoryExists(existing) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := svc.CategoryExists(9999); err != nil || ok {
		t.Errorf("CategoryExists(missing) = %v, %v; want false, nil", ok, err)
	}

	cats, err := svc.ListCategories()
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 1 {
		t.Fatalf("expected 1 category, got %d", len(cats))
	}
	if cats[0].ArticlesCount != 1 {
		t.Errorf("expected articles_count 1, got %d", cats[0].ArticlesCount)
	}
}

// Filters narrow the listing by status and search term.
func TestArticleListFilters(t *testing.T) {
	svc := NewArticleService(newArticleTestDB(t), nil)

	must := func(title, status string) {
		t.Helper()
		if err := svc.CreateArticle(&models.Article{Title: title, Body: title + " body", Status: status}); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
	}
	must("Refund policy", models.ArticleStatusPublished)
	must("Refund draft", models.ArticleStatusDraft)
	must("Shipping", models.ArticleStatusPublished)

	published, err := svc.ListArticles(ArticleFilters{Status: models.ArticleStatusPublished})
	if err != nil {
		t.Fatalf("list published: %v", err)
	}
	if len(published) != 2 {
		t.Errorf("expected 2 published, got %d", len(published))
	}

	refunds, err := svc.ListArticles(ArticleFilters{Search: "Refund"})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if len(refunds) != 2 {
		t.Errorf("expected 2 matching 'Refund', got %d", len(refunds))
	}
}

// Update persists edits; Delete removes the row (FindArticleByID then errors).
func TestArticleUpdateAndDelete(t *testing.T) {
	svc := NewArticleService(newArticleTestDB(t), nil)

	a := &models.Article{Title: "Temp", Body: "b", Status: models.ArticleStatusDraft}
	if err := svc.CreateArticle(a); err != nil {
		t.Fatalf("create: %v", err)
	}

	a.Title = "Renamed"
	a.Slug = "" // force re-default from the new title
	if err := svc.UpdateArticle(a); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := svc.FindArticleByID(a.ID)
	if err != nil {
		t.Fatalf("find after update: %v", err)
	}
	if got.Title != "Renamed" || got.Slug != "renamed" {
		t.Errorf("update not persisted: %+v", got)
	}

	if err := svc.DeleteArticle(a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.FindArticleByID(a.ID); err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

// Category update persists edits; delete removes the row.
func TestArticleCategoryUpdateAndDelete(t *testing.T) {
	svc := NewArticleService(newArticleTestDB(t), nil)

	c := &models.ArticleCategory{Name: "General", Description: strptr("misc")}
	if err := svc.CreateCategory(c); err != nil {
		t.Fatalf("create: %v", err)
	}

	c.Name = "Support"
	c.Position = 5
	if err := svc.UpdateCategory(c); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := svc.FindCategoryByID(c.ID)
	if err != nil {
		t.Fatalf("find after update: %v", err)
	}
	if got.Name != "Support" || got.Position != 5 {
		t.Errorf("update not persisted: %+v", got)
	}
	if got.Description == nil || *got.Description != "misc" {
		t.Errorf("description mismatch: %v", got.Description)
	}

	if err := svc.DeleteCategory(c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.FindCategoryByID(c.ID); err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}
}
