package router_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/escalated-dev/escalated-go/router"
)

// A refusal or a failure inside the panel stays inside the panel: an Inertia
// visit to an Escalated route that ends in 403, 404, 419, 429, 500 or 503 gets
// the shared frontend's Escalated/Error page, with the props Rails and Laravel
// send (status and a generic message), through both routers. Full page loads,
// the JSON API and the host's own routes are left alone.

func decodeErrorPage(t *testing.T, mount string, body []byte) (string, map[string]any) {
	t.Helper()
	var page struct {
		Component string         `json:"component"`
		Props     map[string]any `json:"props"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("%s: decode page: %v (%q)", mount, err, body)
	}
	return page.Component, page.Props
}

func TestErrorPageForARefusedInertiaVisit(t *testing.T) {
	f := newGuardFixture(t)
	for name, h := range f.mounts() {
		rec := send(h, "GET", "/escalated/admin/settings", "900", "agent", "")

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		if rec.Header().Get("X-Inertia") != "true" {
			t.Fatalf("%s: not an Inertia page: %q", name, rec.Body.String())
		}
		component, props := decodeErrorPage(t, name, rec.Body.Bytes())
		if component != "Escalated/Error" {
			t.Errorf("%s: component %q", name, component)
		}
		if props["status"] != float64(403) || props["message"] != "You are not authorized to perform this action." {
			t.Errorf("%s: props %v", name, props)
		}
		if _, ok := props["home"]; ok {
			t.Errorf("%s: sent a home prop", name)
		}
	}
}

func TestErrorPageForAnInertiaVisitToAMissingTicket(t *testing.T) {
	f := newGuardFixture(t)
	for name, h := range f.mounts() {
		rec := send(h, "GET", "/escalated/agent/tickets/999999", "900", "agent", "")

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d (%q)", name, rec.Code, rec.Body.String())
		}
		component, props := decodeErrorPage(t, name, rec.Body.Bytes())
		if component != "Escalated/Error" || props["status"] != float64(404) ||
			props["message"] != "The requested resource was not found." {
			t.Errorf("%s: page %q %v", name, component, props)
		}
		if strings.Contains(rec.Body.String(), "not found\n") {
			t.Errorf("%s: handler text leaked", name)
		}
	}
}

func TestErrorPageLeavesAFullPageLoadAlone(t *testing.T) {
	f := newGuardFixture(t)
	for name, h := range f.mounts() {
		rec := sendAPI(h, "GET", "/escalated/admin/settings", "900", "agent", "")

		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		if rec.Header().Get("X-Inertia") != "" || strings.Contains(rec.Body.String(), "Escalated/Error") {
			t.Errorf("%s: rendered the error page for a full page load", name)
		}
	}
}

func TestErrorPageLeavesTheJSONAPIAlone(t *testing.T) {
	f := newGuardFixture(t)
	for name, h := range f.mounts() {
		rec := send(h, "GET", "/escalated/api/tickets", customerA, "", "")

		if !denied(rec.Code) {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "Escalated/Error") {
			t.Errorf("%s: rendered the error page on the API", name)
		}
	}
}

func TestErrorPageLeavesHostRoutesAlone(t *testing.T) {
	f := newGuardFixture(t)

	r := chi.NewRouter()
	router.MountChi(r, f.esc)
	r.Get("/host/missing", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "host page", http.StatusNotFound) })
	mux := http.NewServeMux()
	router.MountStdlib(mux, f.esc)
	mux.HandleFunc("GET /host/missing", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "host page", http.StatusNotFound) })

	for name, h := range map[string]http.Handler{"chi": r, "stdlib": mux} {
		rec := send(h, "GET", "/host/missing", "", "", "")
		if rec.Code != http.StatusNotFound || rec.Body.String() != "host page\n" {
			t.Errorf("%s: %d %q", name, rec.Code, rec.Body.String())
		}
	}
}

func TestErrorPageIsOffWhileTheUIIsDisabled(t *testing.T) {
	f := newGuardFixture(t)
	f.esc.Config.UIEnabled = false

	for name, h := range f.mounts() {
		rec := send(h, "GET", "/escalated/agent/tickets/999999", "900", "agent", "")
		if strings.Contains(rec.Body.String(), "Escalated/Error") {
			t.Errorf("%s: rendered the error page with the UI off", name)
		}
	}
}
