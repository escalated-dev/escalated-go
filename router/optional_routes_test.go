package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/middleware"
)

// The guest routes: the guest ticket pages, the guest API and the mobile
// guest API, all unauthenticated.
var guestRoutes = [][2]string{
	{http.MethodGet, "/escalated/guest/create"},
	{http.MethodPost, "/escalated/guest"},
	{http.MethodGet, "/escalated/guest/abc"},
	{http.MethodPost, "/escalated/guest/abc/reply"},
	{http.MethodPost, "/escalated/guest/abc/rate"},
	{http.MethodPost, "/escalated/api/guest/verification"},
	{http.MethodPost, "/escalated/api/guest/lookup"},
	{http.MethodPost, "/escalated/api/guest/tickets"},
	{http.MethodGet, "/escalated/api/guest/tickets/abc"},
	{http.MethodPost, "/escalated/api/guest/tickets/abc/replies"},
	{http.MethodPost, "/escalated/api/guest/tickets/abc/rate"},
	{http.MethodPost, "/escalated/api/mobile/guest/verification"},
	{http.MethodPost, "/escalated/api/mobile/guest/lookup"},
	{http.MethodPost, "/escalated/api/mobile/guest/tickets"},
	{http.MethodGet, "/escalated/api/mobile/guest/tickets/abc"},
	{http.MethodPost, "/escalated/api/mobile/guest/tickets/abc/replies"},
}

var widgetRoutes = [][2]string{
	{http.MethodGet, "/escalated/widget/embed.js"},
	{http.MethodGet, "/escalated/widget/config"},
}

// mounted reports, per router, whether method path resolves to a route:
// through chi's tree and the ServeMux's patterns, not by calling a handler.
func mounted(t *testing.T, esc *escalated.Escalated, method, path string) map[string]bool {
	t.Helper()
	r := chi.NewRouter()
	MountChi(r, esc)
	mux := http.NewServeMux()
	MountStdlib(mux, esc)
	inner := http.NewServeMux()
	mountStdlib(inner, esc, middleware.NewGuestRateLimiter(esc.Config.GuestRateLimit))

	_, outer := mux.Handler(httptest.NewRequest(method, path, nil))
	_, pattern := inner.Handler(httptest.NewRequest(method, path, nil))
	stdlib := pattern != ""
	// The widget is mounted on the outer mux, not the inner one.
	if strings.Contains(path, "/widget/") {
		stdlib = strings.Contains(outer, "/widget/")
	}
	return map[string]bool{
		"chi":    r.Match(chi.NewRouteContext(), method, path),
		"stdlib": stdlib,
	}
}

func optionalRoutesInstance(t *testing.T, configure func(*escalated.Config)) *escalated.Escalated {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = true
	if configure != nil {
		configure(&cfg)
	}
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(esc.Close)
	return esc
}

func TestGuestRoutesAreMountedByDefault(t *testing.T) {
	t.Setenv(escalated.EnvGuestRoutes, "")
	esc := optionalRoutesInstance(t, nil)
	for _, route := range guestRoutes {
		for router, ok := range mounted(t, esc, route[0], route[1]) {
			if !ok {
				t.Errorf("%s: %s %s is not mounted by default", router, route[0], route[1])
			}
		}
	}
	if _, ok := esc.RouteList()["escalated.guest.tickets.create"]; !ok {
		t.Error("the guest names left the route list by default")
	}
}

func TestGuestRoutesSwitchedOffAreNotMounted(t *testing.T) {
	off := false
	for name, configure := range map[string]func(*escalated.Config){
		"config": func(c *escalated.Config) { c.GuestRoutes = &off },
		"env":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			if configure == nil {
				t.Setenv(escalated.EnvGuestRoutes, "false")
			} else {
				t.Setenv(escalated.EnvGuestRoutes, "")
			}
			esc := optionalRoutesInstance(t, configure)
			for _, route := range guestRoutes {
				for router, ok := range mounted(t, esc, route[0], route[1]) {
					if ok {
						t.Errorf("%s: %s %s is mounted with guest routes off", router, route[0], route[1])
					}
				}
			}
			for routeName := range esc.RouteList() {
				if strings.HasPrefix(routeName, "escalated.guest.") {
					t.Errorf("%s is in the route list with guest routes off", routeName)
				}
			}
			// The rest of the portal stays.
			for router, ok := range mounted(t, esc, http.MethodGet, "/escalated/tickets") {
				if !ok {
					t.Errorf("%s: the customer routes went with the guest routes", router)
				}
			}
		})
	}
}

func TestGuestRoutesConfigWinsOverEnv(t *testing.T) {
	t.Setenv(escalated.EnvGuestRoutes, "false")
	on := true
	esc := optionalRoutesInstance(t, func(c *escalated.Config) { c.GuestRoutes = &on })
	for router, ok := range mounted(t, esc, http.MethodGet, "/escalated/guest/create") {
		if !ok {
			t.Errorf("%s: GuestRoutes true did not mount the guest pages", router)
		}
	}
}

func TestWidgetRoutesOnlyWithEnableWidget(t *testing.T) {
	off := optionalRoutesInstance(t, nil)
	on := optionalRoutesInstance(t, func(c *escalated.Config) { c.EnableWidget = true })
	for _, route := range widgetRoutes {
		for router, ok := range mounted(t, off, route[0], route[1]) {
			if ok {
				t.Errorf("%s: %s %s is mounted without EnableWidget", router, route[0], route[1])
			}
		}
		for router, ok := range mounted(t, on, route[0], route[1]) {
			if !ok {
				t.Errorf("%s: %s %s is not mounted with EnableWidget", router, route[0], route[1])
			}
		}
	}
}
