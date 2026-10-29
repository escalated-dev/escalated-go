package plugins

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var slugUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Slug is a plugin's slug: its name without a leading "@", each "/" as "--",
// and anything else outside [A-Za-z0-9_-] as "-". So
// "@escalated-dev/plugin-slack" is "escalated-dev--plugin-slack".
func Slug(name string) string {
	s := strings.TrimPrefix(name, "@")
	s = strings.ReplaceAll(s, "/", "--")
	return slugUnsafe.ReplaceAllString(s, "-")
}

var routeMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "OPTIONS": true, "HEAD": true,
}

var paramSegment = regexp.MustCompile(`^:([A-Za-z_][A-Za-z0-9_]*)$`)

// route is one parsed endpoint or webhook. path is the declared path, which is
// what a plugin is dispatched on.
type route struct {
	method     string
	path       string
	capability string
	handler    RouteFunc // nil for SDK plugins: the runtime answers
	segments   []string
	params     []int // indexes of :param segments
}

// parseRoute validates a route as escalated-laravel's RouteRegistrar does,
// refusing unknown methods and paths that are not absolute or hold "..",
// control characters, "?", "#" or a backslash.
func parseRoute(plugin, method, path, capability string, handler RouteFunc) (route, error) {
	m := strings.ToUpper(strings.TrimSpace(method))
	if !routeMethods[m] || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\\ ") {
		return route{}, fmt.Errorf("plugin %s: route %q is not METHOD /path", plugin, method+" "+path)
	}
	for _, c := range path {
		if c < 0x20 || c == 0x7f {
			return route{}, fmt.Errorf("plugin %s: route %q is not METHOD /path", plugin, method+" "+path)
		}
	}
	segments := strings.Split(path, "/")
	r := route{method: m, path: path, capability: capability, handler: handler, segments: segments}
	for i, seg := range segments {
		if seg == ".." {
			return route{}, fmt.Errorf("plugin %s: route %q is not METHOD /path", plugin, method+" "+path)
		}
		if paramSegment.MatchString(seg) {
			r.params = append(r.params, i)
		}
	}
	return r, nil
}

// match reports whether the request path matches the route, with its params.
func (r route) match(method, path string) (map[string]string, bool) {
	if r.method != method {
		return nil, false
	}
	parts := strings.Split(path, "/")
	if len(parts) != len(r.segments) {
		return nil, false
	}
	params := map[string]string{}
	for i, seg := range r.segments {
		if paramSegment.MatchString(seg) {
			if parts[i] == "" {
				return nil, false
			}
			v, err := url.PathUnescape(parts[i])
			if err != nil {
				return nil, false
			}
			params[seg[1:]] = v
			continue
		}
		if parts[i] != seg {
			return nil, false
		}
	}
	return params, true
}

func matchRoute(routes []route, method, path string) (route, map[string]string, bool) {
	for _, r := range routes {
		if params, ok := r.match(method, path); ok {
			return r, params, true
		}
	}
	return route{}, nil, false
}

// pageRoute is a page's route with leading slashes removed; empty means none.
func pageRoute(p Page) string {
	return strings.TrimLeft(p.Route, "/")
}
