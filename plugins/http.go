package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/renderer"
)

// Page names this package renders.
const (
	IndexPage  = "Escalated/Admin/Plugins/Index"
	PluginPage = "Escalated/Plugin/Page"
)

const flashCookie = "escalated_plugins_flash"

// maxRequestBody caps an endpoint or webhook body, so that the bytes, their
// base64 and the parsed body fit one 10 MB runtime message.
const maxRequestBody = 3 << 20

// Handler serves the plugin routes. The router mounts Admin behind the admin
// guard at {RoutePrefix}/admin/plugins, Endpoint behind the admin guard at
// {RoutePrefix}/api/plugins, and Webhook, unguarded, at
// {RoutePrefix}/webhooks/plugins.
type Handler struct {
	Host     *Host
	Renderer renderer.Renderer
}

func (h *Handler) prefix() string {
	if h.Host.routePrefix == "" {
		return "/escalated"
	}
	return h.Host.routePrefix
}

// rest is the request path below base, still percent-encoded.
func rest(r *http.Request, base string) (string, bool) {
	path := r.URL.EscapedPath()
	if path == base {
		return "", true
	}
	if !strings.HasPrefix(path, base+"/") {
		return "", false
	}
	return path[len(base):], true
}

// splitSlug splits "/{slug}/more" into the slug and "/more" (still encoded).
func splitSlug(path string) (string, string, bool) {
	path = strings.TrimPrefix(path, "/")
	slug, below, _ := strings.Cut(path, "/")
	decoded, err := url.PathUnescape(slug)
	if err != nil || decoded == "" {
		return "", "", false
	}
	return decoded, "/" + below, true
}

// Admin serves Escalated/Admin/Plugins/Index, the lifecycle routes and the
// plugins' own pages.
func (h *Handler) Admin(w http.ResponseWriter, r *http.Request) {
	if h.Host == nil {
		http.NotFound(w, r)
		return
	}
	path, ok := rest(r, h.prefix()+"/admin/plugins")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if path == "" || path == "/" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		h.index(w, r)
		return
	}
	slug, below, ok := splitSlug(path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case below == "/" && r.Method == http.MethodDelete:
		h.lifecycle(w, r, slug, "uninstall")
	case below == "/activate" && r.Method == http.MethodPost:
		h.lifecycle(w, r, slug, "activate")
	case below == "/deactivate" && r.Method == http.MethodPost:
		h.lifecycle(w, r, slug, "deactivate")
	case below != "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.page(w, r, slug, below)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	list, err := h.Host.List(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		h.Host.logf("listing plugins: %v", err)
		return
	}
	props := map[string]any{"plugins": list, "can_upload": false}
	if flash := h.takeFlash(w, r); flash != nil {
		props["flash"] = flash
	}
	_ = h.Renderer.Render(w, r, IndexPage, props)
}

func (h *Handler) lifecycle(w http.ResponseWriter, r *http.Request, slug, action string) {
	ctx := withRequest(r.Context(), r)
	var err error
	var message string
	switch action {
	case "activate":
		err, message = h.Host.Activate(ctx, slug), "Plugin activated."
	case "deactivate":
		err, message = h.Host.Deactivate(ctx, slug), "Plugin deactivated."
	default:
		err, message = h.Host.Uninstall(ctx, slug), "Plugin uninstalled."
	}
	switch {
	case errors.Is(err, ErrUnknownPlugin):
		if wantsJSON(r) {
			http.Error(w, "Plugin not found.", http.StatusNotFound)
			return
		}
		h.toIndex(w, r, "error", "Plugin not found.")
	case errors.Is(err, ErrPackageManaged):
		h.toIndex(w, r, "error", "This plugin is installed by the package manager; remove it there.")
	case err != nil:
		h.Host.logf("%s %s: %v", action, slug, err)
		h.toIndex(w, r, "error", "The plugin could not be changed.")
	default:
		h.toIndex(w, r, "success", message)
	}
}

func wantsJSON(r *http.Request) bool {
	return r.Header.Get("X-Inertia") == "" && strings.Contains(r.Header.Get("Accept"), "application/json")
}

