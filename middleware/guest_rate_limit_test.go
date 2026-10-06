package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// The guest endpoints are unauthenticated and every accepted request writes
// rows and sends mail, so they are capped per client IP: 5 ticket submissions
// and 10 replies per minute unless the host configures otherwise.

func statusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})
}

func hitN(h http.Handler, ip string, n int) []int {
	out := make([]int, 0, n)
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodPost, "/guest", nil)
		req.RemoteAddr = ip + ":51234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		out = append(out, rec.Code)
	}
	return out
}

func repeat(status, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = status
	}
	return out
}

func assertStatuses(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", got, want)
		}
	}
}

func TestGuestRateLimit_SixthTicketInAMinuteIs429(t *testing.T) {
	h := NewGuestRateLimiter(GuestRateLimitConfig{}).Middleware(GuestTicketScope)(statusHandler(http.StatusCreated))

	got := hitN(h, "203.0.113.1", 6)

	assertStatuses(t, got, append(repeat(http.StatusCreated, 5), http.StatusTooManyRequests))
}

func TestGuestRateLimit_EleventhReplyInAMinuteIs429(t *testing.T) {
	h := NewGuestRateLimiter(GuestRateLimitConfig{}).Middleware(GuestReplyScope)(statusHandler(http.StatusCreated))

	got := hitN(h, "203.0.113.1", 11)

	assertStatuses(t, got, append(repeat(http.StatusCreated, 10), http.StatusTooManyRequests))
}

func TestGuestRateLimit_WrongTokenRepliesCount(t *testing.T) {
	// The limit wraps the handler that checks the guest token, so requests
	// the token check refuses still use up the allowance.
	h := NewGuestRateLimiter(GuestRateLimitConfig{RepliesPerMinute: 2}).
		Middleware(GuestReplyScope)(statusHandler(http.StatusForbidden))

	got := hitN(h, "203.0.113.1", 3)

	assertStatuses(t, got, []int{http.StatusForbidden, http.StatusForbidden, http.StatusTooManyRequests})
}

func TestGuestRateLimit_HonoursConfiguredLimits(t *testing.T) {
	l := NewGuestRateLimiter(GuestRateLimitConfig{TicketsPerMinute: 2})

	got := hitN(l.Middleware(GuestTicketScope)(statusHandler(http.StatusCreated)), "203.0.113.1", 3)

	assertStatuses(t, got, []int{http.StatusCreated, http.StatusCreated, http.StatusTooManyRequests})

	// The reply limit the host left unset keeps its default of 10.
	replies := hitN(l.Middleware(GuestReplyScope)(statusHandler(http.StatusCreated)), "203.0.113.1", 11)
	assertStatuses(t, replies, append(repeat(http.StatusCreated, 10), http.StatusTooManyRequests))
}

func TestGuestRateLimit_DisabledNeverRefuses(t *testing.T) {
	h := NewGuestRateLimiter(GuestRateLimitConfig{Disabled: true}).Middleware(GuestTicketScope)(statusHandler(http.StatusCreated))

	got := hitN(h, "203.0.113.1", 20)

	assertStatuses(t, got, repeat(http.StatusCreated, 20))
}

func TestGuestRateLimit_KeysEachIPSeparately(t *testing.T) {
	h := NewGuestRateLimiter(GuestRateLimitConfig{}).Middleware(GuestTicketScope)(statusHandler(http.StatusCreated))
	hitN(h, "203.0.113.1", 5)

	assertStatuses(t, hitN(h, "203.0.113.2", 1), []int{http.StatusCreated})
}

func TestGuestRateLimit_TicketsAndRepliesUseSeparateBuckets(t *testing.T) {
	l := NewGuestRateLimiter(GuestRateLimitConfig{})
	hitN(l.Middleware(GuestTicketScope)(statusHandler(http.StatusCreated)), "203.0.113.1", 5)

	got := hitN(l.Middleware(GuestReplyScope)(statusHandler(http.StatusCreated)), "203.0.113.1", 1)

	assertStatuses(t, got, []int{http.StatusCreated})
}

func TestGuestRateLimit_SetsRetryAfter(t *testing.T) {
	h := NewGuestRateLimiter(GuestRateLimitConfig{TicketsPerMinute: 1}).Middleware(GuestTicketScope)(statusHandler(http.StatusCreated))
	hitN(h, "203.0.113.1", 1)

	req := httptest.NewRequest(http.MethodPost, "/guest", nil)
	req.RemoteAddr = "203.0.113.1:51234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retry <= 0 || retry > 60 {
		t.Fatalf("Retry-After = %q, want 1..60", rec.Header().Get("Retry-After"))
	}
}

type recordingStore struct {
	keys    []string
	windows []time.Duration
}

func (s *recordingStore) Hit(_ context.Context, key string, window time.Duration) (int, time.Duration, error) {
	s.keys = append(s.keys, key)
	s.windows = append(s.windows, window)
	return 11, 30 * time.Second, nil
}

func TestGuestRateLimit_CountsInAHostSuppliedStore(t *testing.T) {
	shared := &recordingStore{}
	h := NewGuestRateLimiter(GuestRateLimitConfig{Store: shared}).Middleware(GuestReplyScope)(statusHandler(http.StatusCreated))

	req := httptest.NewRequest(http.MethodPost, "/guest", nil)
	req.RemoteAddr = "203.0.113.9:51234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if len(shared.keys) != 1 || shared.keys[0] != "escalated:guest:reply:203.0.113.9" || shared.windows[0] != time.Minute {
		t.Fatalf("store hits = %v %v", shared.keys, shared.windows)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("status = %d Retry-After = %q, want 429 and 30", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestGuestRateLimit_UsesConfiguredClientIP(t *testing.T) {
	l := NewGuestRateLimiter(GuestRateLimitConfig{
		TicketsPerMinute: 1,
		ClientIP:         func(r *http.Request) string { return r.Header.Get("X-Client") },
	})
	h := l.Middleware(GuestTicketScope)(statusHandler(http.StatusCreated))

	send := func(client string) int {
		req := httptest.NewRequest(http.MethodPost, "/guest", nil)
		req.Header.Set("X-Client", client)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assertStatuses(t, []int{send("a"), send("b"), send("a")}, []int{http.StatusCreated, http.StatusCreated, http.StatusTooManyRequests})
}

func TestMemoryGuestRateLimitStore_ResetsAfterTheWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := NewMemoryGuestRateLimitStore()
	s.now = func() time.Time { return now }
	ctx := context.Background()

	if count, _, _ := s.Hit(ctx, "k", time.Minute); count != 1 {
		t.Fatalf("first count = %d", count)
	}
	now = now.Add(15 * time.Second)
	count, resetIn, _ := s.Hit(ctx, "k", time.Minute)
	if count != 2 || resetIn != 45*time.Second {
		t.Fatalf("count = %d resetIn = %v, want 2 and 45s", count, resetIn)
	}
	now = now.Add(45 * time.Second)
	if count, _, _ := s.Hit(ctx, "k", time.Minute); count != 1 {
		t.Fatalf("count after window = %d, want 1", count)
	}
}
