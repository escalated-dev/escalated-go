package handlers

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/store"
)

// WidgetAllowedOriginsSetting is the settings key admins use to list the sites
// that may embed the widget, one origin per line or comma separated. It is
// added to WidgetConfig.AllowedOrigins.
const WidgetAllowedOriginsSetting = "widget_allowed_origins"

// widgetPreflightMaxAge is how long a browser may cache a preflight answer.
const widgetPreflightMaxAge = "600"

var (
	widgetOriginPatternRe = regexp.MustCompile(`(?i)^(https?)://(\*\.)?([^/?#*]+?)/?$`)
	widgetOriginSplitRe   = regexp.MustCompile(`[\s,]+`)
)

type widgetCrossOriginKey struct{}

// WidgetCrossOrigin returns the Origin of a request that WidgetCORS let in from
// an allowed embedding site, or "" for every other request. Such a request is a
// guest: a host's UserIDFunc / AgentCheck / AdminCheck should treat it as
// signed out (Escalated.TicketAccessMiddleware already does).
func WidgetCrossOrigin(ctx context.Context) string {
	origin, _ := ctx.Value(widgetCrossOriginKey{}).(string)
	return origin
}

// NormalizeWidgetOriginPattern turns an allow-list entry into its canonical
// form ("https://shop.example.com", "https://*.example.com", lowercased, default
// port dropped), or returns "" when the entry is not a bare http(s) origin. A
// bare "*" is never accepted.
func NormalizeWidgetOriginPattern(entry string) string {
	m := widgetOriginPatternRe.FindStringSubmatch(strings.TrimSpace(entry))
	if m == nil {
		return ""
	}
	origin := normalizeWidgetOrigin(strings.ToLower(m[1]) + "://" + m[3])
	if origin == "" || m[2] == "" {
		return origin
	}
	return strings.Replace(origin, "://", "://*.", 1)
}

// ParseWidgetOrigins splits a newline or comma separated list into normalised
// patterns (duplicates removed) and the entries that are not valid origins.
func ParseWidgetOrigins(list string) (patterns []string, invalid []string) {
	seen := map[string]bool{}
	for _, entry := range widgetOriginSplitRe.Split(list, -1) {
		if entry == "" {
			continue
		}
		pattern := NormalizeWidgetOriginPattern(entry)
		if pattern == "" {
			invalid = append(invalid, entry)
			continue
		}
		if !seen[pattern] {
			seen[pattern] = true
			patterns = append(patterns, pattern)
		}
	}
	return patterns, invalid
}

// normalizeWidgetOrigin returns scheme://host[:port], lowercased with the
// default port dropped, or "" when origin is not a bare http(s) origin.
func normalizeWidgetOrigin(origin string) string {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if (scheme != "http" && scheme != "https") || host == "" || (u.Path != "" && u.Path != "/") {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return ""
		}
		if (scheme == "http" && n == 80) || (scheme == "https" && n == 443) {
			port = ""
		} else {
			port = strconv.Itoa(n)
		}
	}
	if port == "" {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

func widgetOriginMatches(pattern, origin string) bool {
	scheme, suffix, wildcard := strings.Cut(pattern, "://*.")
	if !wildcard {
		return pattern == origin
	}
	host, ok := strings.CutPrefix(origin, scheme+"://")
	return ok && len(host) > len(suffix)+1 && strings.HasSuffix(host, "."+suffix)
}

// widgetRequestCrossOrigin returns the request's normalised Origin when it
// names another site, or "" for same-origin requests and requests without an
// Origin header.
func widgetRequestCrossOrigin(r *http.Request) string {
	raw := r.Header.Get("Origin")
	if raw == "" || raw == "null" {
		return ""
	}
	origin := normalizeWidgetOrigin(raw)
	if origin == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if origin == normalizeWidgetOrigin(scheme+"://"+r.Host) {
		return ""
	}
	return origin
}

// WidgetOriginPolicy decides which other sites may embed the widget: the
// host's WidgetConfig.AllowedOrigins plus the widget_allowed_origins setting.
type WidgetOriginPolicy struct {
	configured []string
	store      store.Store
}

// NewWidgetOriginPolicy builds the policy for cfg. Invalid configured entries
// (including a bare "*") are ignored. s may be nil to use the config list only.
func NewWidgetOriginPolicy(cfg WidgetConfig, s store.Store) *WidgetOriginPolicy {
	configured, _ := ParseWidgetOrigins(cfg.AllowedOrigins)
	return &WidgetOriginPolicy{configured: configured, store: s}
}

// Patterns returns the configured patterns followed by those in the setting.
func (p *WidgetOriginPolicy) Patterns(ctx context.Context) []string {
	patterns := append([]string(nil), p.configured...)
	for _, pattern := range p.settingPatterns(ctx) {
		if !containsString(patterns, pattern) {
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

// Allows reports whether origin may embed the widget.
func (p *WidgetOriginPolicy) Allows(ctx context.Context, origin string) bool {
	origin = normalizeWidgetOrigin(origin)
	if origin == "" {
		return false
	}
	for _, pattern := range p.configured {
		if widgetOriginMatches(pattern, origin) {
			return true
		}
	}
	for _, pattern := range p.settingPatterns(ctx) {
		if widgetOriginMatches(pattern, origin) {
			return true
		}
	}
	return false
}

func (p *WidgetOriginPolicy) settingPatterns(ctx context.Context) []string {
	if p.store == nil {
		return nil
	}
	value, err := p.store.GetSetting(ctx, WidgetAllowedOriginsSetting)
	if err != nil || value == "" {
		return nil
	}
	patterns, _ := ParseWidgetOrigins(value)
	return patterns
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// WidgetCORS returns middleware for the widget routes (tickets, articles, chat
// and config; not embed.js, which WidgetScript serves to everyone). Only
// origins in cfg.AllowedOrigins or the widget_allowed_origins setting get CORS
// headers:
//
//   - a preflight (OPTIONS with Access-Control-Request-Method) is answered 204
//     here, with the grant only for an allowed origin;
//   - any other request from an allowed origin gets Access-Control-Allow-Origin
//     before the handler runs, so errors (403, 404, 422, 429) carry it too, and
//     runs as a guest (see WidgetCrossOrigin).
//
// Same-origin requests, requests without an Origin and requests from other
// sites pass through untouched. Credentials are never allowed.
func WidgetCORS(cfg WidgetConfig, s store.Store) func(http.Handler) http.Handler {
	policy := NewWidgetOriginPolicy(cfg, s)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := widgetRequestCrossOrigin(r)
			allowed := origin != "" && policy.Allows(r.Context(), origin)
			h := w.Header()

			if r.Method == http.MethodOptions && len(r.Header.Values("Access-Control-Request-Method")) > 0 {
				addVaryOrigin(h)
				if allowed {
					h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					h.Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Requested-With, X-Guest-Token")
					h.Set("Access-Control-Max-Age", widgetPreflightMaxAge)
					setWidgetCORSHeaders(h, origin)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if !allowed {
				next.ServeHTTP(w, r)
				return
			}

			setWidgetCORSHeaders(h, origin)
			addVaryOrigin(h)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), widgetCrossOriginKey{}, origin)))
		})
	}
}

func setWidgetCORSHeaders(h http.Header, origin string) {
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Expose-Headers", "Retry-After")
}

func addVaryOrigin(h http.Header) {
	for _, line := range h.Values("Vary") {
		for _, v := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(v), "Origin") {
				return
			}
		}
	}
	h.Add("Vary", "Origin")
}
