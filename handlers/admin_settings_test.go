package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/renderer"
)

// newAdminHandlerForTest builds an AdminHandler wired to a fresh mock
// store. The renderer isn't exercised by the public-tickets endpoints
// (they use writeJSON) — a JSONRenderer keeps the constructor happy.
func newAdminHandlerForTest() (*AdminHandler, *handlerMockStore) {
	ms := newHandlerMockStore()
	h := NewAdminHandler(ms, renderer.NewJSONRenderer())
	return h, ms
}

func decodeSettings(t *testing.T, body *bytes.Buffer) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body.Bytes(), &out); err != nil {
		t.Fatalf("decode response body: %v; body: %s", err, body.String())
	}
	return out
}

func TestAdminHandler_GetPublicTicketsSettings_DefaultsWhenEmpty(t *testing.T) {
	h, _ := newAdminHandlerForTest()

	req := httptest.NewRequest(http.MethodGet, "/admin/settings/public-tickets", nil)
	rec := httptest.NewRecorder()
	h.GetPublicTicketsSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	got := decodeSettings(t, rec.Body)
	if got["guest_policy_mode"] != "unassigned" {
		t.Errorf("mode: want unassigned, got %v", got["guest_policy_mode"])
	}
	if got["guest_policy_user_id"] != nil {
		t.Errorf("user_id: want nil, got %v", got["guest_policy_user_id"])
	}
	if got["guest_policy_signup_url_template"] != "" {
		t.Errorf("template: want empty, got %v", got["guest_policy_signup_url_template"])
	}
}

func TestAdminHandler_UpdatePublicTicketsSettings_GuestUserMode(t *testing.T) {
	h, ms := newAdminHandlerForTest()

	body := `{"guest_policy_mode":"guest_user","guest_policy_user_id":42,"guest_policy_signup_url_template":"https://ignored.example"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The response should reflect the persisted state (read-after-write).
	got := decodeSettings(t, rec.Body)
	if got["guest_policy_mode"] != "guest_user" {
		t.Errorf("mode: got %v", got["guest_policy_mode"])
	}
	// JSON-decoded int comes back as float64; compare via conversion.
	if id, ok := got["guest_policy_user_id"].(float64); !ok || int64(id) != 42 {
		t.Errorf("user_id: got %v (%T)", got["guest_policy_user_id"], got["guest_policy_user_id"])
	}
	// signup_url_template should be cleared because mode != prompt_signup.
	if got["guest_policy_signup_url_template"] != "" {
		t.Errorf("template should be cleared on guest_user mode, got %v",
			got["guest_policy_signup_url_template"])
	}

	// And the underlying store should also reflect that.
	if ms.settings["guest_policy_signup_url_template"] != "" {
		t.Errorf("store template should be empty, got %q",
			ms.settings["guest_policy_signup_url_template"])
	}
}

func TestAdminHandler_UpdatePublicTicketsSettings_PromptSignupMode(t *testing.T) {
	h, ms := newAdminHandlerForTest()

	body := `{"guest_policy_mode":"prompt_signup","guest_policy_user_id":99,"guest_policy_signup_url_template":"https://example.com/join?t={{token}}"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	got := decodeSettings(t, rec.Body)
	if got["guest_policy_mode"] != "prompt_signup" {
		t.Errorf("mode: got %v", got["guest_policy_mode"])
	}
	// user_id should be cleared on prompt_signup mode.
	if got["guest_policy_user_id"] != nil {
		t.Errorf("user_id should be nil on prompt_signup mode, got %v", got["guest_policy_user_id"])
	}
	if got["guest_policy_signup_url_template"] != "https://example.com/join?t={{token}}" {
		t.Errorf("template: got %v", got["guest_policy_signup_url_template"])
	}

	if ms.settings["guest_policy_user_id"] != "" {
		t.Errorf("store user_id should be empty on prompt_signup mode, got %q",
			ms.settings["guest_policy_user_id"])
	}
}

func TestAdminHandler_UpdatePublicTicketsSettings_UnknownModeCoercesToUnassigned(t *testing.T) {
	h, _ := newAdminHandlerForTest()

	body := `{"guest_policy_mode":"bogus","guest_policy_user_id":5,"guest_policy_signup_url_template":"ignored"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got := decodeSettings(t, rec.Body)
	if got["guest_policy_mode"] != "unassigned" {
		t.Errorf("want unassigned, got %v", got["guest_policy_mode"])
	}
	if got["guest_policy_user_id"] != nil {
		t.Errorf("user_id should be cleared for unassigned mode, got %v", got["guest_policy_user_id"])
	}
}

func TestAdminHandler_UpdatePublicTicketsSettings_TruncatesLongTemplate(t *testing.T) {
	h, _ := newAdminHandlerForTest()

	long := strings.Repeat("x", 1000)
	body := `{"guest_policy_mode":"prompt_signup","guest_policy_signup_url_template":"` + long + `"}`
	req := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got := decodeSettings(t, rec.Body)
	template, _ := got["guest_policy_signup_url_template"].(string)
	if len(template) != 500 {
		t.Errorf("template should be truncated to 500 chars, got %d", len(template))
	}
}

func TestAdminHandler_UpdatePublicTicketsSettings_ZeroUserIDClearsField(t *testing.T) {
	h, _ := newAdminHandlerForTest()

	body := `{"guest_policy_mode":"guest_user","guest_policy_user_id":0}`
	req := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	got := decodeSettings(t, rec.Body)
	// Zero user id is semantically "no choice made" — surface as null.
	if got["guest_policy_user_id"] != nil {
		t.Errorf("zero user_id should surface as nil, got %v", got["guest_policy_user_id"])
	}
}

func TestAdminHandler_UpdatePublicTicketsSettings_InvalidJSONReturns400(t *testing.T) {
	h, _ := newAdminHandlerForTest()

	req := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAdminHandler_ModeSwitchClearsStaleFields(t *testing.T) {
	h, ms := newAdminHandlerForTest()

	// Start on guest_user with a user_id.
	body1 := `{"guest_policy_mode":"guest_user","guest_policy_user_id":42}`
	req1 := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body1))
	h.UpdatePublicTicketsSettings(httptest.NewRecorder(), req1)
	if ms.settings["guest_policy_user_id"] != "42" {
		t.Fatalf("setup failed: user_id not stored")
	}

	// Switch to unassigned — user_id should clear.
	body2 := `{"guest_policy_mode":"unassigned"}`
	req2 := httptest.NewRequest(http.MethodPut, "/admin/settings/public-tickets", strings.NewReader(body2))
	rec := httptest.NewRecorder()
	h.UpdatePublicTicketsSettings(rec, req2)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ms.settings["guest_policy_user_id"] != "" {
		t.Errorf("user_id should be cleared after switch to unassigned, got %q",
			ms.settings["guest_policy_user_id"])
	}
}
