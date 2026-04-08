package middleware

import (
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
)

// KBSettingsProvider is a function that returns the current KB settings.
// This allows the middleware to read settings dynamically (e.g., from DB or config).
type KBSettingsProvider func() models.KBSettings

// RequireKBEnabled returns middleware that blocks requests when the knowledge base
// is disabled. It returns 404 to hide the existence of KB routes.
func RequireKBEnabled(provider KBSettingsProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			settings := provider()
			if !settings.KnowledgeBaseEnabled {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireKBPublic returns middleware that blocks unauthenticated requests to the
// knowledge base when it is not set to public. The authCheck function should return
// true if the current request is from an authenticated user.
func RequireKBPublic(provider KBSettingsProvider, authCheck func(r *http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			settings := provider()
			if !settings.KnowledgeBaseEnabled {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			if !settings.Public && !authCheck(r) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireKBFeedback returns middleware that blocks feedback endpoints when
// feedback is disabled in KB settings.
func RequireKBFeedback(provider KBSettingsProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			settings := provider()
			if !settings.KnowledgeBaseEnabled || !settings.FeedbackEnabled {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
