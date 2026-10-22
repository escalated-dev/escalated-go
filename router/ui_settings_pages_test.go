package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// The admin Settings sub-pages, on escalated-laravel's paths, props and
// validation: CsatSettings, DataRetention, DatabaseConnection, EmailSettings
// and PublicTickets. Sandbox stays unrendered, as in Laravel.

func (f *uiFixture) setting(t *testing.T, key string) string {
	t.Helper()
	v, err := f.esc.Store.GetSetting(context.Background(), key)
	if err != nil {
		t.Fatalf("setting %s: %v", key, err)
	}
	return v
}

func TestAdminCsatSettingsPage(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			path := "/escalated/admin/settings/csat"

			page := visit(t, h, path, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Settings/CsatSettings" {
				t.Fatalf("component %q", page.Component)
			}
			settings := obj(t, page.Props, "settings")
			hasKeys(t, "settings", settings, "csat_question_text", "csat_scale", "csat_delivery_trigger", "csat_delay_hours")
			if settings["csat_question_text"] != "How would you rate your support experience?" || settings["csat_scale"] != "1-5" ||
				settings["csat_delivery_trigger"] != "on_resolve" || settings["csat_delay_hours"] != float64(0) {
				t.Errorf("defaults %v", settings)
			}

			wantRedirect(t, pageJSON(h, http.MethodPost, path, `{"csat_question_text":"How did we do?","csat_scale":"1-3","csat_delivery_trigger":"delayed","csat_delay_hours":24}`), "update", path)
			if f.setting(t, "csat_question_text") != "How did we do?" || f.setting(t, "csat_scale") != "1-3" ||
				f.setting(t, "csat_delivery_trigger") != "delayed" || f.setting(t, "csat_delay_hours") != "24" {
				t.Error("CSAT settings not stored")
			}
			if again := obj(t, visit(t, h, path, uiAgent, "admin").Props, "settings"); again["csat_delay_hours"] != float64(24) {
				t.Errorf("stored delay hours read back as %v", again["csat_delay_hours"])
			}

			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"csat_question_text":"x","csat_scale":"1-10","csat_delivery_trigger":"on_resolve","csat_delay_hours":0}`), "bad scale", "Escalated/Admin/Settings/CsatSettings", "csat_scale")
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"csat_question_text":"x","csat_scale":"1-5","csat_delivery_trigger":"on_resolve","csat_delay_hours":169}`), "delay over a week", "Escalated/Admin/Settings/CsatSettings", "csat_delay_hours")
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"csat_question_text":"","csat_scale":"1-5","csat_delivery_trigger":"on_resolve","csat_delay_hours":0}`), "no question", "Escalated/Admin/Settings/CsatSettings", "csat_question_text")
			if f.setting(t, "csat_scale") != "1-3" {
				t.Error("a refused save changed the settings")
			}
		})
	}
}

func TestAdminDataRetentionPage(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			db := f.esc.Config.DB
			path := "/escalated/admin/settings/data-retention"

			page := visit(t, h, path, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Settings/DataRetention" {
				t.Fatalf("component %q", page.Component)
			}
			settings := obj(t, page.Props, "settings")
			hasKeys(t, "settings", settings, "retention_closed_tickets", "retention_attachments", "retention_audit_logs", "retention_user_data_gdpr")
			if settings["retention_closed_tickets"] != "never" || settings["retention_attachments"] != "never" ||
				settings["retention_audit_logs"] != "never" || settings["retention_user_data_gdpr"] != false {
				t.Errorf("defaults %v", settings)
			}
			preview := obj(t, page.Props, "purgePreview")
			hasKeys(t, "purgePreview", preview, "tickets", "attachments", "audit_logs")
			if preview["tickets"] != float64(0) || preview["attachments"] != float64(0) || preview["audit_logs"] != float64(0) {
				t.Errorf("never should preview nothing: %v", preview)
			}

			wantRedirect(t, pageJSON(h, http.MethodPost, path, `{"retention_closed_tickets":"1_year","retention_attachments":"2_years","retention_audit_logs":"90_days","retention_user_data_gdpr":true}`), "update", path)
			if f.setting(t, "retention_closed_tickets") != "1_year" || f.setting(t, "retention_attachments") != "2_years" ||
				f.setting(t, "retention_audit_logs") != "90_days" || f.setting(t, "retention_user_data_gdpr") != "1" {
				t.Error("retention settings not stored")
			}

			// The fixture's ticket, closed two years ago, is now a candidate.
			old := time.Now().AddDate(-2, 0, 0)
			if _, err := db.Exec(sqldialect.Rebind(db, `UPDATE escalated_tickets SET status = ?, closed_at = ? WHERE id = ?`), models.StatusClosed, old, f.ticket.ID); err != nil {
				t.Fatal(err)
			}
			again := visit(t, h, path, uiAgent, "admin")
			if got := obj(t, again.Props, "settings"); got["retention_user_data_gdpr"] != true {
				t.Errorf("gdpr read back as %v", got["retention_user_data_gdpr"])
			}
			if got := obj(t, again.Props, "purgePreview"); got["tickets"] != float64(1) {
				t.Errorf("preview %v, want one closed ticket", got)
			}

			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"retention_closed_tickets":"90_days","retention_attachments":"never","retention_audit_logs":"never","retention_user_data_gdpr":false}`), "tickets in days", "Escalated/Admin/Settings/DataRetention", "retention_closed_tickets")
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"retention_closed_tickets":"never","retention_attachments":"never","retention_audit_logs":"never"}`), "no gdpr flag", "Escalated/Admin/Settings/DataRetention", "retention_user_data_gdpr")
			if f.setting(t, "retention_closed_tickets") != "1_year" {
				t.Error("a refused save changed the settings")
			}
		})
	}
}

func TestAdminDatabaseConnectionPage(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			path := "/escalated/admin/settings/database"

			page := visit(t, h, path, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Settings/DatabaseConnection" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "connections", "current", "hostDefault", "pinnedByConfig", "tablePrefix")
			if page.Props["current"] != "default" || page.Props["hostDefault"] != "default" || page.Props["pinnedByConfig"] != true || page.Props["tablePrefix"] != "escalated_" {
				t.Errorf("props %v", page.Props)
			}
			conns := list(t, page.Props, "connections")
			if len(conns) != 1 {
				t.Fatalf("connections %v", conns)
			}
			conn := conns[0].(map[string]any)
			hasKeys(t, "connection", conn, "name", "driver", "database", "is_current", "is_host_default", "reachable", "migrated", "ticket_count", "error")
			if conn["name"] != "default" || conn["is_current"] != true || conn["is_host_default"] != true || conn["reachable"] != true ||
				conn["migrated"] != true || conn["ticket_count"] != float64(2) || conn["error"] != nil || conn["driver"] == "" {
				t.Errorf("connection %v", conn)
			}

			// The connection is the host's *sql.DB: a switch is refused.
			wantRedirect(t, pageJSON(h, http.MethodPost, path, `{"connection":"default"}`), "update", path)

			rec := sendAPI(h, http.MethodPost, path+"/test", uiAgent, "admin", `{"connection":"default"}`)
			var described map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &described) != nil || described["reachable"] != true || described["name"] != "default" {
				t.Errorf("test: %d %s", rec.Code, rec.Body.String())
			}
			if rec := sendAPI(h, http.MethodPost, path+"/test", uiAgent, "admin", `{"connection":"reporting"}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("unknown connection: %d %s", rec.Code, rec.Body.String())
			}
			wantPageErrors(t, pageJSON(h, http.MethodPost, path+"/test", `{"connection":""}`), "no connection", "Escalated/Admin/Settings/DatabaseConnection", "connection")
			wantRedirect(t, pageJSON(h, http.MethodPost, path+"/test", `{"connection":"default"}`), "page test", path)
		})
	}
}