// toIndex answers a lifecycle route: a 303 to the index, with the flash in a
// short-lived cookie that the index reads once (this package keeps no session).
func (h *Handler) toIndex(w http.ResponseWriter, r *http.Request, kind, message string) {
	raw, _ := json.Marshal(map[string]string{kind: message})
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     h.prefix() + "/admin/plugins",
		MaxAge:   60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	http.Redirect(w, r, h.prefix()+"/admin/plugins", http.StatusSeeOther)
}

func (h *Handler) takeFlash(w http.ResponseWriter, r *http.Request) map[string]string {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: h.prefix() + "/admin/plugins", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	var flash map[string]string
	if json.Unmarshal(raw, &flash) != nil {
		return nil
	}
	out := map[string]string{}
	for _, kind := range []string{"success", "error"} {
		if v, ok := flash[kind]; ok {
			out[kind] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// page renders an active plugin's page as Escalated/Plugin/Page, with the
// props of the plugin's own GET /{component} endpoint, or {}.
func (h *Handler) page(w http.ResponseWriter, r *http.Request, slug, below string) {
	route, err := url.PathUnescape(strings.Trim(below, "/"))
	if err != nil || route == "" {
		http.NotFound(w, r)
		return
	}
	ctx := withRequest(r.Context(), r)
	e, err := h.Host.active(ctx, slug)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if e == nil {
		http.NotFound(w, r)
		return
	}
	var declared *Page
	for i, p := range e.manifest.Pages {
		if p.Component != "" && pageRoute(p) != "" && pageRoute(p) == route {
			declared = &e.manifest.Pages[i]
			break
		}
	}
	if declared == nil {
		http.NotFound(w, r)
		return
	}
	if !h.Host.allows(r, declared.Capability) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	props := map[string]any{}
	if rt, params, ok := matchRoute(e.endpoints, http.MethodGet, "/"+declared.Component); ok {
		req := &Request{Method: http.MethodGet, Path: rt.path, Params: params, Query: map[string]string{}, Headers: map[string]string{}}
		if result, err := h.Host.invoke(ctx, e, rt, req, false, endpointTimeout); err == nil {
			if env := asRecord(result); env != nil {
				if _, isEnvelope := env["__escalated_http"]; isEnvelope {
					result = env["body"]
				}
			}
			if resp, ok := result.(Response); ok {
				result = resp.Body
			}
			if resp, ok := result.(*Response); ok && resp != nil {
				result = resp.Body
			}
			if clean, err := roundTrip(result); err == nil {
				if m := asRecord(clean); m != nil {
					props = m
				}
			}
		}
	}
	layout := declared.Layout
	if layout == "" {
		layout = "admin"
	}
	_ = h.Renderer.Render(w, r, PluginPage, map[string]any{
		"plugin":    e.name,
		"component": declared.Component,
		"layout":    layout,
		"props":     props,
	})
}

// Endpoint serves {RoutePrefix}/api/plugins/{slug}{path}: an active plugin's
// endpoint, plus its capability when it declares one. Calls that change state
// must come from a page script (X-Requested-With or X-Inertia), which another
// site cannot send without a CORS preflight.
func (h *Handler) Endpoint(w http.ResponseWriter, r *http.Request) {
	h.serveRoute(w, r, "/api/plugins", false)
}

// Webhook serves {RoutePrefix}/webhooks/plugins/{slug}{path}: an active
// plugin's webhook, with no guard. The plugin authenticates the sender.
func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	h.serveRoute(w, r, "/webhooks/plugins", true)
}

func (h *Handler) serveRoute(w http.ResponseWriter, r *http.Request, base string, webhook bool) {
	if h.Host == nil {
		http.NotFound(w, r)
		return
	}
	path, ok := rest(r, h.prefix()+base)
	if !ok {
		http.NotFound(w, r)
		return
	}
	slug, below, ok := splitSlug(path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := withRequest(r.Context(), r)
	e, err := h.Host.active(ctx, slug)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	routes := []route(nil)
	if e != nil {
		routes = e.endpoints
		if webhook {
			routes = e.webhooks
		}
	}
	rt, params, found := matchRoute(routes, r.Method, below)
	if e == nil || !found {
		http.NotFound(w, r)
		return
	}
	if !webhook {
		if !h.Host.allows(r, rt.capability) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Requested-With") == "" && r.Header.Get("X-Inertia") == "" {
				http.Error(w, "Plugin endpoint calls must be sent by a page script.", http.StatusForbidden)
				return
			}
		}
	}

	req, err := h.Host.pluginRequest(r, rt.path, params)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	timeout := endpointTimeout
	if webhook {
		timeout = webhookTimeout
	}
	result, err := h.Host.invoke(ctx, e, rt, req, webhook, timeout)
	if err != nil {
		switch {
		case errors.Is(err, ErrRuntimeUnavailable), errors.Is(err, errNoHTTPContract):
			writeJSONMessage(w, http.StatusServiceUnavailable, "The plugin runtime is not available.")
		default:
			// The client gets a generic answer; the cause goes to the log.
			h.Host.logf("plugin %s %s %s failed: %v", e.name, rt.method, rt.path, err)
			writeJSONMessage(w, http.StatusInternalServerError, "The plugin could not handle this request.")
		}
		return
	}
	rep, err := makeReply(result)
	if err != nil {
		h.Host.logf("plugin %s %s %s: %v", e.name, rt.method, rt.path, err)
		writeJSONMessage(w, http.StatusBadGateway, err.Error())
		return
	}
	for name, value := range rep.headers {
		w.Header().Set(name, value)
	}
	w.WriteHeader(rep.status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(rep.body)
	}
}

func writeJSONMessage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

var errNoHTTPContract = errors.New("the plugin runtime does not support HTTP contract version 1")

// withheld headers never reach plugin code: the caller's browser and proxy
// credentials.
var withheld = map[string]bool{"cookie": true, "authorization": true, "x-xsrf-token": true, "x-csrf-token": true}

// pluginRequest builds the request a handler receives, as escalated-laravel's
// PluginHttp does for HTTP contract version 1.
func (h *Host) pluginRequest(r *http.Request, declared string, params map[string]string) (*Request, error) {
	var raw []byte
	if r.Body != nil {
		var err error
		raw, err = io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRequestBody))
		if err != nil {
			return nil, err
		}
	}
	headers := map[string]string{}
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		if withheld[lower] || strings.HasPrefix(lower, "proxy-") {
			continue
		}
		headers[lower] = strings.Join(values, ", ")
	}
	query := map[string]string{}
	for key, values := range r.URL.Query() {
		query[key] = strings.Join(values, ",")
	}
	ip := ""
	if h.clientIP != nil {
		ip = h.clientIP(r)
	} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	} else {
		ip = r.RemoteAddr
	}
	return &Request{
		Method:   r.Method,
		Path:     declared,
		Body:     parseBody(r.Header.Get("Content-Type"), raw),
		RawBody:  raw,
		Params:   params,
		Query:    query,
		Headers:  headers,
		ClientIP: ip,
	}, nil
}

