package escalated

import (
	"database/sql"
	"net/http"
)

// Config holds the configuration for the Escalated support ticket system.
type Config struct {
	// RoutePrefix is the URL prefix for all Escalated routes (e.g., "/support").
	// Defaults to "/escalated".
	RoutePrefix string

	// UIEnabled controls whether Inertia-powered UI routes are mounted.
	// When false, only JSON API handlers are registered.
	UIEnabled bool

	// TablePrefix is the prefix for all database tables (e.g., "escalated_").
	// Defaults to "escalated_".
	TablePrefix string

	// AdminCheck returns true if the current request is from an admin user.
	// This is called by middleware to gate access to admin routes.
	AdminCheck func(r *http.Request) bool

	// AgentCheck returns true if the current request is from an agent user.
	// This is called by middleware to gate access to agent routes.
	AgentCheck func(r *http.Request) bool

	// UserIDFunc extracts the current user's ID from a request.
	// Used for assigning tickets, tracking activity causers, etc.
	UserIDFunc func(r *http.Request) int64

	// DB is the database connection used by the default store implementations.
	DB *sql.DB
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		RoutePrefix: "/escalated",
		UIEnabled:   true,
		TablePrefix: "escalated_",
		AdminCheck: func(r *http.Request) bool {
			return false
		},
		AgentCheck: func(r *http.Request) bool {
			return false
		},
		UserIDFunc: func(r *http.Request) int64 {
			return 0
		},
	}
}

// TableName returns the full table name with prefix applied.
func (c Config) TableName(name string) string {
	return c.TablePrefix + name
}