func TestAdminEmailSettingsPage(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			path := "/escalated/admin/settings/email"

			page := visit(t, h, path, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Settings/EmailSettings" {
				t.Fatalf("component %q", page.Component)
			}
			hasKeys(t, "props", page.Props, "addresses", "defaultReplyAddress")
			if len(list(t, page.Props, "addresses")) != 0 || page.Props["defaultReplyAddress"] != "" {
				t.Errorf("defaults %v", page.Props)
			}

			wantRedirect(t, pageJSON(h, http.MethodPost, path,
				`{"addresses":[{"email":"support@example.com","display_name":"Support","department_id":3,"dkim_status":"verified","extra":"x"},{"email":"billing@example.com"}],"default_reply_address":"support@example.com"}`),
				"update", path)
			if f.setting(t, "default_reply_address") != "support@example.com" {
				t.Error("reply address not stored")
			}
			again := visit(t, h, path, uiAgent, "admin")
			addresses := list(t, again.Props, "addresses")
			if len(addresses) != 2 || again.Props["defaultReplyAddress"] != "support@example.com" {
				t.Fatalf("read back %v", again.Props)
			}
			first := addresses[0].(map[string]any)
			hasKeys(t, "address", first, "email", "display_name", "department_id", "dkim_status")
			if len(first) != 4 || first["department_id"] != float64(3) || first["dkim_status"] != "verified" {
				t.Errorf("address %v", first)
			}
			if second := addresses[1].(map[string]any); second["dkim_status"] != "unknown" || second["display_name"] != nil || second["department_id"] != nil {
				t.Errorf("defaults of a bare address %v", second)
			}

			// Leaving the reply address out keeps it.
			wantRedirect(t, pageJSON(h, http.MethodPost, path, `{"addresses":[{"email":"help@example.com"}]}`), "update without reply", path)
			if f.setting(t, "default_reply_address") != "support@example.com" {
				t.Error("an omitted reply address was cleared")
			}

			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"addresses":[{"email":"not-an-email"}]}`), "bad address", "Escalated/Admin/Settings/EmailSettings", "addresses.0.email")
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"addresses":[{"email":"a@example.com","dkim_status":"great"}]}`), "bad dkim", "Escalated/Admin/Settings/EmailSettings", "addresses.0.dkim_status")
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"addresses":[{"email":"a@example.com"}],"default_reply_address":"nope"}`), "bad reply", "Escalated/Admin/Settings/EmailSettings", "default_reply_address")
			if rec := sendAPI(h, http.MethodPost, path, uiAgent, "admin", `{"addresses":"x"}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("API validation: %d", rec.Code)
			}
		})
	}
}

