package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

// The knowledge base pages: the admin article and category screens (Laravel
// Admin\ArticleController, ArticleCategoryController) and the reader pages
// (Customer\KnowledgeBaseController). The forms post JSON with ids as ""
// when unset; the reader pages follow the knowledge base settings.

func (f *uiFixture) id(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	db := f.esc.Config.DB
	var id int64
	if err := db.QueryRow(sqldialect.Rebind(db, query), args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

func TestAdminKnowledgeBasePages(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			categories := "/escalated/admin/kb-categories"
			articles := "/escalated/admin/kb-articles"

			wantRedirect(t, pageJSON(h, http.MethodPost, categories, `{"name":"Billing help","slug":"","parent_id":"","position":0,"description":"Invoices"}`), "store category", categories)
			catID := f.id(t, `SELECT id FROM escalated_article_categories WHERE name = ?`, "Billing help")
			wantPageErrors(t, pageJSON(h, http.MethodPost, categories, `{"name":"","parent_id":""}`), "category without a name", "Escalated/Admin/KnowledgeBase/Categories/Index", "name")
			wantRedirect(t, pageJSON(h, http.MethodPut, categories+"/"+itoa(catID), `{"name":"Billing","slug":"billing","parent_id":"","position":1,"description":"Invoices"}`), "update category", categories)

			form := visit(t, h, articles+"/create", uiAgent, "admin")
			if form.Component != "Escalated/Admin/KnowledgeBase/Articles/Form" {
				t.Fatalf("create component %q", form.Component)
			}
			hasKeys(t, "category option", first(t, list(t, form.Props, "categories"), "categories"), "id", "name")

			wantRedirect(t, pageJSON(h, http.MethodPost, articles, `{"title":"Update your card","slug":"","body":"Go to billing.","status":"published","category_id":"`+itoa(catID)+`"}`), "store article", articles)
			wantRedirect(t, pageJSON(h, http.MethodPost, articles, `{"title":"Draft notes","slug":"","body":"wip","status":"draft","category_id":""}`), "store draft", articles)
			wantPageErrors(t, pageJSON(h, http.MethodPost, articles, `{"title":"","status":"draft","category_id":""}`), "article without a title", "Escalated/Admin/KnowledgeBase/Articles/Form", "title")
			wantPageErrors(t, pageJSON(h, http.MethodPost, articles, `{"title":"x","status":"draft","category_id":"9999"}`), "unknown category", "Escalated/Admin/KnowledgeBase/Articles/Form", "category_id")
			articleID := f.id(t, `SELECT id FROM escalated_articles WHERE title = ?`, "Update your card")

			index := visit(t, h, articles+"?status=published", uiAgent, "admin")
			if index.Component != "Escalated/Admin/KnowledgeBase/Articles/Index" {
				t.Fatalf("index component %q", index.Component)
			}
			hasKeys(t, "props", index.Props, "articles", "categories", "filters")
			page := obj(t, index.Props, "articles")
			paginatorKeys(t, "articles", page)
			if page["total"].(float64) != 1 || page["per_page"].(float64) != 20 {
				t.Errorf("articles %v", page)
			}
			row := first(t, list(t, page, "data"), "articles.data")
			hasKeys(t, "article", row, "id", "title", "status", "category", "view_count", "helpful_count", "created_at")
			if obj(t, row, "category")["name"] != "Billing" {
				t.Errorf("row %v", row)
			}
			if obj(t, index.Props, "filters")["status"] != "published" {
				t.Errorf("filters %v", index.Props["filters"])
			}

			edit := visit(t, h, articles+"/"+itoa(articleID)+"/edit", uiAgent, "admin")
			hasKeys(t, "article", obj(t, edit.Props, "article"), "id", "title", "slug", "body", "status", "category_id")
			wantRedirect(t, pageJSON(h, http.MethodPut, articles+"/"+itoa(articleID), `{"title":"Update your card details","slug":"update-your-card","body":"Go to billing.","status":"published","category_id":`+itoa(catID)+`}`), "update article", articles)

			cats := visit(t, h, categories, uiAgent, "admin")
			if cats.Component != "Escalated/Admin/KnowledgeBase/Categories/Index" {
				t.Fatalf("categories component %q", cats.Component)
			}
			cat := first(t, list(t, cats.Props, "categories"), "categories")
			hasKeys(t, "category", cat, "id", "name", "slug", "parent_id", "position", "description", "articles_count")
			if cat["articles_count"].(float64) != 1 {
				t.Errorf("category %v", cat)
			}

			// API clients keep their JSON.
			rec := sendAPI(h, http.MethodGet, "/escalated/admin/kb/articles", uiAgent, "admin", "")
			var out map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out["data"].([]any)) != 2 {
				t.Errorf("API list: %d %s", rec.Code, rec.Body.String())
			}

			draftID := f.id(t, `SELECT id FROM escalated_articles WHERE title = ?`, "Draft notes")
			wantRedirect(t, pageJSON(h, http.MethodDelete, articles+"/"+itoa(draftID), ""), "destroy article", articles)
		})
	}
}

