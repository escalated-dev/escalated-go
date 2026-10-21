package router_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/router"
)

// The guest ticket endpoint is unauthenticated and every accepted request
// writes rows, so the mounted route itself caps it per client IP (5 a minute
// by default) rather than relying on each host to put a throttle in front.

func guestLimitMounts(t *testing.T, limit middleware.GuestRateLimitConfig) map[string]guestMount {
	t.Helper()
	cfg := escalated.DefaultConfig()
	cfg.DB = testdb.Open(t)
	cfg.UIEnabled = false
	cfg.GuestRateLimit = limit
	cfg.Mailer = newCodeMailer()

	esc, err := escalated.New(cfg)
	if err != nil {
		t.Fatalf("new escalated: %v", err)
	}

	r := chi.NewRouter()
	router.MountChi(r, esc)
	mux := http.NewServeMux()
	router.MountStdlib(mux, esc)
	return map[string]guestMount{"chi": {r, esc}, "stdlib": {mux, esc}}
}

type guestMount struct {
	h   http.Handler
	esc *escalated.Escalated
}

func postGuestTickets(t *testing.T, m guestMount, n int) []int {
	t.Helper()
	h := m.h
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		// A distinct email per call, so only the per-IP limit can be what trips.
		b, _ := json.Marshal(withProof(t, m.esc, "ticket", map[string]any{
			"guest_email": fmt.Sprintf("guest%d@example.com", i), "subject": "Help", "description": "d",
		}))
		body := string(b)
		req := httptest.NewRequest(http.MethodPost, "/escalated/api/guest/tickets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:40000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		out = append(out, rec.Code)
	}
	return out
}

func TestGuestTicketRoute_SixthTicketFromOneIPIs429(t *testing.T) {
	for name, h := range guestLimitMounts(t, middleware.GuestRateLimitConfig{}) {
		t.Run(name, func(t *testing.T) {
			got := postGuestTickets(t, h, 6)
			want := []int{201, 201, 201, 201, 201, 429}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("statuses = %v, want %v", got, want)
			}
		})
	}
}

func TestGuestTicketRoute_TakesItsLimitFromTheConfig(t *testing.T) {
	for name, h := range guestLimitMounts(t, middleware.GuestRateLimitConfig{TicketsPerMinute: 2}) {
		t.Run(name, func(t *testing.T) {
			got := postGuestTickets(t, h, 3)
			if fmt.Sprint(got) != fmt.Sprint([]int{201, 201, 429}) {
				t.Fatalf("statuses = %v, want [201 201 429]", got)
			}
		})
	}
}

func TestGuestTicketRoute_CanBeSwitchedOff(t *testing.T) {
	for name, h := range guestLimitMounts(t, middleware.GuestRateLimitConfig{Disabled: true}) {
		t.Run(name, func(t *testing.T) {
			for _, status := range postGuestTickets(t, h, 8) {
				if status != http.StatusCreated {
					t.Fatalf("status = %d with the limit disabled, want 201", status)
				}
			}
		})
	}
}