func TestAdminPublicTicketsPageAcceptsPostAndValidates(t *testing.T) {
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			h := f.mounts()[name]
			path := "/escalated/admin/settings/public-tickets"

			// The page posts.
			wantRedirect(t, pageJSON(h, http.MethodPost, path, `{"guest_policy_mode":"guest_user","guest_policy_user_id":"42","guest_policy_signup_url_template":"ignored"}`), "post", path)
			if f.setting(t, "guest_policy_mode") != "guest_user" || f.setting(t, "guest_policy_user_id") != "42" || f.setting(t, "guest_policy_signup_url_template") != "" {
				t.Error("guest policy not stored")
			}
			settings := obj(t, visit(t, h, path, uiAgent, "admin").Props, "settings")
			if settings["guest_policy_user_id"] != "42" {
				t.Errorf("user id %v (%T), want the string", settings["guest_policy_user_id"], settings["guest_policy_user_id"])
			}

			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"guest_policy_mode":"everyone"}`), "unknown mode", "Escalated/Admin/Settings/PublicTickets", "guest_policy_mode")
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"guest_policy_mode":"guest_user","guest_policy_user_id":"0"}`), "user 0", "Escalated/Admin/Settings/PublicTickets", "guest_policy_user_id")
			long := make([]byte, 501)
			for i := range long {
				long[i] = 'a'
			}
			wantPageErrors(t, pageJSON(h, http.MethodPost, path, `{"guest_policy_mode":"prompt_signup","guest_policy_signup_url_template":"`+string(long)+`"}`), "long template", "Escalated/Admin/Settings/PublicTickets", "guest_policy_signup_url_template")
			if f.setting(t, "guest_policy_mode") != "guest_user" {
				t.Error("a refused save changed the policy")
			}

			wantRedirect(t, pageJSON(h, http.MethodPost, path, `{"guest_policy_mode":"unassigned","guest_policy_user_id":""}`), "back to unassigned", path)
			if settings := obj(t, visit(t, h, path, uiAgent, "admin").Props, "settings"); settings["guest_policy_user_id"] != nil {
				t.Errorf("user id %v, want null", settings["guest_policy_user_id"])
			}
		})
	}
}

