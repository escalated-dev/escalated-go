package router_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
)

// The webhook pages (Laravel Admin\WebhookController): the list with each
// webhook's latest delivery, the form with the events this package sends, and
// the delivery log; their writes answered the Inertia way.
func TestAdminWebhookPages(t *testing.T) {
	// A database per router: the retry below re-sends in the background, and
	// on a shared database its late write could land on the next router's
	// delivery once SQLite reuses the deleted rows' ids.
	for _, name := range []string{"chi", "stdlib"} {
		t.Run(name, func(t *testing.T) {
			f := newUIFixture(t)
			db := f.esc.Config.DB
			h := f.mounts()[name]
			index := "/escalated/admin/webhooks"
			form := visit(t, h, index+"/create", uiAgent, "admin")
			if form.Component != "Escalated/Admin/Webhooks/Form" {
				t.Fatalf("create component %q", form.Component)
			}
			if events := list(t, form.Props, "availableEvents"); len(events) == 0 {
				t.Error("no availableEvents")
			}

			url := "https://example.test/" + name
			wantRedirect(t, pageJSON(h, http.MethodPost, index, `{"url":"`+url+`","events":["ticket.created"],"secret":"","active":true}`), "store", index)
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"url":"http://10.0.0.1/hook","events":["ticket.created"]}`), "private url", "Escalated/Admin/Webhooks/Form", "url")
			wantPageErrors(t, pageJSON(h, http.MethodPost, index, `{"url":"https://example.test/x","events":[]}`), "no events", "Escalated/Admin/Webhooks/Form", "events")

			var id int64
			if err := db.QueryRow(sqldialect.Rebind(db, `SELECT id FROM escalated_webhooks WHERE url = ?`), url).Scan(&id); err != nil {
				t.Fatalf("stored webhook: %v", err)
			}
			if _, err := db.Exec(sqldialect.Rebind(db, `INSERT INTO escalated_webhook_deliveries (webhook_id, event, payload, response_code, attempts) VALUES (?, ?, ?, ?, ?)`),
				id, "ticket.created", `{"ticket":{"id":1}}`, 500, 1); err != nil {
				t.Fatalf("delivery: %v", err)
			}

			page := visit(t, h, index, uiAgent, "admin")
			if page.Component != "Escalated/Admin/Webhooks/Index" {
				t.Fatalf("index component %q", page.Component)
			}
			var row map[string]any
			for _, item := range list(t, page.Props, "webhooks") {
				if m := item.(map[string]any); m["url"] == url {
					row = m
				}
			}
			if row == nil {
				t.Fatalf("webhooks %v", page.Props["webhooks"])
			}
			hasKeys(t, "webhook", row, "id", "url", "events", "active", "deliveries")
			if first(t, list(t, row, "deliveries"), "deliveries")["response_code"].(float64) != 500 {
				t.Errorf("latest delivery %v", row["deliveries"])
			}

			edit := visit(t, h, index+"/"+itoa(id)+"/edit", uiAgent, "admin")
			hasKeys(t, "edit props", edit.Props, "webhook", "availableEvents")
			hasKeys(t, "webhook", obj(t, edit.Props, "webhook"), "id", "url", "events", "has_secret", "active")

			wantRedirect(t, pageJSON(h, http.MethodPut, index+"/"+itoa(id), `{"url":"`+url+`","events":["ticket.created","ticket.assigned"],"secret":"","active":false}`), "update", index)
			var active bool
			var events string
			_ = db.QueryRow(sqldialect.Rebind(db, `SELECT active, events FROM escalated_webhooks WHERE id = ?`), id).Scan(&active, &events)
			if active || !strings.Contains(events, "ticket.assigned") {
				t.Errorf("after update: active %v events %s", active, events)
			}

			logPage := visit(t, h, index+"/"+itoa(id)+"/deliveries", uiAgent, "admin")
			if logPage.Component != "Escalated/Admin/Webhooks/DeliveryLog" {
				t.Fatalf("log component %q", logPage.Component)
			}
			hasKeys(t, "webhook", obj(t, logPage.Props, "webhook"), "id", "url")
			deliveries := obj(t, logPage.Props, "deliveries")
			paginatorKeys(t, "deliveries", deliveries)
			delivery := first(t, list(t, deliveries, "data"), "deliveries.data")
			hasKeys(t, "delivery", delivery, "id", "event", "payload", "response_code", "attempts", "created_at")

			from := index + "/" + itoa(id) + "/deliveries"
			wantBack(t, postPage(h, http.MethodPost, index+"/deliveries/"+itoa(int64(delivery["id"].(float64)))+"/retry", uiAgent, "admin", from, nil), "retry", from)

			// API clients keep the JSON list.
			rec := sendAPI(h, http.MethodGet, index, uiAgent, "admin", "")
			var out map[string]any
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out["available_events"] == nil {
				t.Errorf("API list: %d %s", rec.Code, rec.Body.String())
			}

			wantRedirect(t, pageJSON(h, http.MethodDelete, index+"/"+itoa(id), ""), "destroy", index)
		})
	}
}
