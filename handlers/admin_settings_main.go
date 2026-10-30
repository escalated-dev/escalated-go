package handlers

import (
	"context"
	"net/http"
	"strings"
)

// The main admin Settings page, as escalated-laravel's SettingsController:
// Escalated/Admin/Settings with settings and supported_settings. The page
// lists every control Laravel has; supported_settings names the ones this
// package stores and honours, so the page hides the rest and the save ignores
// any other key:
//
//   - knowledge_base_enabled and knowledge_base_public, which the widget
//     reads (KBSettingsFromStore);
//   - widget_allowed_origins, the sites that may embed the widget (also
//     saved through PUT /admin/settings/widget).
//
// Guest tickets, attachments, reference prefix, inbound mail and the widget's
// appearance come from Config here, not from the settings table.
var supportedMainSettings = []string{SettingKnowledgeBaseEnabled, SettingKnowledgeBasePublic, WidgetAllowedOriginsSetting}

// SettingTwoFactorRequired is the runtime "require two-factor" setting
// (escalated-laravel's two_factor_required). It is offered and saved only
// while two-factor is on; Admin/Settings/TwoFactor posts it alone.
const SettingTwoFactorRequired = "two_factor_required"

func (h *AdminHandler) supportedSettings() []string {
	if h.TwoFactorRequired == nil {
		return supportedMainSettings
	}
	return append(append([]string{}, supportedMainSettings...), SettingTwoFactorRequired)
}

func (h *AdminHandler) mainSettings(ctx context.Context) (map[string]any, error) {
	kb := KBSettingsFromStore(ctx, h.store)
	origins, err := h.store.GetSetting(ctx, WidgetAllowedOriginsSetting)
	if err != nil {
		return nil, err
	}
	settings := map[string]any{
		SettingKnowledgeBaseEnabled: kb.KnowledgeBaseEnabled,
		SettingKnowledgeBasePublic:  kb.Public,
		WidgetAllowedOriginsSetting: origins,
	}
	if h.TwoFactorRequired != nil {
		settings[SettingTwoFactorRequired] = h.TwoFactorRequired(ctx)
	}
	return settings, nil
}

// SettingsPage handles GET /admin/settings (escalated.admin.settings).
func (h *AdminHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	h.settingsPage(w, r, nil)
}

func (h *AdminHandler) settingsPage(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	settings, err := h.mainSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderSettingsPage(w, r, "Escalated/Admin/Settings", map[string]any{
		"settings":           settings,
		"supported_settings": h.supportedSettings(),
	}, errs)
}

// UpdateSettings handles POST /admin/settings (escalated.admin.settings.update).
// Each supported key is optional, as Laravel's `sometimes` rules: a key left
// out keeps its stored value.
func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	errs := map[string]string{}
	var values [][2]string
	booleans := []string{SettingKnowledgeBaseEnabled, SettingKnowledgeBasePublic}
	if h.TwoFactorRequired != nil {
		booleans = append(booleans, SettingTwoFactorRequired)
	}
	for _, key := range booleans {
		if _, given := in[key]; !given {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(in.Get(key))) {
		case "true", "1":
			values = append(values, [2]string{key, "1"})
		case "false", "0":
			values = append(values, [2]string{key, "0"})
		default:
			errs[key] = "The " + strings.ReplaceAll(key, "_", " ") + " field must be true or false."
		}
	}
	if _, given := in[WidgetAllowedOriginsSetting]; given {
		origins := strings.TrimSpace(in.Get(WidgetAllowedOriginsSetting))
		if len([]rune(origins)) > 5000 {
			errs[WidgetAllowedOriginsSetting] = "The widget allowed origins field must not be greater than 5000 characters."
		} else if _, invalid := ParseWidgetOrigins(origins); len(invalid) > 0 {
			errs[WidgetAllowedOriginsSetting] = "Each allowed origin must look like https://shop.example.com or https://*.example.com (no path, no bare *). Invalid: " + strings.Join(invalid, ", ")
		} else {
			values = append(values, [2]string{WidgetAllowedOriginsSetting, origins})
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.settingsPage(w, r, errs) })
		return
	}

	if !h.saveSettings(w, r.Context(), values) {
		return
	}
	h.settingsSaved(w, r, "/settings", func() (any, error) { return h.mainSettings(r.Context()) })
}
