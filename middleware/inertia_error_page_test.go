package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/renderer"
)

// The failures the router tests cannot provoke on a real route (419, 429,
// 503, a panic), against the middleware with the real Inertia renderer.

func errorPageHandler(next http.Handler) http.Handler {
	return middleware.InertiaErrorPage(renderer.NewInertiaRenderer(""), "/escalated")(next)
}

func inertiaGet(h http.Handler, path string, inertia bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if inertia {
		r.Header.Set("X-Inertia", "true")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func errorPage(t *testing.T, rec *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	if rec.Header().Get("X-Inertia") != "true" {
		t.Fatalf("not an Inertia page: status %d, body %q", rec.Code, rec.Body.String())
	}
	var page struct {
		Component string         `json:"component"`
		Props     map[string]any `json:"props"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v (%q)", err, rec.Body.String())
	}
	return page.Component, page.Props
}

func TestInertiaErrorPageRendersEachStatus(t *testing.T) {
	cases := map[int]string{
		403: "You are not authorized to perform this action.",
		404: "The requested resource was not found.",
		419: "Your session has expired. Refresh the page and try again.",
		429: "Too many requests. Wait a moment and try again.",
		500: "Something went wrong on our end.",
		503: "The service is temporarily unavailable. Try again shortly.",
	}
	for status, message := range cases {
		h := errorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream detail: relation escalated_secret does not exist", status)
		}))

		rec := inertiaGet(h, "/escalated/agent", true)

		if rec.Code != status {
			t.Errorf("%d: status %d", status, rec.Code)
		}
		component, props := errorPage(t, rec)
		if component != "Escalated/Error" {
			t.Errorf("%d: component %q", status, component)
		}
		if props["status"] != float64(status) || props["message"] != message {
			t.Errorf("%d: props %v", status, props)
		}
		if strings.Contains(rec.Body.String(), "upstream detail") || strings.Contains(rec.Body.String(), "escalated_secret") {
			t.Errorf("%d: handler text leaked: %s", status, rec.Body.String())
		}
	}
}

func TestInertiaErrorPageSendsOnlyStatusAndMessage(t *testing.T) {
	h := errorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))

	_, props := errorPage(t, inertiaGet(h, "/escalated/admin", true))

	for _, key := range []string{"home", "exception", "error"} {
		if _, ok := props[key]; ok {
			t.Errorf("unexpected prop %q in %v", key, props)
		}
	}
}

func TestInertiaErrorPageRendersAPanicAsA500(t *testing.T) {
	h := errorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("sql: relation escalated_secret does not exist")
	}))

	rec := inertiaGet(h, "/escalated/agent", true)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	component, props := errorPage(t, rec)
	if component != "Escalated/Error" || props["status"] != float64(500) {
		t.Errorf("page %q %v", component, props)
	}
	if strings.Contains(rec.Body.String(), "escalated_secret") {
		t.Errorf("panic value leaked: %s", rec.Body.String())
	}
}

func TestInertiaErrorPageLeavesAPanicOfAFullPageLoadToTheHost(t *testing.T) {
	h := errorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	defer func() {
		if recover() == nil {
			t.Error("the panic was swallowed")
		}
	}()
	inertiaGet(h, "/escalated/agent", false)
}

func TestInertiaErrorPageLeavesOtherResponsesAlone(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		inertia bool
		handler http.HandlerFunc
		status  int
		body    string
	}{
		{"full page load", "/escalated/agent", false, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Forbidden", http.StatusForbidden)
		}, 403, "Forbidden\n"},
		{"json api", "/escalated/api/tickets", true, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
		}, 403, "{\"message\":\"Forbidden\"}\n"},
		{"webhooks", "/escalated/webhooks/plugins/x", true, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}, 404, "nope\n"},
		{"widget", "/escalated/widget/config", true, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}, 404, "nope\n"},
		{"sso", "/escalated/sso/login", true, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}, 404, "nope\n"},
		{"outside the prefix", "/host/page", true, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "host page", http.StatusNotFound)
		}, 404, "host page\n"},
		{"a status the page has no wording for", "/escalated/agent", true, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "conflict", http.StatusConflict)
		}, 409, "conflict\n"},
		{"a redirect", "/escalated/agent", true, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/escalated/agent/tickets", http.StatusSeeOther)
		}, 303, ""},
		{"an Inertia page already", "/escalated/agent", true, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Inertia", "true")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"component":"Escalated/Agent/Dashboard"}`))
		}, 404, `{"component":"Escalated/Agent/Dashboard"}`},
	}
	for _, c := range cases {
		rec := inertiaGet(errorPageHandler(c.handler), c.path, c.inertia)
		if rec.Code != c.status {
			t.Errorf("%s: status %d", c.name, rec.Code)
		}
		if c.body != "" && rec.Body.String() != c.body {
			t.Errorf("%s: body %q", c.name, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Escalated/Error") {
			t.Errorf("%s: rendered the error page", c.name)
		}
	}
}

// With debug on (Config.Debug), a 500 is the developer's to read: the
// handler's own response and a panic reach the host as they would without
// Escalated, the way escalated-laravel leaves a 500 to the framework's debug
// page under APP_DEBUG. Every other status still gets the page.

func debugErrorPageHandler(next http.Handler) http.Handler {
	return middleware.InertiaErrorPage(renderer.NewInertiaRenderer(""), "/escalated", middleware.ErrorPageDebug(true))(next)
}

func TestInertiaErrorPageInDebugModeLeavesA500ToTheHandler(t *testing.T) {
	h := debugErrorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sql: relation escalated_secret does not exist", http.StatusInternalServerError)
	}))

	rec := inertiaGet(h, "/escalated/agent", true)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("X-Inertia") != "" || strings.Contains(rec.Body.String(), "Escalated/Error") {
		t.Fatalf("rendered the error page in debug mode: %q", rec.Body.String())
	}
	if rec.Body.String() != "sql: relation escalated_secret does not exist\n" {
		t.Errorf("body %q", rec.Body.String())
	}
}

func TestInertiaErrorPageInDebugModeLeavesAPanicToTheHost(t *testing.T) {
	h := debugErrorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	defer func() {
		if recover() != "boom" {
			t.Error("the panic did not reach the host")
		}
	}()
	inertiaGet(h, "/escalated/agent", true)
}

func TestInertiaErrorPageInDebugModeStillRendersOtherStatuses(t *testing.T) {
	h := debugErrorPageHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))

	rec := inertiaGet(h, "/escalated/agent", true)

	component, props := errorPage(t, rec)
	if rec.Code != http.StatusForbidden || component != "Escalated/Error" || props["status"] != float64(403) {
		t.Errorf("status %d, page %q %v", rec.Code, component, props)
	}
}

func TestInertiaErrorPageWithDebugOffRendersA500(t *testing.T) {
	h := middleware.InertiaErrorPage(renderer.NewInertiaRenderer(""), "/escalated", middleware.ErrorPageDebug(false))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "sql: relation escalated_secret does not exist", http.StatusInternalServerError)
		}))

	rec := inertiaGet(h, "/escalated/agent", true)

	component, _ := errorPage(t, rec)
	if rec.Code != http.StatusInternalServerError || component != "Escalated/Error" {
		t.Errorf("status %d, page %q", rec.Code, component)
	}
	if strings.Contains(rec.Body.String(), "escalated_secret") {
		t.Errorf("handler text leaked: %s", rec.Body.String())
	}
}
