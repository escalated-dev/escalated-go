package escalated

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// NewsletterPermissionCheck lets hosts with granular permissions enforce
// newsletter slugs. Return true for admins that should have all newsletter
// access. When nil, the package relies on AdminCheck, matching the rest of
// this backend.
type NewsletterPermissionCheck func(r *http.Request, permission string) bool

// NewsletterMail is the rendered outbound newsletter message handed to the
// host application's mail adapter.
type NewsletterMail struct {
	To       string
	From     string
	ReplyTo  string
	Subject  string
	HTML     string
	Headers  map[string]string
	TestSend bool
}

// NewsletterMailer sends rendered newsletter messages. It is optional; sending
// endpoints and the dispatcher fail deliveries when no mailer is configured.
type NewsletterMailer interface {
	SendNewsletter(ctx context.Context, msg NewsletterMail) error
}

// NewsletterConfig controls the optional newsletter subsystem.
type NewsletterConfig struct {
	BaseURL              string
	DefaultFrom          string
	DefaultReplyTo       string
	DefaultTheme         string
	TrackingEnabled      bool
	ThemesDir            string
	BatchSize            int
	RateLimitPerMinute   int
	ClaimTimeoutMinutes  int
	AutoPauseBounceRate  float64
	AutoPauseThreshold   int
	BrandName            string
	BrandAccent          string
	BrandLogoURL         string
	BrandPhysicalAddress string
}

// NotificationConfig configures the ticket notification emails.
type NotificationConfig struct {
	// Disabled turns the notification emails off while keeping Mailer for
	// guest verification codes.
	Disabled bool

	// Domain is used in Message-IDs (<ticket-{id}@Domain>) and the signed
	// Reply-To. Defaults to the host of BaseURL, else "escalated.dev".
	Domain string

	// InboundSecret, when set, gives every email a signed Reply-To
	// (reply+{id}.{hmac}@Domain) that the inbound email routes verify.
	InboundSecret string

	// BaseURL is the public origin of the app (https://support.example.com).
	// When set, emails link agents to {BaseURL}{RoutePrefix}/agent/tickets/{id}
	// and signed-in requesters to {BaseURL}{RoutePrefix}/tickets/{id}.
	BaseURL string

	// Branding styles the HTML emails. Zero values use
	// services.DefaultEmailBranding.
	Branding services.EmailBranding

	// Sync sends inline instead of in the background (tests).
	Sync bool
}

