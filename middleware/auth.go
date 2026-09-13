// Package middleware provides HTTP middleware for Escalated route protection.
package middleware

import (
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
)

// RequireAdmin returns middleware that rejects requests where adminCheck returns false.
func RequireAdmin(adminCheck func(r *http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !adminCheck(r) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAgent returns middleware that rejects requests where agentCheck returns false.
func RequireAgent(agentCheck func(r *http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !agentCheck(r) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAgentOrAdmin returns middleware that requires either agent or admin access.
func RequireAgentOrAdmin(agentCheck, adminCheck func(r *http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !agentCheck(r) && !adminCheck(r) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireUser returns middleware that rejects requests with no signed-in user.
// userID is the host's Config.UserIDFunc, and an empty id means nobody is signed
// in. It answers 401 rather than 403, because the same request may succeed once
// the caller authenticates.
func RequireUser(userID func(r *http.Request) models.UserID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if userID == nil || userID(r).Empty() {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
