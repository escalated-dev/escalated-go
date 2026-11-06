package router_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/router"
	"github.com/escalated-dev/escalated-go/services/newsletter"
)

// MountStdlib mounts with newsletters on (it used to panic: the campaign
// edit pattern conflicted with the list and template show patterns), and
// every newsletter route answers on the ServeMux as it does on chi.
func TestStdlibMountsNewsletterRoutesLikeChi(t *testing.T) {
	f := newUIFixture(t, func(cfg *escalated.Config) { cfg.EnableNewsletters = true })

	ctx := context.Background()
	cfg := f.esc.Config
	store := newsletter.NewSQLStore(cfg.DB, cfg.TablePrefix, cfg.DatabaseDialect)
	list := &models.NewsletterList{Name: "Customers", Kind: models.NewsletterListStatic}
	if err := store.CreateList(ctx, list); err != nil {
		t.Fatalf("list: %v", err)
	}
	tpl := &models.NewsletterTemplate{Name: "Plain", Theme: "default", BodyMarkdown: "Hi"}
	if err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("template: %v", err)
	}
	body := "Hello"
	campaign := &models.Newsletter{Subject: "November", FromEmail: "news@example.com", TargetListID: list.ID, BodyMarkdown: &body, Status: models.NewsletterDraft}
	if err := store.CreateNewsletter(ctx, campaign); err != nil {
		t.Fatalf("newsletter: %v", err)
	}

	chiRouter := chi.NewRouter()
	router.MountChi(chiRouter, f.esc)
	mux := http.NewServeMux()
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("MountStdlib with newsletters on panicked: %v", p)
			}
		}()
		router.MountStdlib(mux, f.esc)
	}()

	const p = "/escalated/admin/newsletters"
	n := fmt.Sprint(campaign.ID)
	l := fmt.Sprint(list.ID)
	tp := fmt.Sprint(tpl.ID)
	// Writes go to records that do not exist, so chi's request does not
	// change what the ServeMux's request finds.
	requests := []struct{ method, path string }{
		{http.MethodGet, p},
		{http.MethodGet, p + "/new"},
		{http.MethodPost, p},
		{http.MethodPost, p + "/preview"},
		{http.MethodPost, p + "/test"},
		{http.MethodGet, p + "/lists"},
		{http.MethodGet, p + "/lists/new"},
		{http.MethodPost, p + "/lists"},
		{http.MethodGet, p + "/lists/" + l},
		{http.MethodPut, p + "/lists/999"},
		{http.MethodDelete, p + "/lists/999"},
		{http.MethodPost, p + "/lists/999/members"},
		{http.MethodDelete, p + "/lists/999/members/2"},
		{http.MethodPost, p + "/lists/999/import"},
		{http.MethodGet, p + "/templates"},
		{http.MethodGet, p + "/templates/new"},
		{http.MethodPost, p + "/templates"},
		{http.MethodGet, p + "/templates/" + tp},
		{http.MethodPut, p + "/templates/999"},
		{http.MethodDelete, p + "/templates/999"},
		{http.MethodGet, p + "/settings"},
		{http.MethodPut, p + "/settings"},
		{http.MethodGet, p + "/" + n},
		{http.MethodGet, p + "/" + n + "/edit"},
		{http.MethodGet, p + "/999/edit"},
		{http.MethodPut, p + "/999"},
		{http.MethodDelete, p + "/999"},
		// The paths both conflicting patterns matched: chi takes the list and
		// template show routes.
		{http.MethodGet, p + "/lists/edit"},
		{http.MethodGet, p + "/templates/edit"},
		// Not a route.
		{http.MethodGet, p + "/" + n + "/other"},
		// Public tracking and webhooks.
		{http.MethodGet, "/escalated/n/o/tok"},
		{http.MethodGet, "/escalated/n/c/tok"},
		{http.MethodGet, "/escalated/n/u/tok"},
		{http.MethodPost, "/escalated/n/u/tok"},
		{http.MethodGet, "/escalated/n/v/tok"},
		{http.MethodPost, "/escalated/webhooks/newsletter/postmark"},
		{http.MethodPost, "/escalated/webhooks/newsletter/mailgun"},
		{http.MethodPost, "/escalated/webhooks/newsletter/ses"},
		{http.MethodPost, "/escalated/webhooks/newsletter/sendgrid"},
	}
	for _, role := range []string{"admin", "agent", ""} {
		for _, req := range requests {
			name := fmt.Sprintf("%s %s as %q", req.method, req.path, role)
			want := send(chiRouter, req.method, req.path, uiAgent, role, "")
			got := send(mux, req.method, req.path, uiAgent, role, "")
			if got.Code != want.Code {
				t.Errorf("%s: stdlib %d, chi %d (%s)", name, got.Code, want.Code, got.Body.String())
			}
		}
	}

	// The campaign edit screen itself, and the list and template screens on
	// the formerly ambiguous shape.
	for _, path := range []string{p + "/" + n + "/edit", p + "/lists/" + l, p + "/templates/" + tp} {
		want := pageComponent(t, chiRouter, path)
		if got := pageComponent(t, mux, path); got != want || got == "" {
			t.Errorf("GET %s: stdlib renders %q, chi %q", path, got, want)
		}
	}
}

func pageComponent(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := send(h, http.MethodGet, path, uiAgent, "admin", "")
	if rec.Code != http.StatusOK {
		t.Errorf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		return ""
	}
	var page inertiaPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Errorf("GET %s: not an Inertia page: %v", path, err)
		return ""
	}
	return page.Component
}
