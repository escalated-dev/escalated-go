package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/services"
)

// The admin Settings sub-pages, as escalated-laravel's CsatSettingsController,
// DataRetentionController, DatabaseConnectionController and
// EmailSettingsController: Escalated/Admin/Settings/{CsatSettings,
// DataRetention, DatabaseConnection, EmailSettings}. Everything lives in the
// settings store, as string values. A save answers a page visit with a 303
// back to the page (or the page again with errors) and an API client with the
// stored values as JSON (or a 422).

// --- CSAT ---

const defaultCsatQuestion = "How would you rate your support experience?"

var (
	csatScales   = []string{"1-3", "1-5"}
	csatTriggers = []string{"on_resolve", "delayed", "manual"}
)

func (h *AdminHandler) settingOr(ctx context.Context, key, fallback string) (string, error) {
	v, err := h.store.GetSetting(ctx, key)
	if err != nil {
		return "", err
	}
	if v == "" {
		return fallback, nil
	}
	return v, nil
}

func (h *AdminHandler) csatSettings(ctx context.Context) (map[string]any, error) {
	question, err := h.settingOr(ctx, "csat_question_text", defaultCsatQuestion)
	if err != nil {
		return nil, err
	}
	scale, err := h.settingOr(ctx, "csat_scale", "1-5")
	if err != nil {
		return nil, err
	}
	trigger, err := h.settingOr(ctx, "csat_delivery_trigger", "on_resolve")
	if err != nil {
		return nil, err
	}
	delay, err := h.settingOr(ctx, "csat_delay_hours", "0")
	if err != nil {
		return nil, err
	}
	hours, _ := strconv.Atoi(delay)
	return map[string]any{
		"csat_question_text":    question,
		"csat_scale":            scale,
		"csat_delivery_trigger": trigger,
		"csat_delay_hours":      hours,
	}, nil
}

func (h *AdminHandler) renderSettingsPage(w http.ResponseWriter, r *http.Request, component string, props map[string]any, errs map[string]string) {
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, component, props)
}

// CsatSettingsPage handles GET /admin/settings/csat
// (escalated.admin.settings.csat).
func (h *AdminHandler) CsatSettingsPage(w http.ResponseWriter, r *http.Request) {
	h.csatPage(w, r, nil)
}

func (h *AdminHandler) csatPage(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	settings, err := h.csatSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderSettingsPage(w, r, "Escalated/Admin/Settings/CsatSettings", map[string]any{"settings": settings}, errs)
}

// UpdateCsatSettings handles POST /admin/settings/csat
// (escalated.admin.settings.csat.update).
func (h *AdminHandler) UpdateCsatSettings(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	question := in.Get("csat_question_text")
	scale := in.Get("csat_scale")
	trigger := in.Get("csat_delivery_trigger")
	delay := strings.TrimSpace(in.Get("csat_delay_hours"))

	errs := map[string]string{}
	switch {
	case strings.TrimSpace(question) == "":
		errs["csat_question_text"] = requiredMessage("csat question text")
	case len([]rune(question)) > 500:
		errs["csat_question_text"] = "The csat question text field must not be greater than 500 characters."
	}
	if !oneOf(scale, csatScales) {
		errs["csat_scale"] = "The selected csat scale is invalid."
	}
	if !oneOf(trigger, csatTriggers) {
		errs["csat_delivery_trigger"] = "The selected csat delivery trigger is invalid."
	}
	hours, convErr := strconv.Atoi(delay)
	switch {
	case delay == "":
		errs["csat_delay_hours"] = requiredMessage("csat delay hours")
	case convErr != nil:
		errs["csat_delay_hours"] = "The csat delay hours field must be an integer."
	case hours < 0 || hours > 168:
		errs["csat_delay_hours"] = "The csat delay hours field must be between 0 and 168."
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.csatPage(w, r, errs) })
		return
	}

	values := [][2]string{
		{"csat_question_text", question},
		{"csat_scale", scale},
		{"csat_delivery_trigger", trigger},
		{"csat_delay_hours", strconv.Itoa(hours)},
	}
	if !h.saveSettings(w, r.Context(), values) {
		return
	}
	h.settingsSaved(w, r, "/settings/csat", func() (any, error) { return h.csatSettings(r.Context()) })
}

// --- Data retention ---

var (
	retentionPeriods      = []string{"never", "1_year", "2_years", "3_years", "5_years"}
	auditRetentionPeriods = []string{"never", "90_days", "180_days", "365_days"}
)

