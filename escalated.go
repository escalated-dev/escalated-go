// Package escalated provides an embeddable support ticket system for Go applications.
//
// It works with standard net/http and popular routers (chi, gin, echo) by exposing
// handlers as plain http.HandlerFunc values. Database access is abstracted behind
// the Store interface with ready-made PostgreSQL and SQLite implementations.
//
// Quick start with Chi:
//
//	cfg := escalated.DefaultConfig()
//	cfg.DB = db
//	esc, _ := escalated.New(cfg)
//	r := chi.NewRouter()
//	router.MountChi(r, esc)
//	http.ListenAndServe(":8080", r)
//
// Quick start with stdlib:
//
//	cfg := escalated.DefaultConfig()
//	cfg.DB = db
//	esc, _ := escalated.New(cfg)
//	mux := http.NewServeMux()
//	router.MountStdlib(mux, esc)
//	http.ListenAndServe(":8080", mux)
package escalated

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/services/newsletter"
	"github.com/escalated-dev/escalated-go/store"
)

// Version is the current version of the Escalated Go package.
const Version = "0.1.0"

// Escalated is the central container that holds the store, renderer, and config.
// Create one with New() and pass it to router.MountChi or router.MountStdlib.
type Escalated struct {
	Access   *ticketaccess.Access
	Config   Config
	Store    store.Store
	Renderer renderer.Renderer
	// GuestAccess issues and checks guest email codes and grants for the
	// guest API and the widget (docs/guest-access.md). Host code can call
	// GuestAccess.Revoke after changing a ticket's guest email.
	GuestAccess *services.GuestAccess
	// Notifier sends the ticket notification emails; nil while Config.Mailer
	// is nil or Config.Notifications.Disabled is set.
	Notifier *services.Notifier
}

type newsletterMailerBridge struct {
	mailer NewsletterMailer
}

func (b newsletterMailerBridge) SendNewsletter(ctx context.Context, msg newsletter.MailMessage) error {
	return b.mailer.SendNewsletter(ctx, NewsletterMail{
		To:       msg.To,
		From:     msg.From,
		ReplyTo:  msg.ReplyTo,
		Subject:  msg.Subject,
		HTML:     msg.HTML,
		Headers:  msg.Headers,
		TestSend: msg.TestSend,
	})
}

// New creates a new Escalated instance from the given config.
//
// The store is chosen from the database Config.DB is actually connected to, so
// a host that opened a SQLite connection gets the SQLite store without having
// to know that NewSQLite exists. Set Config.DatabaseDialect to skip detection
// and say which one you want.
//
// This used to assume PostgreSQL unconditionally. Handing it a SQLite
// connection was not an error anyone saw at startup: the wrong SQL reached the
// database on the first query that happened to differ.
func New(cfg Config) (*Escalated, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("escalated: Config.DB is required")
	}

	applyDefaults(&cfg)

	if cfg.DatabaseDialect == "" {
		dialect, err := DetectDialect(cfg.DB)
		if err != nil {
			return nil, err
		}

		cfg.DatabaseDialect = dialect
	}

	var s store.Store

	switch cfg.DatabaseDialect {
	case DialectSQLite:
		s = store.NewSQLiteStore(cfg.DB, cfg.TablePrefix)
	case DialectPostgres:
		s = store.NewPostgresStore(cfg.DB, cfg.TablePrefix)
	default:
		return nil, fmt.Errorf(
			"escalated: unsupported Config.DatabaseDialect %q; use %q or %q",
			cfg.DatabaseDialect, DialectPostgres, DialectSQLite,
		)
	}

	var rend renderer.Renderer
	if cfg.UIEnabled {
		rend = renderer.NewInertiaRenderer("")
	} else {
		rend = renderer.NewJSONRenderer()
	}

	access, err := ticketaccess.New(cfg.DB, cfg.TablePrefix, ticketaccess.Options{Visibility: cfg.Visibility, Accounts: cfg.Accounts, TargetedSLAs: cfg.TargetedSLAs, ResolveUser: cfg.TicketAccessUserResolver})
	if err != nil {
		return nil, err
	}
	if configured, ok := s.(interface{ ConfigureTicketAccess(*ticketaccess.Access) }); ok {
		configured.ConfigureTicketAccess(access)
	}
	return &Escalated{
		Access:      access,
		Config:      cfg,
		Store:       s,
		Renderer:    rend,
		GuestAccess: newGuestAccess(cfg),
		Notifier:    newNotifier(cfg, access),
	}, nil
}