func parseBody(contentType string, raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	ct, _, _ := mime.ParseMediaType(contentType)
	switch {
	case ct == "application/json" || strings.HasSuffix(ct, "+json"):
		return decodeJSON(raw)
	case ct == "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(raw))
		if err != nil {
			return nil
		}
		out := map[string]any{}
		for k, v := range values {
			if len(v) == 1 {
				out[k] = v[0]
				continue
			}
			list := make([]any, len(v))
			for i, item := range v {
				list[i] = item
			}
			out[k] = list
		}
		return out
	}
	return nil
}

// invoke runs one endpoint or webhook: the Go handler of a native plugin, or
// the runtime's endpoint/webhook call for an SDK plugin.
func (h *Host) invoke(ctx context.Context, e *entry, rt route, req *Request, webhook bool, timeout time.Duration) (any, error) {
	if !e.sdk {
		var result any
		err := h.runNative(ctx, timeout, func(c context.Context) error {
			v, err := rt.handler(c, h.context(e, requestFrom(ctx)), req)
			result = v
			return err
		})
		return result, err
	}
	if !h.bridge.supportsHTTP() {
		if _, err := h.bridge.ensure(); err != nil {
			return nil, err
		}
		if !h.bridge.supportsHTTP() {
			return nil, errNoHTTPContract
		}
	}
	method := "endpoint"
	if webhook {
		method = "webhook"
	}
	params := map[string]any{
		"plugin":        e.name,
		"method":        rt.method,
		"path":          rt.path,
		"body":          req.Body,
		"params":        req.Params,
		"query":         req.Query,
		"headers":       req.Headers,
		"httpContract":  1,
		"rawBodyBase64": base64.StdEncoding.EncodeToString(req.RawBody),
	}
	if req.ClientIP != "" {
		params["clientIp"] = req.ClientIP
	}
	raw, err := h.bridge.call(ctx, scope{plugins: []string{e.name}, request: requestFrom(ctx)}, method, params, timeout)
	if err != nil {
		return nil, err
	}
	return decodeJSON(raw), nil
}