func TestKnowledgeBaseReaderPages(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			ctx := context.Background()
			admin := "/escalated/admin/kb-articles"
			wantRedirect(t, pageJSON(h, http.MethodPost, "/escalated/admin/kb-categories", `{"name":"Billing","description":"Invoices","parent_id":""}`), "category", "/escalated/admin/kb-categories")
			catID := f.id(t, `SELECT id FROM escalated_article_categories WHERE name = ?`, "Billing")
			for _, a := range []struct{ title, status string }{{"Update your card", "published"}, {"Refund policy", "published"}, {"Secret draft", "draft"}} {
				wantRedirect(t, pageJSON(h, http.MethodPost, admin, `{"title":"`+a.title+`","body":"About `+a.title+`","status":"`+a.status+`","category_id":`+itoa(catID)+`}`), a.title, admin)
			}

			page := visit(t, h, "/escalated/kb", customerB, "")
			if page.Component != "Escalated/Customer/KnowledgeBase/Index" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "categories", "articles", "filters", "feedbackEnabled")
			cat := first(t, list(t, page.Props, "categories"), "categories")
			hasKeys(t, "category", cat, "id", "name", "description", "articles_count")
			if cat["articles_count"].(float64) != 2 {
				t.Errorf("category counts drafts: %v", cat)
			}
			articles := obj(t, page.Props, "articles")
			paginatorKeys(t, "articles", articles)
			if articles["total"].(float64) != 2 {
				t.Errorf("readers see %v articles, want the 2 published", articles["total"])
			}
			hasKeys(t, "article", first(t, list(t, articles, "data"), "articles.data"), "id", "title", "slug", "category", "view_count")

			searched := obj(t, visit(t, h, "/escalated/kb?search=Refund", customerB, "").Props, "articles")
			if searched["total"].(float64) != 1 {
				t.Errorf("search found %v", searched["total"])
			}

			show := "/escalated/kb/update-your-card"
			art := visit(t, h, show, customerB, "")
			if art.Component != "Escalated/Customer/KnowledgeBase/Article" {
				t.Fatalf("article component %q", art.Component)
			}
			hasKeys(t, "props", art.Props, "article", "related", "feedbackEnabled")
			article := obj(t, art.Props, "article")
			hasKeys(t, "article", article, "id", "title", "slug", "body", "category", "published_at", "view_count")
			if article["view_count"].(float64) != 1 || obj(t, article, "category")["name"] != "Billing" {
				t.Errorf("article %v", article)
			}
			if rel := list(t, art.Props, "related"); len(rel) != 1 || rel[0].(map[string]any)["slug"] != "refund-policy" {
				t.Errorf("related %v", rel)
			}
			if rec := send(h, http.MethodGet, "/escalated/kb/secret-draft", customerB, "", ""); rec.Code != http.StatusNotFound {
				t.Errorf("a draft is readable: %d", rec.Code)
			}

			wantBack(t, postPage(h, http.MethodPost, show+"/feedback", customerB, "", show, url.Values{"helpful": {"1"}}), "feedback", show)
			if n := f.count(t, "articles", "slug = ? AND helpful_count = 1", "update-your-card"); n != 1 {
				t.Error("the vote was not counted")
			}

			// The settings: feedback off, then private, then off entirely.
			_ = f.esc.Store.SetSetting(ctx, handlers.SettingKnowledgeBaseFeedbackEnabled, "0")
			if rec := postPage(h, http.MethodPost, show+"/feedback", customerB, "", show, url.Values{"helpful": {"1"}}); rec.Code != http.StatusNotFound {
				t.Errorf("feedback while off: %d", rec.Code)
			}
			if visit(t, h, show, customerB, "").Props["feedbackEnabled"] != false {
				t.Error("feedbackEnabled stays true")
			}
			_ = f.esc.Store.SetSetting(ctx, handlers.SettingKnowledgeBasePublic, "0")
			if rec := send(h, http.MethodGet, "/escalated/kb", "", "", ""); rec.Code != http.StatusForbidden {
				t.Errorf("private KB for a visitor: %d", rec.Code)
			}
			_ = f.esc.Store.SetSetting(ctx, handlers.SettingKnowledgeBaseEnabled, "0")
			if rec := send(h, http.MethodGet, "/escalated/kb", customerB, "", ""); rec.Code != http.StatusNotFound {
				t.Errorf("disabled KB: %d", rec.Code)
			}
		})
	}
}
