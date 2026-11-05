package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/middleware"
)

// defaultRoutesFixture is a snapshot of every route the package mounts with
// no embed configuration, taken from main before the embeddable admin
// existed (ADR 2026-11-05-embeddable-admin). Regenerate it only for a
// deliberate route change: ESCALATED_UPDATE_ROUTE_SNAPSHOT=1 go test ./router.
const defaultRoutesFixture = "../testdata/embed/default-routes.json"

type routeSnapshot struct {
	// Chi is every route MountChi registers: method, pattern, the handler and
	// the middleware it runs (outermost first).
	Chi []chiRoute `json:"chi"`
	// Stdlib is the ServeMux pattern MountStdlib's mux picks for each of
	// those routes (the ServeMux cannot be listed).
	Stdlib []stdlibRoute `json:"stdlib"`
	// Names is the route list the pages' route() reads (the ziggy prop).
	Names map[string]escalated.NamedRoute `json:"names"`
}

type chiRoute struct {
	Method     string   `json:"method"`
	Pattern    string   `json:"pattern"`
	Handler    string   `json:"handler"`
	Middleware []string `json:"middleware"`
}

type stdlibRoute struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Pattern string `json:"pattern"`
}

var inlineSegment = regexp.MustCompile(`^(\d+|func\d+)$`)

// funcName names a middleware or handler function stably: the package path
// and the compiler's closure and inlining numbers are dropped, and a
// function inlined into the router's mount code is named by itself alone
// ("~RequireAdmin"), so moving code within MountChi does not change it.
func funcName(v any) string {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Func {
		return reflect.TypeOf(v).String()
	}
	full := runtime.FuncForPC(rv.Pointer()).Name()
	full = full[strings.LastIndex(full, "/")+1:]
	var kept []string
	for _, seg := range strings.Split(full, ".") {
		if !inlineSegment.MatchString(seg) {
			kept = append(kept, seg)
		}
	}
	if len(kept) > 2 && kept[0] == "router" {
		return "~" + kept[len(kept)-1]
	}
	return strings.Join(kept, ".")
}

func handlerName(h http.Handler) string {
	if fn, ok := h.(http.HandlerFunc); ok {
		return funcName(fn)
	}
	return reflect.TypeOf(h).String()
}

// snapshotConfigs are the configurations the snapshot covers: the defaults,
// and every optional route group switched on.
func snapshotConfigs(t *testing.T) map[string]escalated.Config {
	base := escalated.DefaultConfig()
	base.RoutePrefix = "/support"
	all := base
	all.EnableWidget = true
	all.APITokensEnabled = true
	// MountStdlib panics with newsletters on (two of their ServeMux patterns
	// conflict), so this one is snapshotted through chi only.
	news := base
	news.EnableNewsletters = true
	return map[string]escalated.Config{"default": base, "optional_groups_on": all, "newsletters_on": news}
}