func (h *AdminHandler) retentionSettings(ctx context.Context) (map[string]any, error) {
	settings := map[string]any{}
	for _, key := range []string{"retention_closed_tickets", "retention_attachments", "retention_audit_logs"} {
		v, err := h.settingOr(ctx, key, "never")
		if err != nil {
			return nil, err
		}
		settings[key] = v
	}
	gdpr, err := h.store.GetSetting(ctx, "retention_user_data_gdpr")
	if err != nil {
		return nil, err
	}
	settings["retention_user_data_gdpr"] = gdpr == "1" || gdpr == "true"
	return settings, nil
}

// DataRetentionPage handles GET /admin/settings/data-retention
// (escalated.admin.settings.data-retention).
func (h *AdminHandler) DataRetentionPage(w http.ResponseWriter, r *http.Request) {
	h.dataRetentionPage(w, r, nil)
}

func (h *AdminHandler) dataRetentionPage(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	settings, err := h.retentionSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	preview := services.RetentionPreview{}
	if h.Retention != nil {
		preview = h.Retention.Preview(r.Context(), time.Now())
	}
	h.renderSettingsPage(w, r, "Escalated/Admin/Settings/DataRetention", map[string]any{
		"settings": settings,
		"purgePreview": map[string]any{
			"tickets":     preview.Tickets,
			"attachments": preview.Attachments,
			"audit_logs":  preview.AuditLogs,
		},
	}, errs)
}

// UpdateDataRetention handles POST /admin/settings/data-retention
// (escalated.admin.settings.data-retention.update). The periods take effect
// when the host runs Escalated.PurgeExpired (or StartRetentionPurger).
func (h *AdminHandler) UpdateDataRetention(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	errs := map[string]string{}
	for key, allowed := range map[string][]string{
		"retention_closed_tickets": retentionPeriods,
		"retention_attachments":    retentionPeriods,
		"retention_audit_logs":     auditRetentionPeriods,
	} {
		v := in.Get(key)
		if v == "" {
			errs[key] = requiredMessage(strings.ReplaceAll(key, "_", " "))
		} else if !oneOf(v, allowed) {
			errs[key] = "The selected " + strings.ReplaceAll(key, "_", " ") + " is invalid."
		}
	}
	gdpr := ""
	switch raw := strings.TrimSpace(in.Get("retention_user_data_gdpr")); {
	case !in.Has("retention_user_data_gdpr") || raw == "":
		errs["retention_user_data_gdpr"] = requiredMessage("retention user data gdpr")
	case raw == "true" || raw == "1":
		gdpr = "1"
	case raw == "false" || raw == "0":
		gdpr = "0"
	default:
		errs["retention_user_data_gdpr"] = "The retention user data gdpr field must be true or false."
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.dataRetentionPage(w, r, errs) })
		return
	}

	values := [][2]string{
		{"retention_closed_tickets", in.Get("retention_closed_tickets")},
		{"retention_attachments", in.Get("retention_attachments")},
		{"retention_audit_logs", in.Get("retention_audit_logs")},
		{"retention_user_data_gdpr", gdpr},
	}
	if !h.saveSettings(w, r.Context(), values) {
		return
	}
	h.settingsSaved(w, r, "/settings/data-retention", func() (any, error) { return h.retentionSettings(r.Context()) })
}

// --- Database connection (read-only) ---

// databaseConnectionName names the one connection Escalated has: the *sql.DB
// the host passed as Config.DB. It cannot be switched from a web page.
const databaseConnectionName = "default"

// describeConnection is Laravel's ConnectionInspector description of Config.DB.
// A probe that fails is reported (reachable false, error set), never raised.
func (h *AdminHandler) describeConnection(ctx context.Context) map[string]any {
	desc := map[string]any{
		"name":            databaseConnectionName,
		"driver":          h.DatabaseDialect,
		"database":        nil,
		"is_current":      true,
		"is_host_default": true,
		"reachable":       false,
		"migrated":        false,
		"ticket_count":    nil,
		"error":           nil,
	}
	if h.DB == nil {
		desc["error"] = "No database is configured."
		return desc
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := h.DB.PingContext(ctx); err != nil {
		desc["error"] = err.Error()
		return desc
	}
	desc["reachable"] = true
	desc["database"] = h.databaseLabel(ctx)

	tickets := h.TablePrefix + "tickets"
	if h.TablePrefix == "" {
		tickets = "escalated_tickets"
	}
	var exists bool
	var err error
	if h.DatabaseDialect == "sqlite" {
		var n int
		err = h.DB.QueryRowContext(ctx, sqldialect.Rebind(h.DB, "SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?"), tickets).Scan(&n)
		exists = n > 0
	} else {
		err = h.DB.QueryRowContext(ctx, sqldialect.Rebind(h.DB, "SELECT to_regclass(?) IS NOT NULL"), tickets).Scan(&exists)
	}
	if err != nil {
		desc["error"] = err.Error()
		return desc
	}
	desc["migrated"] = exists
	if exists {
		var n int64
		if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(1) FROM "+tickets).Scan(&n); err != nil {
			desc["error"] = err.Error()
		} else {
			desc["ticket_count"] = n
		}
	}
	return desc
}