func TestAdminSettingsSubPagesAreAdminOnly(t *testing.T) {
	f := newUIFixture(t)
	for name, h := range f.mounts() {
		for _, path := range []string{"csat", "data-retention", "database", "email", "public-tickets"} {
			full := "/escalated/admin/settings/" + path
			if rec := send(h, http.MethodGet, full, uiAgent, "agent", ""); !denied(rec.Code) {
				t.Errorf("%s GET %s as agent: %d", name, full, rec.Code)
			}
			if rec := send(h, http.MethodPost, full, uiAgent, "agent", `{}`); !denied(rec.Code) {
				t.Errorf("%s POST %s as agent: %d", name, full, rec.Code)
			}
		}
		if rec := send(h, http.MethodPost, "/escalated/admin/settings/database/test", uiAgent, "agent", `{"connection":"default"}`); !denied(rec.Code) {
			t.Errorf("%s database test as agent: %d", name, rec.Code)
		}
	}
}

// The periods saved on the page are the ones Escalated.PurgeExpired applies.
func TestPurgeExpiredAppliesTheDataRetentionPage(t *testing.T) {
	f := newUIFixture(t)
	h := f.mounts()["chi"]
	db := f.esc.Config.DB
	ctx := context.Background()
	old := time.Now().AddDate(-2, 0, 0)
	if _, err := db.Exec(sqldialect.Rebind(db, `UPDATE escalated_tickets SET status = ?, closed_at = ? WHERE id = ?`), models.StatusClosed, old, f.ticket.ID); err != nil {
		t.Fatal(err)
	}

	// Defaults: never, so nothing goes.
	if rep, err := f.esc.PurgeExpired(ctx, false); err != nil || rep.ClosedTicketsDeleted != 0 {
		t.Fatalf("defaults purged: %+v %v", rep, err)
	}

	wantRedirect(t, pageJSON(h, http.MethodPost, "/escalated/admin/settings/data-retention", `{"retention_closed_tickets":"1_year","retention_attachments":"never","retention_audit_logs":"never","retention_user_data_gdpr":false}`), "update", "/escalated/admin/settings/data-retention")
	if rep, err := f.esc.PurgeExpired(ctx, true); err != nil || rep.ClosedTicketsDeleted != 1 {
		t.Fatalf("dry run: %+v %v", rep, err)
	}
	if got, _ := f.esc.Store.GetTicket(ctx, f.ticket.ID); got == nil {
		t.Fatal("a dry run deleted the ticket")
	}
	if rep, err := f.esc.PurgeExpired(ctx, false); err != nil || rep.ClosedTicketsDeleted != 1 {
		t.Fatalf("purge: %+v %v", rep, err)
	}
	if got, _ := f.esc.Store.GetTicket(ctx, f.ticket.ID); got != nil {
		t.Error("the expired ticket is still there")
	}
	if got, _ := f.esc.Store.GetTicket(ctx, f.other.ID); got == nil {
		t.Error("the open ticket was purged")
	}
}
