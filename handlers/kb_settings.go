package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

// KB setting keys, shared with the Laravel port's admin settings.
const (
	SettingKnowledgeBaseEnabled = "knowledge_base_enabled"
	SettingKnowledgeBasePublic  = "knowledge_base_public"
)

// KBSettingsFromStore reads the knowledge base switches from the settings
// table. Both default to on when unset, as in Laravel; "0", "false", "no" and
// "off" turn one off.
func KBSettingsFromStore(ctx context.Context, s store.Store) models.KBSettings {
	kb := models.DefaultKBSettings()
	kb.KnowledgeBaseEnabled = settingBool(ctx, s, SettingKnowledgeBaseEnabled, true)
	kb.Public = settingBool(ctx, s, SettingKnowledgeBasePublic, true)
	return kb
}

func settingBool(ctx context.Context, s store.Store, key string, def bool) bool {
	if s == nil {
		return def
	}
	v, err := s.GetSetting(ctx, key)
	if err != nil {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return def
	case "0", "false", "no", "off":
		return false
	}
	return true
}

// widgetKBAllowed answers 404 when the knowledge base is off and 403 when it
// is private and the caller is not signed in (Laravel WidgetController).
func (h *WidgetHandler) widgetKBAllowed(w http.ResponseWriter, r *http.Request) bool {
	kb := KBSettingsFromStore(r.Context(), h.store)
	if !kb.KnowledgeBaseEnabled {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "knowledge base is disabled", "message": "Not found."})
		return false
	}
	if !kb.Public && !h.signedIn(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "knowledge base is private", "message": "Forbidden."})
		return false
	}
	return true
}

func (h *WidgetHandler) signedIn(r *http.Request) bool {
	return h.SignedIn != nil && h.SignedIn(r)
}
