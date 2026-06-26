package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

func kbFixture(t *testing.T) (*KBHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	return NewKBHandler(db), db
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func TestKBListArticlesPublishedOnly(t *testing.T) {
	h, db := kbFixture(t)
	defer db.Close()
	mustExec(t, db, `INSERT INTO escalated_articles (title, slug, body, status, published_at)
		VALUES ('Pub','pub','body','published',CURRENT_TIMESTAMP), ('Draft','draft','body','draft',NULL)`)

	rec := httptest.NewRecorder()
	h.ListArticles(rec, httptest.NewRequest(http.MethodGet, "/api/kb/articles", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Data []models.Article `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].Slug != "pub" {
		t.Fatalf("want exactly one published article 'pub', got %+v", body.Data)
	}
}

func TestKBShowArticleIncrementsViewsAndHidesDrafts(t *testing.T) {
	h, db := kbFixture(t)
	defer db.Close()
	mustExec(t, db, `INSERT INTO escalated_articles (title, slug, body, status, published_at, view_count)
		VALUES ('Pub','pub','b','published',CURRENT_TIMESTAMP,0)`)
	mustExec(t, db, `INSERT INTO escalated_articles (title, slug, body, status) VALUES ('Draft','draft','b','draft')`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/kb/articles/pub", nil)
	req.SetPathValue("slug", "pub")
	h.ShowArticle(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("published: want 200, got %d", rec.Code)
	}
	var body struct {
		Data models.Article `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.ViewCount != 1 {
		t.Fatalf("want view_count 1 after show, got %d", body.Data.ViewCount)
	}

	for _, slug := range []string{"draft", "unknown"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/kb/articles/"+slug, nil)
		req.SetPathValue("slug", slug)
		h.ShowArticle(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: want 404, got %d", slug, rec.Code)
		}
	}
}

func TestKBListCategoriesOrdered(t *testing.T) {
	h, db := kbFixture(t)
	defer db.Close()
	mustExec(t, db, `INSERT INTO escalated_article_categories (name, slug, position)
		VALUES ('Billing','billing',1), ('General','general',0)`)

	rec := httptest.NewRecorder()
	h.ListCategories(rec, httptest.NewRequest(http.MethodGet, "/api/kb/categories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	var body struct {
		Data []models.ArticleCategory `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Data[0].Slug != "general" {
		t.Fatalf("want ordered [general, billing], got %+v", body.Data)
	}
}
