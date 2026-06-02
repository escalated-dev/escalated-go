package escalated

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/models"
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
	UserIDFunc func(r *http.Request) models.UserID

	// DB is the database connection used by the default store implementations.
	DB *sql.DB

	// UserDirectory is the host's bridge to its own users table.
	// Required only for the admin users-management page
	// (GET/PATCH /admin/users); when nil that page renders an empty
	// list and the PATCH endpoint responds 501. See
	// handlers.UserDirectory for the contract.
	UserDirectory handlers.UserDirectory

	// SkillAgentDirectory lists agents for the Skills admin form. Optional;
	// when nil, available_agents is empty. See handlers.SkillAgentDirectory.
	SkillAgentDirectory handlers.SkillAgentDirectory

	// TicketActions registers host-defined custom action buttons for the agent
	// ticket screen. Each visible action is exposed on the ticket responses and,
	// when triggered, records an internal note and invokes OnCustomAction.
	TicketActions []actions.TicketAction

	// OnCustomAction, when set, is invoked after a custom ticket action is
	// triggered (and the audit note recorded). This is where the host runs its
	// own work (CRM sync, etc.).
	OnCustomAction func(ctx context.Context, e actions.CustomActionEvent) error

	// TicketSubjectResolver loads a host model for presentation when serializing
	// ticket subjects. When nil or when lookup fails, subjects render with
	// title=type#id and missing=true.
	TicketSubjectResolver func(subjectType, subjectID string) (models.TicketSubject, bool)

	// TicketSubjectTypes is the allowlist of subject_type values permitted via the
	// agent/API attach endpoints. Leave empty to disable API attaching; programmatic
	// AttachSubject still works when the allowlist is empty.
	TicketSubjectTypes []string
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
		UserIDFunc: func(r *http.Request) models.UserID {
			return ""
		},
	}
}

// TableName returns the full table name with prefix applied.
func (c Config) TableName(name string) string {
	return c.TablePrefix + name
}