// databaseLabel names the database without credentials: the SQLite file, or
// "<database> @ <host>" on PostgreSQL. Nil when it cannot tell.
func (h *AdminHandler) databaseLabel(ctx context.Context) any {
	if h.DatabaseDialect == "sqlite" {
		var file string
		if err := h.DB.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&file); err != nil {
			return nil
		}
		if file == "" {
			return ":memory:"
		}
		return file
	}
	var name, host string
	if err := h.DB.QueryRowContext(ctx, "SELECT current_database(), COALESCE(host(inet_server_addr()), '')").Scan(&name, &host); err != nil {
		return nil
	}
	if host == "" {
		return name
	}
	return name + " @ " + host
}

// DatabaseConnectionPage handles GET /admin/settings/database
// (escalated.admin.settings.database): a read-only view of the connection
// Escalated's tables live on.
func (h *AdminHandler) DatabaseConnectionPage(w http.ResponseWriter, r *http.Request) {
	h.databasePage(w, r, nil)
}

func (h *AdminHandler) databasePage(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	prefix := h.TablePrefix
	if prefix == "" {
		prefix = "escalated_"
	}
	h.renderSettingsPage(w, r, "Escalated/Admin/Settings/DatabaseConnection", map[string]any{
		"connections":    []any{h.describeConnection(r.Context())},
		"current":        databaseConnectionName,
		"hostDefault":    databaseConnectionName,
		"pinnedByConfig": true,
		"tablePrefix":    prefix,
	}, errs)
}

// UpdateDatabaseConnection handles POST /admin/settings/database
// (escalated.admin.settings.database.update). The connection is the host's
// Config.DB, so a switch is always refused.
func (h *AdminHandler) UpdateDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	const msg = "The database connection is set in configuration (Config.DB) and cannot be changed from here."
	if isInertia(r) {
		backTo(w, r, h.adminURL("/settings/database"))
		return
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
}

// TestDatabaseConnection handles POST /admin/settings/database/test
// (escalated.admin.settings.database.test): probe a connection by name. An API
// client gets the description as JSON; a page visit goes back to the page,
// which probes again.
func (h *AdminHandler) TestDatabaseConnection(w http.ResponseWriter, r *http.Request) {
	in, err := readInput(r)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(in.Get("connection"))
	switch {
	case name == "":
		validationErrors(w, r, map[string]string{"connection": requiredMessage("connection")}, func(errs map[string]string) { h.databasePage(w, r, errs) })
		return
	case name != databaseConnectionName:
		validationErrors(w, r, map[string]string{"connection": fmt.Sprintf("[%s] is not a configured connection.", name)}, func(errs map[string]string) { h.databasePage(w, r, errs) })
		return
	}
	if isInertia(r) {
		backTo(w, r, h.adminURL("/settings/database"))
		return
	}
	writeJSON(w, http.StatusOK, h.describeConnection(r.Context()))
}

// --- Email addresses ---

var dkimStatuses = []string{"verified", "pending", "failed", "unknown"}

// emailAddresses reads the email_addresses setting (a JSON array), each entry
// normalised to email, display_name, department_id and dkim_status.
func (h *AdminHandler) emailAddresses(ctx context.Context) ([]map[string]any, error) {
	raw, err := h.store.GetSetting(ctx, "email_addresses")
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	if raw == "" {
		return out, nil
	}
	var stored []map[string]any
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return out, nil
	}
	for _, a := range stored {
		out = append(out, normaliseEmailAddress(a))
	}
	return out, nil
}

func normaliseEmailAddress(a map[string]any) map[string]any {
	email, _ := a["email"].(string)
	var displayName any
	if s, ok := a["display_name"].(string); ok && s != "" {
		displayName = s
	}
	var department any
	switch v := a["department_id"].(type) {
	case float64:
		department = int64(v)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			department = n
		}
	}
	dkim, _ := a["dkim_status"].(string)
	if dkim == "" {
		dkim = "unknown"
	}
	return map[string]any{"email": email, "display_name": displayName, "department_id": department, "dkim_status": dkim}
}