// NewSQLite is New with the SQLite store chosen explicitly, skipping detection.
// New picks the same store on its own for a SQLite connection; this stays for
// hosts that would rather be explicit, and for a driver detection cannot name.
func NewSQLite(cfg Config) (*Escalated, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("escalated: Config.DB is required")
	}

	applyDefaults(&cfg)
	cfg.DatabaseDialect = DialectSQLite
	s := store.NewSQLiteStore(cfg.DB, cfg.TablePrefix)

	var rend renderer.Renderer
	if cfg.UIEnabled {
		rend = renderer.NewInertiaRenderer("")
	} else {
		rend = renderer.NewJSONRenderer()
	}

	access, err := ticketaccess.New(cfg.DB, cfg.TablePrefix, ticketaccess.Options{Visibility: cfg.Visibility, Accounts: cfg.Accounts, TargetedSLAs: cfg.TargetedSLAs, ResolveUser: cfg.TicketAccessUserResolver})
	if err != nil {
		return nil, err
	}
	s.ConfigureTicketAccess(access)
	return &Escalated{
		Access:      access,
		Config:      cfg,
		Store:       s,
		Renderer:    rend,
		GuestAccess: newGuestAccess(cfg),
		Notifier:    newNotifier(cfg, access),
	}, nil
}

// StartNewsletterDispatcher starts the optional minute-cadence newsletter
// worker. The returned function stops the goroutine. When newsletters are
// disabled, the worker is inert and performs no database work.
func (e *Escalated) StartNewsletterDispatcher(ctx context.Context) func() {
	cfg := e.Config
	sqlStore := newsletter.NewSQLStore(cfg.DB, cfg.TablePrefix, cfg.DatabaseDialect)
	bounces := newsletter.NewBounceSuppressionStore(sqlStore)
	segments := newsletter.NewContactSegmentResolver(sqlStore)
	planner := newsletter.NewNewsletterPlanner(sqlStore, segments, bounces)
	renderer := newsletter.NewRenderer(newsletter.Config{
		BaseURL:             cfg.Newsletters.BaseURL,
		DefaultTheme:        cfg.Newsletters.DefaultTheme,
		TrackingEnabled:     cfg.Newsletters.TrackingEnabled,
		ThemesDir:           cfg.Newsletters.ThemesDir,
		BatchSize:           cfg.Newsletters.BatchSize,
		ClaimTimeoutMinutes: cfg.Newsletters.ClaimTimeoutMinutes,
		AutoPauseBounceRate: cfg.Newsletters.AutoPauseBounceRate,
		AutoPauseThreshold:  cfg.Newsletters.AutoPauseThreshold,
		EnableNewsletters:   cfg.EnableNewsletters,
		Brand: newsletter.Brand{
			Name:            cfg.Newsletters.BrandName,
			Accent:          cfg.Newsletters.BrandAccent,
			LogoURL:         cfg.Newsletters.BrandLogoURL,
			PhysicalAddress: cfg.Newsletters.BrandPhysicalAddress,
		},
	})
	var mailer newsletter.Mailer
	if cfg.NewsletterMailer != nil {
		mailer = newsletterMailerBridge{mailer: cfg.NewsletterMailer}
	}
	dispatcher := newsletter.NewNewsletterDispatcher(sqlStore, renderer, mailer, newsletter.DispatcherConfig{
		EnableNewsletters:   cfg.EnableNewsletters,
		BatchSize:           cfg.Newsletters.BatchSize,
		RateLimitPerMinute:  cfg.Newsletters.RateLimitPerMinute,
		ClaimTimeoutMinutes: cfg.Newsletters.ClaimTimeoutMinutes,
		AutoPauseBounceRate: cfg.Newsletters.AutoPauseBounceRate,
		AutoPauseThreshold:  cfg.Newsletters.AutoPauseThreshold,
		BaseURL:             cfg.Newsletters.BaseURL,
	})
	return newsletter.NewWorker(sqlStore, planner, dispatcher, func() bool { return cfg.EnableNewsletters }).Start(ctx)
}

func applyDefaults(cfg *Config) {
	if cfg.RoutePrefix == "" {
		cfg.RoutePrefix = "/escalated"
	}
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = "escalated_"
	}
	if cfg.Newsletters.DefaultTheme == "" {
		cfg.Newsletters.DefaultTheme = "default"
	}
	if cfg.Newsletters.BatchSize <= 0 {
		cfg.Newsletters.BatchSize = 50
	}
	if cfg.Newsletters.RateLimitPerMinute <= 0 {
		cfg.Newsletters.RateLimitPerMinute = 60
	}
	if cfg.Newsletters.ClaimTimeoutMinutes <= 0 {
		cfg.Newsletters.ClaimTimeoutMinutes = 10
	}
	if cfg.Newsletters.AutoPauseBounceRate <= 0 {
		cfg.Newsletters.AutoPauseBounceRate = 0.05
	}
	if cfg.Newsletters.AutoPauseThreshold <= 0 {
		cfg.Newsletters.AutoPauseThreshold = 100
	}
	if cfg.Newsletters.BrandAccent == "" {
		cfg.Newsletters.BrandAccent = "#2563eb"
	}
	if cfg.AdminCheck == nil {
		cfg.AdminCheck = func(_ *http.Request) bool { return false }
	}
	if cfg.AgentCheck == nil {
		cfg.AgentCheck = func(_ *http.Request) bool { return false }
	}
	if cfg.UserIDFunc == nil {
		cfg.UserIDFunc = func(_ *http.Request) models.UserID { return "" }
	}
}

