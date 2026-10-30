package router

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/middleware"
)

var routeParam = regexp.MustCompile(`\{\w+\??\}`)

// Every name in the built-in route list must point at a route the router
// really mounts, with every method it lists: resolved through chi's tree and
// the ServeMux's patterns, not by calling the handlers (which may answer 404
// for a missing record or a switched-off feature).
func TestEveryBuiltInRouteResolvesOnTheRealRouters(t *testing.T) {
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.RoutePrefix = "/support"
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	chiRouter := chi.NewRouter()
	MountChi(chiRouter, esc)
	inner := http.NewServeMux()
	mountStdlib(inner, esc, middleware.NewGuestRateLimiter(cfg.GuestRateLimit))

	routes := escalated.BuiltInRoutes(cfg.RoutePrefix)
	if len(routes) < 150 {
		t.Fatalf("%d built-in routes, want the whole list", len(routes))
	}
	for name, route := range routes {
		path := "/" + routeParam.ReplaceAllString(route.URI, "1")
		for _, method := range route.Methods {
			if method == http.MethodHead {
				continue
			}
			if !chiRouter.Match(chi.NewRouteContext(), method, path) {
				t.Errorf("chi: %s %s (%s) is not mounted", method, path, name)
			}
			matchPath := path
			if strings.HasSuffix(route.URI, "/agent") {
				// The ServeMux dashboard pattern is the subtree "/agent/".
				matchPath += "/"
			}
			req := httptest.NewRequest(method, matchPath, nil)
			if _, pattern := inner.Handler(req); pattern == "" {
				t.Errorf("stdlib: %s %s (%s) is not mounted", method, matchPath, name)
			}
		}
	}
}
