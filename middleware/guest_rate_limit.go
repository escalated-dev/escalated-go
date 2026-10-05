package middleware

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// GuestScope names a guest rate-limit bucket. Each scope keeps its own
// per-IP counter.
type GuestScope string

const (
	// GuestTicketScope counts guest ticket submissions.
	GuestTicketScope GuestScope = "ticket"
	// GuestReplyScope counts guest replies.
	GuestReplyScope GuestScope = "reply"
)

const (
	guestRateLimitWindow  = time.Minute
	defaultGuestTickets   = 5
	defaultGuestReplies   = 10
	memoryStoreSweepAfter = 1000
)

// GuestRateLimitStore is where guest rate-limit counters live. The default is
// in memory, per process; a multi-instance deployment should supply a shared
// store (Redis, the database, ...) so every instance sees the same counts.
type GuestRateLimitStore interface {
	// Hit counts one request against key in a fixed window and returns the
	// count in the current window (this request included) and the time until
	// the window resets.
	Hit(ctx context.Context, key string, window time.Duration) (count int, resetIn time.Duration, err error)
}

// GuestRateLimitConfig configures the per-client-IP limits on the
// unauthenticated guest endpoints. The zero value is enabled with the
// defaults: 5 ticket submissions and 10 replies per IP per minute, counted in
// memory.
//
// The client IP is the host part of r.RemoteAddr. Behind a load balancer or
// reverse proxy that is the proxy's address, so every guest would share one
// limit: either run a real-IP middleware that trusts only your proxies (for
// example chi's middleware.RealIP behind a proxy you control) ahead of the
// Escalated routes, or set ClientIP.
type GuestRateLimitConfig struct {
	// Disabled turns the limit off. Set it only when the host already
	// throttles these endpoints upstream.
	Disabled bool

	// TicketsPerMinute is the guest ticket submissions allowed per IP per
	// minute. Zero means the default, 5.
	TicketsPerMinute int

	// RepliesPerMinute is the guest replies allowed per IP per minute. Zero
	// means the default, 10.
	RepliesPerMinute int

	// Store holds the counters. Nil means an in-memory store, per process.
	Store GuestRateLimitStore

	// ClientIP extracts the client IP to count against. Nil means the host
	// part of r.RemoteAddr.
	ClientIP func(r *http.Request) string
}

// GuestRateLimiter applies GuestRateLimitConfig to guest routes.
type GuestRateLimiter struct {
	cfg   GuestRateLimitConfig
	store GuestRateLimitStore
}

// NewGuestRateLimiter creates a limiter. Routes wrapped by the same limiter
// share its store.
func NewGuestRateLimiter(cfg GuestRateLimitConfig) *GuestRateLimiter {
	store := cfg.Store
	if store == nil {
		store = NewMemoryGuestRateLimitStore()
	}
	return &GuestRateLimiter{cfg: cfg, store: store}
}

// Middleware limits the wrapped handler per client IP in the given scope.
// Over the limit it answers 429 with Retry-After and does not call the
// handler. Wrap the handler that checks a guest token, rather than the other
// way round, so requests with a wrong token are counted too.
func (l *GuestRateLimiter) Middleware(scope GuestScope) func(http.Handler) http.Handler {
	limit := l.limitFor(scope)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l.cfg.Disabled {
				next.ServeHTTP(w, r)
				return
			}

			key := "escalated:guest:" + string(scope) + ":" + l.clientIP(r)
			count, resetIn, err := l.store.Hit(r.Context(), key, guestRateLimitWindow)
			if err != nil {
				// A store outage must not take the guest endpoints down with it.
				log.Printf("escalated: guest rate limit store: %v", err)
				next.ServeHTTP(w, r)
				return
			}

			if count > limit {
				retryAfter := int((resetIn + time.Second - 1) / time.Second)
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"Too many requests. Please try again later."}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (l *GuestRateLimiter) limitFor(scope GuestScope) int {
	if scope == GuestReplyScope {
		if l.cfg.RepliesPerMinute > 0 {
			return l.cfg.RepliesPerMinute
		}
		return defaultGuestReplies
	}
	if l.cfg.TicketsPerMinute > 0 {
		return l.cfg.TicketsPerMinute
	}
	return defaultGuestTickets
}

func (l *GuestRateLimiter) clientIP(r *http.Request) string {
	if l.cfg.ClientIP != nil {
		return l.cfg.ClientIP(r)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// MemoryGuestRateLimitStore is a fixed-window counter in this process's memory.
type MemoryGuestRateLimitStore struct {
	mu      sync.Mutex
	windows map[string]*guestWindow
	now     func() time.Time
}

type guestWindow struct {
	count   int
	resetAt time.Time
}

// NewMemoryGuestRateLimitStore creates an empty in-memory store.
func NewMemoryGuestRateLimitStore() *MemoryGuestRateLimitStore {
	return &MemoryGuestRateLimitStore{windows: make(map[string]*guestWindow), now: time.Now}
}

// Hit implements GuestRateLimitStore.
func (s *MemoryGuestRateLimitStore) Hit(_ context.Context, key string, window time.Duration) (int, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if len(s.windows) >= memoryStoreSweepAfter {
		// Drop expired windows so one-off IPs do not pile up.
		for k, w := range s.windows {
			if !w.resetAt.After(now) {
				delete(s.windows, k)
			}
		}
	}

	w, ok := s.windows[key]
	if !ok || !w.resetAt.After(now) {
		w = &guestWindow{resetAt: now.Add(window)}
		s.windows[key] = w
	}
	w.count++

	return w.count, w.resetAt.Sub(now), nil
}