// EmailSettingsPage handles GET /admin/settings/email
// (escalated.admin.settings.email).
func (h *AdminHandler) EmailSettingsPage(w http.ResponseWriter, r *http.Request) {
	h.emailPage(w, r, nil)
}

func (h *AdminHandler) emailSettings(ctx context.Context) (map[string]any, error) {
	addresses, err := h.emailAddresses(ctx)
	if err != nil {
		return nil, err
	}
	reply, err := h.store.GetSetting(ctx, "default_reply_address")
	if err != nil {
		return nil, err
	}
	return map[string]any{"addresses": addresses, "defaultReplyAddress": reply}, nil
}

func (h *AdminHandler) emailPage(w http.ResponseWriter, r *http.Request, errs map[string]string) {
	props, err := h.emailSettings(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderSettingsPage(w, r, "Escalated/Admin/Settings/EmailSettings", props, errs)
}

func validSettingsEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s, "@")
}

// UpdateEmailSettings handles POST /admin/settings/email
// (escalated.admin.settings.email.update). As in Laravel, the addresses are
// stored only: outbound mail and email channels are unaffected.
func (h *AdminHandler) UpdateEmailSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	rerender := func(errs map[string]string) { h.emailPage(w, r, errs) }

	errs := map[string]string{}
	var entries []map[string]any
	rawAddresses, present := in["addresses"]
	if !present || string(rawAddresses) == "null" {
		errs["addresses"] = requiredMessage("addresses")
	} else if err := json.Unmarshal(rawAddresses, &entries); err != nil {
		errs["addresses"] = "The addresses field must be an array."
	}
	addresses := make([]map[string]any, 0, len(entries))
	for i, entry := range entries {
		key := "addresses." + strconv.Itoa(i) + "."
		if entry == nil {
			errs[key+"email"] = requiredMessage("email")
			continue
		}
		email, _ := entry["email"].(string)
		switch {
		case email == "":
			errs[key+"email"] = requiredMessage("email")
		case len(email) > 255:
			errs[key+"email"] = "The email field must not be greater than 255 characters."
		case !validSettingsEmail(email):
			errs[key+"email"] = "The email field must be a valid email address."
		}
		switch v := entry["display_name"].(type) {
		case nil:
		case string:
			if len(v) > 255 {
				errs[key+"display_name"] = "The display name field must not be greater than 255 characters."
			}
		default:
			errs[key+"display_name"] = "The display name field must be a string."
		}
		switch v := entry["department_id"].(type) {
		case nil:
		case float64:
			if v != float64(int64(v)) {
				errs[key+"department_id"] = "The department id field must be an integer."
			}
		case string:
			if _, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); v != "" && err != nil {
				errs[key+"department_id"] = "The department id field must be an integer."
			}
		default:
			errs[key+"department_id"] = "The department id field must be an integer."
		}
		if v, ok := entry["dkim_status"]; ok && v != nil {
			if s, isString := v.(string); !isString || !oneOf(s, dkimStatuses) {
				errs[key+"dkim_status"] = "The selected dkim status is invalid."
			}
		}
		addresses = append(addresses, normaliseEmailAddress(entry))
	}

	var reply *string
	if raw, ok := in["default_reply_address"]; ok && string(raw) != "null" {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			errs["default_reply_address"] = "The default reply address field must be a valid email address."
		} else if s != "" {
			if len(s) > 255 {
				errs["default_reply_address"] = "The default reply address field must not be greater than 255 characters."
			} else if !validSettingsEmail(s) {
				errs["default_reply_address"] = "The default reply address field must be a valid email address."
			}
			reply = &s
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, rerender)
		return
	}

	encoded, err := json.Marshal(addresses)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	values := [][2]string{{"email_addresses", string(encoded)}}
	if reply != nil {
		values = append(values, [2]string{"default_reply_address", *reply})
	}
	if !h.saveSettings(w, r.Context(), values) {
		return
	}
	h.settingsSaved(w, r, "/settings/email", func() (any, error) { return h.emailSettings(r.Context()) })
}

// --- shared ---

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// saveSettings stores each key/value, answering 500 and false on a failure.
func (h *AdminHandler) saveSettings(w http.ResponseWriter, ctx context.Context, values [][2]string) bool {
	for _, kv := range values {
		if err := h.store.SetSetting(ctx, kv[0], kv[1]); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return false
		}
	}
	return true
}

// settingsSaved ends a save: back to the page for a page visit, the stored
// values as JSON otherwise.
func (h *AdminHandler) settingsSaved(w http.ResponseWriter, r *http.Request, page string, current func() (any, error)) {
	if isInertia(r) {
		backTo(w, r, h.adminURL(page))
		return
	}
	body, err := current()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, body)
}