// Config holds the configuration for the Escalated support ticket system.
type Config struct {
	Visibility               string
	Accounts                 ticketaccess.AccountConfig
	TargetedSLAs             bool
	TicketAccessUserResolver func(context.Context, models.UserID) (*ticketaccess.User, error)

	// RoutePrefix is the URL prefix for all Escalated routes (e.g., "/support").
	// Defaults to "/escalated".
	RoutePrefix string

	// UIEnabled controls whether Inertia-powered UI routes are mounted.
	// When false, only JSON API handlers are registered.
	UIEnabled bool

	// EnableNewsletters controls route registration and dispatch. Defaults off.
	EnableNewsletters bool

	// EnableWidget mounts the embeddable support widget's public routes under
	// {RoutePrefix}/widget: embed.js, plus config, articles, tickets and chat
	// behind handlers.WidgetCORS. Defaults off, so hosts that wire the widget
	// handlers themselves keep doing so. Widget.Enabled still switches the
	// endpoints on and off once they are mounted. See
	// docs/embedding-the-widget.md.
	EnableWidget bool

	// Widget configures the mounted widget routes (EnableWidget).
	Widget handlers.WidgetConfig

	// Newsletters contains runtime defaults for the optional newsletter system.
	Newsletters NewsletterConfig

	// NewsletterMailer sends newsletter email. Required for dispatch/test-send.
	NewsletterMailer NewsletterMailer

	// NewsletterPermissionCheck optionally enforces newsletters.manage and
	// newsletters.send beyond AdminCheck.
	NewsletterPermissionCheck NewsletterPermissionCheck

	// TablePrefix is the prefix for all database tables (e.g., "escalated_").
	// Defaults to "escalated_".
	TablePrefix string

	// DatabaseDialect names the SQL dialect Escalated writes. Leave it empty and
	// New detects it from the database DB is connected to; set it to skip
	// detection. Supported values: "postgres", "sqlite".
	DatabaseDialect string

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

	// APIAuth holds host-app authentication callbacks for the general JSON API
	// (/api/auth/*). Escalated owns no credentials or sessions; configure only
	// the callbacks the app needs. Unconfigured endpoints respond 501. See
	// handlers.APIAuth.
	APIAuth handlers.APIAuth

	// GuestRateLimit caps the unauthenticated guest endpoints per client IP:
	// ticket submissions (POST /api/guest/tickets) at 5 a minute and guest
	// replies at 10 a minute by default, answering 429 with Retry-After. The
	// widget routes use the same limits plus a general request budget
	// (RequestsPerMinute, default 60; WidgetConfig.RateLimitPerMin overrides
	// it for the widget). The
	// zero value is enabled with those defaults and an in-memory store; set
	// Store for a counter shared between instances. Behind a proxy, every guest
	// shares the proxy's address unless a trusted real-IP middleware runs first
	// or ClientIP is set. See middleware.GuestRateLimitConfig.
	GuestRateLimit middleware.GuestRateLimitConfig

	// Mailer delivers Escalated's outbound email: guest verification codes
	// and ticket notifications. Escalated renders each message and hands it
	// over; the host owns the transport. Nil logs the recipient and subject
	// of each message and sends nothing (services.LogMailSender), so guest
	// verification cannot complete until a Mailer is set.
	Mailer services.MailSender

	// MailFrom is the From address on Escalated's email. Empty leaves it to
	// the Mailer.
	MailFrom string

	// ChatAgentOnline reports whether an agent is online and taking chats.
	// The widget's chat availability uses it; nil counts every agent in the
	// chat department as online.
	ChatAgentOnline func(ctx context.Context, agentID models.UserID) (bool, error)

	// ChatAgentName returns an agent's display name for the widget's chat
	// transcript. Nil (or "") shows "Support".
	ChatAgentName func(ctx context.Context, agentID models.UserID) string

	// UserEmail returns a host user's email address. Notification emails to
	// agents, followers and signed-in requesters use it; nil means those are
	// not emailed (guest requesters still are). Escalated owns no users table.
	UserEmail func(ctx context.Context, id models.UserID) (string, error)

	// UserName returns a host user's display name for the shared pages:
	// requesters, assignees, reply authors and activity causers. Nil (or "")
	// shows staff as "Support" and a signed-in requester as "Unknown".
	// Guests are named from their ticket. Escalated owns no users table.
	UserName func(ctx context.Context, id models.UserID) string

	// Notifications configures the ticket notification emails, sent through
	// Mailer. Nothing is sent while Mailer is nil. See
	// docs/notification-emails.md.
	Notifications NotificationConfig

	// GuestAccess configures verified guest access: email codes, expiring
	// grants and the legacy-token transition. Set Secret in production. See
	// docs/guest-access.md.
	GuestAccess services.GuestAccessConfig
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Visibility:  "all",
		RoutePrefix: "/escalated",
		UIEnabled:   true,
		TablePrefix: "escalated_",
		Widget:      handlers.DefaultWidgetConfig(),
		// Left empty on purpose: New detects it from Config.DB. A default of
		// "postgres" here is what let a SQLite host reach the PostgreSQL store
		// with no error at startup.
		Newsletters: NewsletterConfig{
			DefaultTheme:        "default",
			TrackingEnabled:     true,
			BatchSize:           50,
			RateLimitPerMinute:  60,
			ClaimTimeoutMinutes: 10,
			AutoPauseBounceRate: 0.05,
			AutoPauseThreshold:  100,
			BrandAccent:         "#2563eb",
		},
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
