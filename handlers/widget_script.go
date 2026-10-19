package handlers

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"net/http"
	"strings"
)

// widgetBundle is the standalone widget from @escalated-dev/escalated
// (dist/widget/escalated-widget.js), vendored unmodified.
//
//go:embed assets/widget/escalated-widget.js
var widgetBundle []byte

var widgetBundleETag = func() string {
	sum := sha256.Sum256(widgetBundle)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}()

// WidgetScript serves the standalone widget bundle so any site can load it with
// <script src="https://help.example.com/support/widget/embed.js">. Mount it at
// GET {widget prefix}/embed.js, outside WidgetCORS: the script is public and is
// sent with Access-Control-Allow-Origin: *.
func WidgetScript(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "application/javascript; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=3600")
	h.Set("ETag", widgetBundleETag)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Access-Control-Allow-Origin", "*")

	if etagMatches(r.Header.Get("If-None-Match"), widgetBundleETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(widgetBundle)
	}
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}
