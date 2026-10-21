package router_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/escalated-dev/escalated-go/middleware"
)

// embed.js forwards data-department="3" as the string "3"; decoding it into
// an int64 answered 400 and the preset department broke ticket and chat
// creation.
func TestWidgetAcceptsADepartmentIDSentAsAString(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	dept := strconv.FormatInt(f.department, 10)
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			rec, out := widgetCall(t, h, http.MethodPost, "/tickets", withProof(t, f.esc, "ticket", map[string]any{
				"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D", "department_id": dept,
			}))
			if rec.Code != http.StatusCreated {
				t.Fatalf("ticket with department_id %q: %d %s", dept, rec.Code, rec.Body.String())
			}
			tk, err := f.esc.Store.GetTicketByReference(context.Background(), out["reference"].(string))
			if err != nil || tk == nil || tk.DepartmentID == nil || *tk.DepartmentID != f.department {
				t.Fatalf("department not stored: %#v %v", tk, err)
			}

			rec, _ = widgetCall(t, h, http.MethodPost, "/chat/start", withProof(t, f.esc, "chat", map[string]any{
				"name": "Grace", "email": "grace@example.com", "department_id": dept,
			}))
			if rec.Code != http.StatusCreated {
				t.Fatalf("chat with department_id %q: %d %s", dept, rec.Code, rec.Body.String())
			}

			// The widget's empty preset is "", which means none.
			rec, _ = widgetCall(t, h, http.MethodPost, "/tickets", withProof(t, f.esc, "ticket", map[string]any{
				"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D", "department_id": "",
			}))
			if rec.Code != http.StatusCreated {
				t.Fatalf("ticket with empty department_id: %d %s", rec.Code, rec.Body.String())
			}

			rec, _ = widgetCall(t, h, http.MethodPost, "/tickets", map[string]any{
				"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D", "department_id": "abc",
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("ticket with department_id \"abc\": got %d, want 400", rec.Code)
			}
		})
	}
}

// The widget limiter keyed on RemoteAddr at a fixed 30/min: behind a proxy
// every visitor shared one bucket, Config.GuestRateLimit (ClientIP, Store,
// Disabled) was ignored, ticket creation allowed 30/min against the guest
// route's 5, and chat messages were not limited at all.
func TestWidgetRoutesUseTheGuestRateLimits(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	f.esc.Config.GuestRateLimit = middleware.GuestRateLimitConfig{
		ClientIP: func(r *http.Request) string { return r.Header.Get("X-Test-Client-IP") },
	}

	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			ticket := func(ip string) int {
				rec, _ := widgetCall(t, h, http.MethodPost, "/tickets", withProof(t, f.esc, "ticket", map[string]any{
					"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D",
				}), "X-Test-Client-IP", ip)
				return rec.Code
			}
			for i := 0; i < 5; i++ {
				if code := ticket("198.51.100.1"); code != http.StatusCreated {
					t.Fatalf("ticket %d: got %d, want 201", i+1, code)
				}
			}
			if code := ticket("198.51.100.1"); code != http.StatusTooManyRequests {
				t.Fatalf("6th ticket in a minute: got %d, want 429", code)
			}
			// Same RemoteAddr, different client: its own budget.
			if code := ticket("198.51.100.2"); code != http.StatusCreated {
				t.Fatalf("another client behind the same proxy: got %d, want 201", code)
			}

			rec, out := widgetCall(t, h, http.MethodPost, "/chat/start", withProof(t, f.esc, "chat", map[string]any{
				"name": "Grace", "email": "grace@example.com",
			}), "X-Test-Client-IP", "198.51.100.3")
			if rec.Code != http.StatusCreated {
				t.Fatalf("chat start: %d %s", rec.Code, rec.Body.String())
			}
			id := out["id"].(string)
			for i := 0; i < 10; i++ {
				rec, _ := widgetCall(t, h, http.MethodPost, "/chat/"+id+"/messages", map[string]any{"body": "hi"}, "X-Test-Client-IP", "198.51.100.3")
				if rec.Code/100 != 2 {
					t.Fatalf("message %d: got %d", i+1, rec.Code)
				}
			}
			rec, _ = widgetCall(t, h, http.MethodPost, "/chat/"+id+"/messages", map[string]any{"body": "hi"}, "X-Test-Client-IP", "198.51.100.3")
			if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
				t.Fatalf("11th chat message in a minute: got %d (Retry-After %q), want 429", rec.Code, rec.Header().Get("Retry-After"))
			}
		})
	}
}

func TestWidgetGeneralRequestBudgetCoversEveryRoute(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	f.esc.Config.Widget.RateLimitPerMin = 3
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 3; i++ {
				if rec, _ := widgetCall(t, h, http.MethodGet, "/config", nil); rec.Code != http.StatusOK {
					t.Fatalf("config %d: got %d", i+1, rec.Code)
				}
			}
			if rec, _ := widgetCall(t, h, http.MethodPost, "/chat/unknown/typing", nil); rec.Code != http.StatusTooManyRequests {
				t.Fatalf("4th widget request: got %d, want 429", rec.Code)
			}
		})
	}
}

func TestWidgetRateLimitsCanBeDisabled(t *testing.T) {
	f := newWidgetContractFixture(t, true)
	f.esc.Config.GuestRateLimit = middleware.GuestRateLimitConfig{Disabled: true}
	for name, h := range f.mounts() {
		t.Run(name, func(t *testing.T) {
			for i := 0; i < 8; i++ {
				rec, _ := widgetCall(t, h, http.MethodPost, "/tickets", withProof(t, f.esc, "ticket", map[string]any{
					"name": "Ada", "email": "ada@example.com", "subject": "S", "description": "D",
				}))
				if rec.Code != http.StatusCreated {
					t.Fatalf("ticket %d with limits disabled: got %d", i+1, rec.Code)
				}
			}
		})
	}
}
