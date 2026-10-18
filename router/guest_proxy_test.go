package router_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/internal/testdb"
	"github.com/escalated-dev/escalated-go/router"
)

// Exercise the host's proxy middleware and Escalated's mounted guest endpoint
// together: forwarded headers may select a rate-limit bucket only behind a
// configured trusted proxy. A direct client must not get a new bucket by
// changing X-Forwarded-For.
func TestGuestTicketRoute_TrustedProxyRateLimits(t *testing.T) {
	type proxyRequest struct {
		peer, forwarded string
		want            int
	}
	for _, tc := range []struct {
		name     string
		requests []proxyRequest
	}{
		{
			name: "direct clients cannot rotate forwarded addresses",
			requests: []proxyRequest{
				{"203.0.113.7:40000", "198.51.100.1", http.StatusCreated},
				{"203.0.113.7:40001", "198.51.100.2", http.StatusTooManyRequests},
				{"203.0.113.8:40000", "198.51.100.1", http.StatusCreated},
			},
		},
		{
			name: "trusted proxy keeps clients separate",
			requests: []proxyRequest{
				{"10.0.0.2:40000", "198.51.100.1", http.StatusCreated},
				{"10.0.0.2:40001", "192.0.2.99, 198.51.100.1", http.StatusTooManyRequests},
				{"10.0.0.2:40002", "198.51.100.2", http.StatusCreated},
			},
		},
		{
			name: "trusted proxy chain stops at the first untrusted hop",
			requests: []proxyRequest{
				{"10.0.0.2:40000", "198.51.100.1, 10.0.0.3", http.StatusCreated},
				{"10.0.0.2:40001", "192.0.2.99, 198.51.100.1, 10.0.0.3", http.StatusTooManyRequests},
				{"10.0.0.2:40002", "198.51.100.2, 10.0.0.3", http.StatusCreated},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := escalated.DefaultConfig()
			cfg.DB = testdb.Open(t)
			cfg.UIEnabled = false
			cfg.GuestRateLimit.TicketsPerMinute = 1
			cfg.GuestRateLimit.ClientIP = func(r *http.Request) string {
				return chimiddleware.GetClientIP(r.Context())
			}

			esc, err := escalated.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			r := chi.NewRouter()
			r.Use(chimiddleware.ClientIPFromXFF("10.0.0.0/8"))
			router.MountChi(r, esc)

			for i, request := range tc.requests {
				body := fmt.Sprintf(`{"guest_email":"guest%d@example.test","subject":"Help","description":"d"}`, i)
				req := httptest.NewRequest(http.MethodPost, "/escalated/api/guest/tickets", strings.NewReader(body))
				req.RemoteAddr = request.peer
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", request.forwarded)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				if rec.Code != request.want {
					t.Errorf("request %d: got %d, want %d: %s", i+1, rec.Code, request.want, rec.Body.String())
				}
			}

			var tickets int
			if err := cfg.DB.QueryRow("SELECT COUNT(*) FROM escalated_tickets").Scan(&tickets); err != nil {
				t.Fatal(err)
			}
			if tickets != 2 {
				t.Errorf("created %d tickets, want 2; the rejected request must not write a ticket", tickets)
			}
		})
	}
}
