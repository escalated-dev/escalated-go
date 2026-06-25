package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ErrUnauthorized signals an authentication failure from an APIAuth callback
// (mapped to HTTP 401). Any other non-nil error maps to 422.
var ErrUnauthorized = errors.New("escalated: unauthorized")

// APIAuth holds host-app-provided authentication callbacks for the general
// JSON API (/api/auth/*). Escalated owns no credentials or sessions, so it
// ships no password-hashing or session dependency; the host implements only
// the callbacks it needs. A nil callback makes its endpoint respond 501.
//
// Each callback returns the JSON payload to send (e.g. token + user) and an
// error: ErrUnauthorized for a failed login/invalid token (401), or any other
// error for a client error (422).
type APIAuth struct {
	Login         func(ctx context.Context, params map[string]any) (map[string]any, error)
	Register      func(ctx context.Context, params map[string]any) (map[string]any, error)
	Validate      func(ctx context.Context, token string) (map[string]any, error)
	Refresh       func(ctx context.Context, token string) (map[string]any, error)
	UpdateProfile func(ctx context.Context, token string, attrs map[string]any) (map[string]any, error)
	Logout        func(ctx context.Context, token string) error
}

// AuthHandler serves /api/auth/* endpoints, delegating to host callbacks.
type AuthHandler struct {
	auth APIAuth
}

// NewAuthHandler creates an AuthHandler from the host's auth callbacks.
func NewAuthHandler(auth APIAuth) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// Login authenticates a credentials payload via the host.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	h.withParams(w, r, h.auth.Login)
}

// Register creates an account via the host.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	h.withParams(w, r, h.auth.Register)
}

// Me validates the bearer token and returns the associated user.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	h.withToken(w, r, h.auth.Validate)
}

// Refresh exchanges the bearer token for a fresh one via the host.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	h.withToken(w, r, h.auth.Refresh)
}

// Validate validates a token supplied in the JSON body.
func (h *AuthHandler) Validate(w http.ResponseWriter, r *http.Request) {
	if h.auth.Validate == nil {
		authNotConfigured(w)
		return
	}
	params, ok := authDecodeParams(w, r)
	if !ok {
		return
	}
	token, _ := params["token"].(string)
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "token is required"})
		return
	}
	result, err := h.auth.Validate(r.Context(), token)
	authRespond(w, result, err)
}

// Profile updates the authenticated user's profile via the host.
func (h *AuthHandler) Profile(w http.ResponseWriter, r *http.Request) {
	if h.auth.UpdateProfile == nil {
		authNotConfigured(w)
		return
	}
	token, ok := authBearerToken(r)
	if !ok {
		authUnauthorized(w)
		return
	}
	attrs, ok := authDecodeParams(w, r)
	if !ok {
		return
	}
	result, err := h.auth.UpdateProfile(r.Context(), token, attrs)
	authRespond(w, result, err)
}

// Logout invalidates the bearer token via the host (best-effort).
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token, _ := authBearerToken(r)
	if h.auth.Logout != nil {
		_ = h.auth.Logout(r.Context(), token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"success": true}})
}

func (h *AuthHandler) withParams(w http.ResponseWriter, r *http.Request, fn func(context.Context, map[string]any) (map[string]any, error)) {
	if fn == nil {
		authNotConfigured(w)
		return
	}
	params, ok := authDecodeParams(w, r)
	if !ok {
		return
	}
	result, err := fn(r.Context(), params)
	authRespond(w, result, err)
}

func (h *AuthHandler) withToken(w http.ResponseWriter, r *http.Request, fn func(context.Context, string) (map[string]any, error)) {
	if fn == nil {
		authNotConfigured(w)
		return
	}
	token, ok := authBearerToken(r)
	if !ok {
		authUnauthorized(w)
		return
	}
	result, err := fn(r.Context(), token)
	authRespond(w, result, err)
}

func authDecodeParams(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	params := map[string]any{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
			return nil, false
		}
	}
	return params, true
}

func authBearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}
	if strings.HasPrefix(header, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		return token, token != ""
	}
	return header, true
}

func authRespond(w http.ResponseWriter, result map[string]any, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		authUnauthorized(w)
	case err != nil:
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"data": result})
	}
}

func authNotConfigured(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "authentication is not configured on this host"})
}

func authUnauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
}
