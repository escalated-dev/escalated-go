// Package middleware provides HTTP middleware for Escalated route protection.
package middleware

import (
	"net/http"
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
