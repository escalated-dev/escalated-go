package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
	"strings"
)

// ErrorPageComponent is the shared frontend's refusal and failure screen.
const ErrorPageComponent = "Escalated/Error"

// errorPageMessages are the statuses Escalated/Error has wording for and the
// generic message each one gets. A handler's own error text never reaches the
// page.
var errorPageMessages = map[int]string{
	http.StatusForbidden:           "You are not authorized to perform this action.",
	http.StatusNotFound:            "The requested resource was not found.",
	419:                            "Your session has expired. Refresh the page and try again.",
	http.StatusTooManyRequests:     "Too many requests. Wait a moment and try again.",
	http.StatusInternalServerError: "Something went wrong on our end.",
	http.StatusServiceUnavailable:  "The service is temporarily unavailable. Try again shortly.",
}

// errorPageSkipped are the surfaces under the prefix that are not pages: the
// JSON API, webhooks, the widget, single sign-on and newsletter tracking.
var errorPageSkipped = []string{"/api", "/webhooks", "/widget", "/sso", "/n"}

// PageRenderer renders a named page (renderer.Renderer satisfies it).
type PageRenderer interface {
	Render(w http.ResponseWriter, r *http.Request, component string, props map[string]any) error
}

// InertiaErrorPage answers an Inertia visit to an Escalated page under prefix
// that ends in 403, 404, 419, 429, 500 or 503 with the Escalated/Error page,
// props {status, message} as Rails and Laravel send them. Without it the
// Inertia client gets a plain-text error back and can only show it in a modal
// over the screen the user was on.
//
// It covers a status a handler or middleware writes and a panic (a 500; the
// panic is logged with its stack). Left alone: requests without X-Inertia
// (full page loads, API clients), paths outside prefix or under the non-page
// surfaces above, responses that are already Inertia pages, and every other
// status, redirects included. A full page load's panic is re-raised for the
// host's recoverer. In debug mode (ErrorPageDebug) a 500 is left alone too:
// the handler's response goes out as written and a panic is re-raised.
func InertiaErrorPage(pages PageRenderer, prefix string, opts ...ErrorPageOption) func(http.Handler) http.Handler {
	var o errorPageOptions
	for _, opt := range opts {
		opt(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Inertia") == "" || !isErrorPagePath(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}

			ew := &errorPageWriter{ResponseWriter: w, debug: o.debug}
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler || ew.wroteHeader || o.debug {
						panic(p)
					}
					log.Printf("escalated: panic serving %s %s: %v\n%s", r.Method, r.URL.Path, p, debug.Stack())
					ew.status = http.StatusInternalServerError
				}
				if ew.status != 0 {
					renderErrorPage(pages, w, r, ew.status)
				}
			}()

			next.ServeHTTP(ew, r)
		})
	}
}

// ErrorPageOption configures InertiaErrorPage.
type ErrorPageOption func(*errorPageOptions)

type errorPageOptions struct {
	debug bool
}

// ErrorPageDebug sets debug mode (escalated.Config.Debug). While it is on a
// 500 is not turned into the page: the handler's own 500 response goes out as
// written, and a panic is re-raised for the host's recoverer, the way
// escalated-laravel leaves a 500 to the framework's debug page under
// APP_DEBUG. Every other status still gets the page. Off by default.
func ErrorPageDebug(on bool) ErrorPageOption {
	return func(o *errorPageOptions) { o.debug = on }
}

func isErrorPagePath(path, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	if path != prefix && !strings.HasPrefix(path, prefix+"/") {
		return false
	}
	rest := strings.TrimPrefix(path, prefix)
	for _, skip := range errorPageSkipped {
		if rest == skip || strings.HasPrefix(rest, skip+"/") {
			return false
		}
	}
	return true
}

func renderErrorPage(pages PageRenderer, w http.ResponseWriter, r *http.Request, status int) {
	h := w.Header()
	for _, key := range []string{"Content-Type", "Content-Length", "X-Content-Type-Options"} {
		h.Del(key)
	}
	sw := &statusWriter{ResponseWriter: w, status: status}
	props := map[string]any{"status": status, "message": errorPageMessages[status]}
	if err := pages.Render(sw, r, ErrorPageComponent, props); err != nil && !sw.wroteHeader {
		http.Error(w, http.StatusText(status), status)
	}
}

// errorPageWriter holds back an error status the page has wording for (and
// the body that goes with it), passing everything else through.
type errorPageWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	// debug passes a 500 through as written (ErrorPageDebug).
	debug bool
}

func (w *errorPageWriter) WriteHeader(code int) {
	if w.status != 0 || w.wroteHeader {
		return
	}
	if _, ok := errorPageMessages[code]; ok && w.Header().Get("X-Inertia") == "" &&
		(!w.debug || code != http.StatusInternalServerError) {
		w.status = code
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *errorPageWriter) Write(b []byte) (int, error) {
	if w.status == 0 && !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.status != 0 {
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func (w *errorPageWriter) Flush() {
	if w.status != 0 {
		return
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *errorPageWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// statusWriter writes the page with the error's status instead of 200.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(w.status)
	}
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.WriteHeader(w.status)
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
