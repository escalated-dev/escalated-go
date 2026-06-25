package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthLoginNotConfigured(t *testing.T) {
	h := NewAuthHandler(APIAuth{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`))

	h.Login(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("want 501, got %d", rec.Code)
	}
}

func TestAuthLoginSuccess(t *testing.T) {
	h := NewAuthHandler(APIAuth{
		Login: func(_ context.Context, params map[string]any) (map[string]any, error) {
			return map[string]any{"token": "abc", "email": params["email"]}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"a@b.com"}`))

	h.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data["token"] != "abc" {
		t.Fatalf("want token abc, got %v", body.Data["token"])
	}
	if body.Data["email"] != "a@b.com" {
		t.Fatalf("login params not forwarded, got %v", body.Data["email"])
	}
}

func TestAuthLoginUnauthorized(t *testing.T) {
	h := NewAuthHandler(APIAuth{
		Login: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, ErrUnauthorized
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`))

	h.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestAuthLoginClientError(t *testing.T) {
	h := NewAuthHandler(APIAuth{
		Login: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return nil, errors.New("email is required")
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{}`))

	h.Login(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", rec.Code)
	}
}

func TestAuthMeRequiresBearerToken(t *testing.T) {
	h := NewAuthHandler(APIAuth{
		Validate: func(_ context.Context, token string) (map[string]any, error) {
			return map[string]any{"id": token}, nil
		},
	})

	rec := httptest.NewRecorder()
	h.Me(rec, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: want 401, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer tok123")
	h.Me(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with token: want 200, got %d", rec.Code)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.Data["id"] != "tok123" {
		t.Fatalf("want id tok123, got %v", body.Data["id"])
	}
}

func TestAuthLogoutAlwaysSucceeds(t *testing.T) {
	called := false
	h := NewAuthHandler(APIAuth{
		Logout: func(_ context.Context, token string) error {
			called = token == "tok123"
			return nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer tok123")

	h.Logout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !called {
		t.Fatalf("logout callback not invoked with token")
	}
}

func TestAuthLogoutNoCallbackStill200(t *testing.T) {
	h := NewAuthHandler(APIAuth{})
	rec := httptest.NewRecorder()
	h.Logout(rec, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}

func TestAuthValidateRequiresToken(t *testing.T) {
	h := NewAuthHandler(APIAuth{
		Validate: func(_ context.Context, _ string) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/validate", strings.NewReader(`{}`))

	h.Validate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}