// reply is an endpoint's HTTP answer.
type reply struct {
	status  int
	headers map[string]string
	body    []byte
}

var blockedResponseHeaders = map[string]bool{
	"connection": true, "content-length": true, "transfer-encoding": true, "keep-alive": true,
	"upgrade": true, "proxy-authenticate": true, "proxy-authorization": true, "te": true,
	"trailer": true, "set-cookie": true,
}

var headerToken = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9a-z-]+$")

// errInvalidResponse is a plugin answer this host will not send.
var errInvalidResponse = errors.New("invalid plugin HTTP response")

// makeReply turns a handler's result into the response: an httpResponse()
// envelope (or a Response) chooses the status, format and headers, validated
// independently of the SDK; anything else is JSON with 200.
func makeReply(result any) (reply, error) {
	jsonType := map[string]string{"content-type": "application/json; charset=utf-8"}
	switch v := result.(type) {
	case *Response:
		if v == nil {
			return reply{status: 200, headers: jsonType, body: []byte("null")}, nil
		}
		return makeReply(*v)
	case Response:
		status := v.Status
		if status == 0 {
			status = http.StatusOK
		}
		format := "json"
		if v.Text {
			format = "text"
		}
		headers := map[string]any{}
		for k, val := range v.Headers {
			headers[k] = val
		}
		return makeReply(map[string]any{"__escalated_http": float64(1), "status": float64(status), "format": format, "headers": headers, "body": v.Body})
	}

	env := asRecord(result)
	if env == nil || env["__escalated_http"] == nil {
		b, err := json.Marshal(result)
		if err != nil {
			return reply{}, fmt.Errorf("%w: %v", errInvalidResponse, err)
		}
		return reply{status: 200, headers: jsonType, body: b}, nil
	}
	version, _ := toFloat(env["__escalated_http"])
	status, isNum := toFloat(env["status"])
	format, _ := env["format"].(string)
	hdrs := asRecord(env["headers"])
	_, hasBody := env["body"]
	if version != 1 || !isNum || status != float64(int(status)) || status < 200 || status > 599 ||
		(format != "json" && format != "text") || hdrs == nil || !hasBody {
		return reply{}, errInvalidResponse
	}
	headers := map[string]string{}
	for name, value := range hdrs {
		lower := strings.ToLower(name)
		s, isString := value.(string)
		if !headerToken.MatchString(lower) || blockedResponseHeaders[lower] || !isString || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return reply{}, fmt.Errorf("%w: header %q", errInvalidResponse, name)
		}
		if _, dup := headers[lower]; dup {
			return reply{}, fmt.Errorf("%w: header %q", errInvalidResponse, name)
		}
		headers[lower] = s
	}
	if format == "text" {
		text, ok := env["body"].(string)
		if !ok {
			return reply{}, fmt.Errorf("%w: a text response needs a string body", errInvalidResponse)
		}
		if _, set := headers["content-type"]; !set {
			headers["content-type"] = "text/plain; charset=utf-8"
		}
		return reply{status: int(status), headers: headers, body: []byte(text)}, nil
	}
	b, err := json.Marshal(env["body"])
	if err != nil {
		return reply{}, fmt.Errorf("%w: %v", errInvalidResponse, err)
	}
	if _, set := headers["content-type"]; !set {
		headers["content-type"] = jsonType["content-type"]
	}
	return reply{status: int(status), headers: headers, body: b}, nil
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