func takeRouteSnapshot(t *testing.T, cfg escalated.Config) routeSnapshot {
	t.Helper()
	cfg.DB = testdb.Open(t)
	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(esc.Close)

	r := chi.NewRouter()
	MountChi(r, esc)
	var inner *http.ServeMux
	if !cfg.EnableNewsletters {
		inner = http.NewServeMux()
		mountStdlib(inner, esc, middleware.NewGuestRateLimiter(cfg.GuestRateLimit))
	}

	snap := routeSnapshot{Names: esc.RouteList()}
	err = chi.Walk(r, func(method, route string, h http.Handler, mws ...func(http.Handler) http.Handler) error {
		names := make([]string, 0, len(mws))
		for _, mw := range mws {
			names = append(names, funcName(mw))
		}
		snap.Chi = append(snap.Chi, chiRoute{Method: method, Pattern: route, Handler: handlerName(h), Middleware: names})
		if inner == nil {
			return nil
		}
		path := routeParam.ReplaceAllString(route, "1")
		path = strings.ReplaceAll(path, "/*", "/x")
		if strings.HasSuffix(path, "/agent/") || strings.HasSuffix(path, "/agent") {
			path = strings.TrimRight(path, "/") + "/"
		}
		_, pattern := inner.Handler(httptest.NewRequest(method, path, nil))
		snap.Stdlib = append(snap.Stdlib, stdlibRoute{Method: method, Path: path, Pattern: pattern})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Slice(snap.Chi, func(i, j int) bool {
		if snap.Chi[i].Pattern != snap.Chi[j].Pattern {
			return snap.Chi[i].Pattern < snap.Chi[j].Pattern
		}
		return snap.Chi[i].Method < snap.Chi[j].Method
	})
	sort.Slice(snap.Stdlib, func(i, j int) bool {
		if snap.Stdlib[i].Path != snap.Stdlib[j].Path {
			return snap.Stdlib[i].Path < snap.Stdlib[j].Path
		}
		return snap.Stdlib[i].Method < snap.Stdlib[j].Method
	})
	return snap
}

// With no embed configuration the route table, the middleware on every
// route and the route names are exactly what they were before the
// embeddable admin existed.
func TestDefaultRouteTableIsUnchangedWithoutEmbedConfig(t *testing.T) {
	live := map[string]routeSnapshot{}
	for name, cfg := range snapshotConfigs(t) {
		live[name] = takeRouteSnapshot(t, cfg)
	}
	got, err := json.MarshalIndent(live, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("ESCALATED_UPDATE_ROUTE_SNAPSHOT") == "1" {
		if err := os.MkdirAll("../testdata/embed", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(defaultRoutesFixture, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("snapshot written")
	}
	want, err := os.ReadFile(defaultRoutesFixture)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var wantSnap map[string]routeSnapshot
	if err := json.Unmarshal(want, &wantSnap); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	for name, snap := range live {
		if !reflect.DeepEqual(normalizeSnapshot(snap), normalizeSnapshot(wantSnap[name])) {
			diffSnapshots(t, name, wantSnap[name], snap)
		}
	}
}

func normalizeSnapshot(s routeSnapshot) routeSnapshot {
	for i := range s.Chi {
		if s.Chi[i].Middleware == nil {
			s.Chi[i].Middleware = []string{}
		}
	}
	return s
}

func diffSnapshots(t *testing.T, name string, want, got routeSnapshot) {
	t.Helper()
	key := func(r chiRoute) string { return r.Method + " " + r.Pattern }
	wantChi := map[string]chiRoute{}
	for _, r := range want.Chi {
		wantChi[key(r)] = r
	}
	gotChi := map[string]chiRoute{}
	for _, r := range got.Chi {
		gotChi[key(r)] = r
	}
	for k, w := range wantChi {
		g, ok := gotChi[k]
		switch {
		case !ok:
			t.Errorf("%s: chi route %s is gone", name, k)
		case !reflect.DeepEqual(normalizeSnapshot(routeSnapshot{Chi: []chiRoute{w}}), normalizeSnapshot(routeSnapshot{Chi: []chiRoute{g}})):
			t.Errorf("%s: chi route %s changed:\nwant %+v\ngot  %+v", name, k, w, g)
		}
	}
	for k := range gotChi {
		if _, ok := wantChi[k]; !ok {
			t.Errorf("%s: chi route %s is new", name, k)
		}
	}
	if !reflect.DeepEqual(want.Stdlib, got.Stdlib) {
		t.Errorf("%s: stdlib patterns changed", name)
	}
	if !reflect.DeepEqual(want.Names, got.Names) {
		for n, w := range want.Names {
			if !reflect.DeepEqual(w, got.Names[n]) {
				t.Errorf("%s: route name %s: want %+v, got %+v", name, n, w, got.Names[n])
			}
		}
		for n := range got.Names {
			if _, ok := want.Names[n]; !ok {
				t.Errorf("%s: route name %s is new", name, n)
			}
		}
	}
}