// TicketAccessMiddleware scopes all package query paths after host authentication.
// A request that handlers.WidgetCORS let in from an allowed embedding site is
// always scoped as a guest, whoever is signed in to the host in that browser.
func (e *Escalated) TicketAccessMiddleware(next http.Handler) http.Handler {
	agent, admin, userID := e.Config.AgentCheck, e.Config.AdminCheck, e.Config.UserIDFunc
	embedded := func(r *http.Request) bool { return handlers.WidgetCrossOrigin(r.Context()) != "" }
	return e.Access.Middleware(
		func(r *http.Request) bool { return !embedded(r) && agent(r) },
		func(r *http.Request) bool { return !embedded(r) && admin(r) },
		func(r *http.Request) models.UserID {
			if embedded(r) {
				return ""
			}
			return userID(r)
		},
	)(next)
}

// NewTicketService returns a ticket service wired to fire its events to
// outbound webhooks and event-driven Workflows, the way the mounted routes use
// it. Workflow tag actions go back through it, so they are announced too.
func (e *Escalated) NewTicketService() *services.TicketService {
	ts := services.NewTicketService(e.Store)
	ts.Webhooks = services.NewWebhookDispatcher(e.Config.DB, nil)
	ts.Workflows = services.NewWorkflowRunner(e.Config.DB, nil, e.Access)
	ts.Workflows.Tickets = ts
	ts.Notifications = e.Notifier
	ts.Statuses = e.TicketStatuses()
	return ts
}

// TicketStatuses returns the custom ticket statuses over the host database,
// with every table name built from Config.TablePrefix. Nil without a
// database, which leaves status inputs to the built-in statuses.
func (e *Escalated) TicketStatuses() *services.TicketStatusService {
	if e.Config.DB == nil {
		return nil
	}
	return &services.TicketStatusService{DB: e.Config.DB, Prefix: e.Config.TablePrefix}
}

// CheckSLA marks tickets whose SLA deadline has passed and fires sla.breached
// for each, then fires sla.warning for deadlines in the next warningMinutes
// (30 when <= 0), to webhooks and workflows. Call it from the host's
// scheduler, every minute being the convention, or use StartSLAChecker.
func (e *Escalated) CheckSLA(ctx context.Context, warningMinutes int) (breaches, warnings int, err error) {
	sla := services.NewSLAService(e.Store)
	sla.Tickets = e.NewTicketService()
	return sla.Check(ctx, warningMinutes)
}

// StartSLAChecker runs CheckSLA every interval (one minute when <= 0) with
// the default warning window until ctx is done or the returned function is
// called. Errors are logged and the next tick runs as usual.
func (e *Escalated) StartSLAChecker(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		interval = time.Minute
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, _, err := e.CheckSLA(ctx, 0); err != nil {
					log.Printf("escalated: SLA check: %v", err)
				}
			}
		}
	}()
	return cancel
}

func newGuestAccess(cfg Config) *services.GuestAccess {
	return services.NewGuestAccess(cfg.DB, cfg.TablePrefix, cfg.GuestAccess, cfg.Mailer, cfg.MailFrom, cfg.GuestRateLimit.Store, nil)
}

func newNotifier(cfg Config, access *ticketaccess.Access) *services.Notifier {
	if cfg.Mailer == nil || cfg.Notifications.Disabled {
		return nil
	}
	n := cfg.Notifications
	branding := n.Branding
	def := services.DefaultEmailBranding()
	if branding.AccentColor == "" {
		branding.AccentColor = def.AccentColor
	}
	if branding.FooterText == "" {
		branding.FooterText = def.FooterText
	}
	if branding.CompanyName == "" {
		branding.CompanyName = def.CompanyName
	}
	domain := n.Domain
	base := strings.TrimRight(n.BaseURL, "/")
	if domain == "" && base != "" {
		if u, err := url.Parse(base); err == nil {
			domain = u.Hostname()
		}
	}
	var ticketURL func(t *models.Ticket, agent bool) string
	if base != "" {
		prefix := base + cfg.RoutePrefix
		ticketURL = func(t *models.Ticket, agent bool) string {
			if agent {
				return fmt.Sprintf("%s/agent/tickets/%d", prefix, t.ID)
			}
			if t.RequesterID != nil && !t.RequesterID.Empty() {
				return fmt.Sprintf("%s/tickets/%d", prefix, t.ID)
			}
			return ""
		}
	}
	return &services.Notifier{
		Mailer:        cfg.Mailer,
		From:          cfg.MailFrom,
		Domain:        domain,
		InboundSecret: n.InboundSecret,
		Branding:      branding,
		UserEmail:     cfg.UserEmail,
		TicketURL:     ticketURL,
		DB:            cfg.DB,
		Access:        access,
		Sync:          n.Sync,
	}
}
