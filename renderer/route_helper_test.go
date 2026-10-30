package renderer

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The host's root template installs route() with {{ .escalatedRouteHelper }}.
func TestRootTemplateCanInstallTheRouteHelper(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "app.html")
	tpl := `<html><body><script data-page="app" type="application/json">{{ marshal .page }}</script><script>{{ .escalatedRouteHelper }}</script></body></html>`
	if err := os.WriteFile(root, []byte(tpl), 0o600); err != nil {
		t.Fatal(err)
	}
	rend := NewInertiaRenderer(root)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/escalated/agent", nil)
	if err := rend.Render(rec, req, "Agent/Dashboard", map[string]any{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<script>"+RouteHelperScript+"</script>") {
		t.Fatalf("route helper not installed verbatim:\n%s", body)
	}
}

func TestRouteHelperKeepsTheHostsRouteAndZiggy(t *testing.T) {
	for _, want := range []string{
		"if (typeof window.route === 'function') return;",
		"window.Ziggy && window.Ziggy.routes",
		"console.warn('[escalated] no route named ' + name)",
		"return '#';",
		"script[data-page]",
	} {
		if !strings.Contains(RouteHelperScript, want) {
			t.Errorf("helper lacks %q", want)
		}
	}
}

func TestSharedObjectsMergeNestedKeysOwnFirst(t *testing.T) {
	ctx := WithShared(httptest.NewRequest("GET", "/", nil).Context(), "ziggy", func() any {
		return map[string]any{"url": "host", "routes": map[string]any{"a": "host-a"}}
	})
	ctx = WithShared(ctx, "ziggy", func() any {
		return map[string]any{"url": "built-in", "port": nil, "routes": map[string]any{"a": "built-in-a", "b": "built-in-b"}}
	})
	out := withSharedProps(ctx, map[string]any{})
	z := out["ziggy"].(map[string]any)
	if z["url"] != "host" {
		t.Errorf("url %v, want the first shared value", z["url"])
	}
	if _, ok := z["port"]; !ok {
		t.Errorf("port dropped")
	}
	routes := z["routes"].(map[string]any)
	if routes["a"] != "host-a" || routes["b"] != "built-in-b" {
		t.Errorf("routes %v", routes)
	}
}
