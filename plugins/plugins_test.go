package plugins

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	for name, want := range map[string]string{
		"@escalated-dev/plugin-slack": "escalated-dev--plugin-slack",
		"@acme/escalated-plugin-crm":  "acme--escalated-plugin-crm",
		"billing":                     "billing",
		"acme.billing v2":             "acme-billing-v2",
	} {
		if got := Slug(name); got != want {
			t.Errorf("Slug(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseRouteRefusesMalformedPaths(t *testing.T) {
	for _, bad := range []struct{ method, path string }{
		{"FETCH", "/x"}, {"GET", "x"}, {"GET", "/a/../b"}, {"GET", "/a?b"}, {"GET", "/a#b"}, {"GET", `/a\b`}, {"GET", "/a b"}, {"GET", "/a\x01"},
	} {
		if _, err := parseRoute("p", bad.method, bad.path, "", nil); err == nil {
			t.Errorf("%s %q was accepted", bad.method, bad.path)
		}
	}
	r, err := parseRoute("p", "get", "/items/:id/notes/:note", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	params, ok := r.match("GET", "/items/4%2F2/notes/n1")
	if !ok || params["id"] != "4/2" || params["note"] != "n1" {
		t.Errorf("match %v %v", params, ok)
	}
	if _, ok := r.match("GET", "/items//notes/n1"); ok {
		t.Error("an empty param matched")
	}
	if _, ok := r.match("POST", "/items/1/notes/2"); ok {
		t.Error("another method matched")
	}
}

func TestMakeReplyValidatesEnvelopes(t *testing.T) {
	ok, err := makeReply(map[string]any{"status": float64(401)})
	if err != nil || ok.status != 200 {
		t.Errorf("an ordinary object with status is JSON 200: %+v %v", ok, err)
	}
	for name, env := range map[string]map[string]any{
		"version":       {"__escalated_http": float64(2), "status": float64(200), "format": "json", "headers": map[string]any{}, "body": nil},
		"status":        {"__escalated_http": float64(1), "status": float64(99), "format": "json", "headers": map[string]any{}, "body": nil},
		"format":        {"__escalated_http": float64(1), "status": float64(200), "format": "xml", "headers": map[string]any{}, "body": nil},
		"no body":       {"__escalated_http": float64(1), "status": float64(200), "format": "json", "headers": map[string]any{}},
		"text body":     {"__escalated_http": float64(1), "status": float64(200), "format": "text", "headers": map[string]any{}, "body": float64(1)},
		"set-cookie":    {"__escalated_http": float64(1), "status": float64(200), "format": "json", "headers": map[string]any{"Set-Cookie": "a=b"}, "body": nil},
		"header value":  {"__escalated_http": float64(1), "status": float64(200), "format": "json", "headers": map[string]any{"x-a": "a\r\nb"}, "body": nil},
		"duplicate hdr": {"__escalated_http": float64(1), "status": float64(200), "format": "json", "headers": map[string]any{"X-A": "1", "x-a": "2"}, "body": nil},
	} {
		if _, err := makeReply(env); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	rep, err := makeReply(map[string]any{"__escalated_http": float64(1), "status": float64(202), "format": "text", "headers": map[string]any{"X-Id": "7"}, "body": "queued"})
	if err != nil || rep.status != 202 || string(rep.body) != "queued" || rep.headers["x-id"] != "7" || !strings.HasPrefix(rep.headers["content-type"], "text/plain") {
		t.Errorf("text envelope %+v %v", rep, err)
	}
}
