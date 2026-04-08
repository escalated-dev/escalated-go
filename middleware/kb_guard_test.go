package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/escalated-dev/escalated-go/models"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestRequireKBEnabled(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		wantStatus int
	}{
		{name: "KB enabled", enabled: true, wantStatus: http.StatusOK},
		{name: "KB disabled", enabled: false, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := func() models.KBSettings {
				return models.KBSettings{KnowledgeBaseEnabled: tt.enabled}
			}
			mw := RequireKBEnabled(provider)
			handler := mw(okHandler())

			req := httptest.NewRequest(http.MethodGet, "/kb/articles", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestRequireKBPublic(t *testing.T) {
	tests := []struct {
		name          string
		enabled       bool
		public        bool
		authenticated bool
		wantStatus    int
	}{
		{name: "public KB, no auth", enabled: true, public: true, authenticated: false, wantStatus: http.StatusOK},
		{name: "public KB, with auth", enabled: true, public: true, authenticated: true, wantStatus: http.StatusOK},
		{name: "private KB, no auth", enabled: true, public: false, authenticated: false, wantStatus: http.StatusForbidden},
		{name: "private KB, with auth", enabled: true, public: false, authenticated: true, wantStatus: http.StatusOK},
		{name: "KB disabled", enabled: false, public: true, authenticated: false, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := func() models.KBSettings {
				return models.KBSettings{
					KnowledgeBaseEnabled: tt.enabled,
					Public:               tt.public,
				}
			}
			authCheck := func(_ *http.Request) bool { return tt.authenticated }
			mw := RequireKBPublic(provider, authCheck)
			handler := mw(okHandler())

			req := httptest.NewRequest(http.MethodGet, "/kb/articles", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestRequireKBFeedback(t *testing.T) {
	tests := []struct {
		name            string
		enabled         bool
		feedbackEnabled bool
		wantStatus      int
	}{
		{name: "KB and feedback enabled", enabled: true, feedbackEnabled: true, wantStatus: http.StatusOK},
		{name: "KB enabled, feedback disabled", enabled: true, feedbackEnabled: false, wantStatus: http.StatusNotFound},
		{name: "KB disabled", enabled: false, feedbackEnabled: true, wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := func() models.KBSettings {
				return models.KBSettings{
					KnowledgeBaseEnabled: tt.enabled,
					FeedbackEnabled:      tt.feedbackEnabled,
				}
			}
			mw := RequireKBFeedback(provider)
			handler := mw(okHandler())

			req := httptest.NewRequest(http.MethodPost, "/kb/articles/1/feedback", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestDefaultKBSettings(t *testing.T) {
	s := models.DefaultKBSettings()
	if s.KnowledgeBaseEnabled {
		t.Error("KB should be disabled by default")
	}
	if !s.Public {
		t.Error("KB should be public by default")
	}
	if !s.FeedbackEnabled {
		t.Error("feedback should be enabled by default")
	}
}
