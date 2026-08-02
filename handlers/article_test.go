package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/escalated-dev/escalated-go/migrations"
	"github.com/escalated-dev/escalated-go/models"
)

func articleFixture(t *testing.T) *ArticleHandler {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrations.MigrateSQLite(db, "escalated_"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewArticleHandler(db, nil)
}

// call drives a handler with an optional JSON body and an authenticated agent
// id in context (so Create can stamp author_id).
func call(h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	r = r.WithContext(context.WithValue(r.Context(), ctxKeyUserID{}, models.UserID("42")))
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

// Create -> list -> publish -> fetch exercised through the HTTP surface.
func TestArticleHandlerCreatePublishFlow(t *testing.T) {
	h := articleFixture(t)

	rec := call(h.Create, http.MethodPost, "/admin/kb/articles",
		`{"title":"Reset your password","body":"Steps..."}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("bad create response: %v (%s)", err, rec.Body.String())
	}

	// The draft is listed and carries the author from context.
	rec = call(h.AdminList, http.MethodGet, "/admin/kb/articles", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", rec.Code)
	}
	var listed struct {
		Data []models.Article `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Slug != "reset-your-password" {
		t.Fatalf("unexpected list: %+v", listed.Data)
	}
	if listed.Data[0].AuthorID == nil || *listed.Data[0].AuthorID != models.UserID("42") {
		t.Errorf("author not stamped from context: %v", listed.Data[0].AuthorID)
	}
	if listed.Data[0].Status != models.ArticleStatusDraft {
		t.Errorf("expected draft, got %q", listed.Data[0].Status)
	}

	// Publish it.
	rec = call(h.Update, http.MethodPatch, "/admin/kb/articles/1", `{"status":"published"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: got %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rec = call(h.Show, http.MethodGet, "/admin/kb/articles/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("show: got %d, want 200", rec.Code)
	}
	var shown struct {
		Data models.Article `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &shown); err != nil {
		t.Fatal(err)
	}
	if !shown.Data.IsPublished() || shown.Data.PublishedAt == nil {
		t.Errorf("expected published with a timestamp, got %+v", shown.Data)
	}
}

// A category_id that does not exist is rejected with 422 (the "exists" rule).
func TestArticleHandlerRejectsUnknownCategory(t *testing.T) {
	h := articleFixture(t)

	rec := call(h.Create, http.MethodPost, "/admin/kb/articles",
		`{"title":"Orphan","status":"draft","category_id":999}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("got %d, want 422 for unknown category: %s", rec.Code, rec.Body.String())
	}
}

// A missing title is rejected; an invalid status is rejected.
func TestArticleHandlerValidation(t *testing.T) {
	h := articleFixture(t)

	if rec := call(h.Create, http.MethodPost, "/admin/kb/articles", `{"body":"no title"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing title: got %d, want 422", rec.Code)
	}
	if rec := call(h.Create, http.MethodPost, "/admin/kb/articles", `{"title":"x","status":"archived"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad status: got %d, want 422", rec.Code)
	}
}
