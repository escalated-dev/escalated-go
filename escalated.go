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
	"sync"
	"time"

	"github.com/escalated-dev/escalated-go/ticketaccess"

	"github.com/escalated-dev/escalated-go/access"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/plugins"
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
	// Plugins is the plugin registry: Config.Plugins' native plugins and the
	// SDK plugins the runtime reports, their activation and their hooks. Host
	// code applies its own filter points with Plugins.ApplyFilter.
	Plugins *plugins.Host
	// Features are the install-level access switches, resolved from
	// Config.Access (else their environment variables) when New ran.
	Features access.Features

	twoFactorOnce sync.Once
	twoFactorGate *access.TwoFactorGate

	importsOnce sync.Once
	imports     *services.ImportService
}

// Imports returns the import service behind the admin Import wizard: the
// Config.ImportAdapters, their jobs and runs. Nil without a database.
func (e *Escalated) Imports() *services.ImportService {
	if e.Config.DB == nil {
		return nil
	}
	e.importsOnce.Do(func() {
		secret := e.Config.ImportSecret
		if secret == "" {
			secret = e.Config.GuestAccess.Secret
		}
		e.imports = &services.ImportService{
			DB:                 e.Config.DB,
			Prefix:             e.Config.TablePrefix,
			Store:              e.Store,
			Adapters:           e.Config.ImportAdapters,
			Secret:             secret,
			UserIDByEmail:      e.Config.UserIDByEmail,
			UserType:           "User",
			Dispatch:           e.Config.ImportDispatcher,
			AttachmentStorage:  e.Config.ImportAttachmentStorage,
			AttachmentMaxBytes: e.Config.ImportAttachmentMaxBytes,
		}
	})
	return e.imports
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
	pluginHost, err := newPluginHost(cfg)
	if err != nil {
		return nil, err
	}
	return checkAccess(wirePlugins(&Escalated{
		Access:      access,
		Config:      cfg,
		Store:       s,
		Renderer:    rend,
		GuestAccess: newGuestAccess(cfg),
		Notifier:    newNotifier(cfg, access),
		Plugins:     pluginHost,
	}))
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
	pluginHost, err := newPluginHost(cfg)
	if err != nil {
		return nil, err
	}
	return checkAccess(wirePlugins(&Escalated{
		Access:      access,
		Config:      cfg,
		Store:       s,
		Renderer:    rend,
		GuestAccess: newGuestAccess(cfg),
		Notifier:    newNotifier(cfg, access),
		Plugins:     pluginHost,
	}))
}

// wirePlugins gives the plugin context its domain: the store, a ticket
// service that fires events like the routes' one, and the host's agents.
func wirePlugins(e *Escalated) *Escalated {
	e.Features = e.Config.Access.Resolve()
	domain := plugins.Domain{
		DB:          e.Config.DB,
		TablePrefix: e.Config.TablePrefix,
		Store:       e.Store,
		Tickets:     e.NewTicketService(),
	}
	if dir := e.Config.SkillAgentDirectory; dir != nil {
		domain.Agents = func(ctx context.Context) ([]plugins.Agent, error) {
			rows, err := dir.ListAgentsForSkillForm(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]plugins.Agent, 0, len(rows))
			for _, row := range rows {
				var email *string
				if row.Email != "" {
					email = &row.Email
				}
				out = append(out, plugins.Agent{ID: row.ID, Name: row.Name, Email: email})
			}
			return out, nil
		}
	}
	e.Plugins.SetDomain(domain)
	return e
}

// newPluginHost builds the plugin registry. A malformed native plugin, or two
// plugins with one slug, is an error here, at startup.
func newPluginHost(cfg Config) (*plugins.Host, error) {
	reserved := append([]string{}, services.SupportedWebhookEvents...)
	for legacy := range models.LegacyEventNames {
		reserved = append(reserved, legacy)
	}
	return plugins.New(cfg.Plugins, plugins.Options{
		DB:          cfg.DB,
		TablePrefix: cfg.TablePrefix,
		RoutePrefix: cfg.RoutePrefix,
		AdminCheck:  cfg.AdminCheck,
		UserID:      cfg.UserIDFunc,
		UserName:    cfg.UserName,
		UserEmail:   cfg.UserEmail,
		ClientIP:    cfg.GuestRateLimit.ClientIP,
		Reserved:    reserved,
		Version:     Version,
	})
}

// Close stops what New started in the background: the plugin runtime.
func (e *Escalated) Close() {
	e.Plugins.Close()
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
	if cfg.APITokenOwner == nil {
		cfg.APITokenOwner = handlers.APITokenOwnerFromDirectory(cfg.UserDirectory)
	}
	tokenAwareChecks(cfg)
}

// tokenAwareChecks makes AgentCheck, AdminCheck and UserIDFunc answer for a
// request that authenticated with an API token (APITokenMiddleware): the
// token's owner, narrowed by its abilities. Every other request gets the
// host's answer, unchanged.
func tokenAwareChecks(cfg *Config) {
	agent, admin, userID := cfg.AgentCheck, cfg.AdminCheck, cfg.UserIDFunc
	cfg.AgentCheck = func(r *http.Request) bool {
		if id := handlers.APITokenFrom(r.Context()); id != nil {
			return id.Agent()
		}
		return agent(r)
	}
	cfg.AdminCheck = func(r *http.Request) bool {
		if id := handlers.APITokenFrom(r.Context()); id != nil {
			return id.Admin()
		}
		return admin(r)
	}
	cfg.UserIDFunc = func(r *http.Request) models.UserID {
		if id := handlers.APITokenFrom(r.Context()); id != nil {
			return id.Token.UserID
		}
		return userID(r)
	}
}

// APITokens returns the API token store over the host database, with the
// table name built from Config.TablePrefix. Nil without a database.
func (e *Escalated) APITokens() *services.APITokenService {
	if e.Config.DB == nil {
		return nil
	}
	return &services.APITokenService{DB: e.Config.DB, Prefix: e.Config.TablePrefix}
}

// APITokenMiddleware authenticates the REST API's agent routes
// ({RoutePrefix}/api/tickets, /api/departments and /api/tags) with API tokens
// when Config.APITokensEnabled is set, and does nothing otherwise. The routers
// mount it ahead of TicketAccessMiddleware, so ticket visibility is the token
// owner's.
func (e *Escalated) APITokenMiddleware(next http.Handler) http.Handler {
	if !e.Config.APITokensEnabled || e.Config.DB == nil {
		return next
	}
	api := strings.TrimRight(e.Config.RoutePrefix, "/") + "/api/"
	auth := &handlers.APITokenAuth{
		Tokens:    e.APITokens(),
		Owner:     e.Config.APITokenOwner,
		HostAgent: e.Config.AgentCheck,
		HostAdmin: e.Config.AdminCheck,
		Applies: func(path string) bool {
			rest, ok := strings.CutPrefix(path, api)
			if !ok {
				return false
			}
			for _, root := range []string{"tickets", "departments", "tags"} {
				if rest == root || strings.HasPrefix(rest, root+"/") {
					return true
				}
			}
			return false
		},
	}
	return auth.Middleware(next)
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
	if e.Plugins != nil {
		ts.Plugins = e.Plugins
	}
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

// RunPluginCron runs the cron schedules of active SDK plugins that are due
// this minute (UTC). Call it from the host's scheduler every minute, or use
// StartPluginCron.
func (e *Escalated) RunPluginCron(ctx context.Context) {
	e.Plugins.RunDueCron(ctx, time.Now())
}

// StartPluginCron runs RunPluginCron at the start of every minute until ctx
// is done or the returned function is called.
func (e *Escalated) StartPluginCron(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			next := time.Now().Truncate(time.Minute).Add(time.Minute)
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				e.Plugins.RunDueCron(ctx, next)
			}
		}
	}()
	return cancel
}

// PurgeExpired applies the data retention settings (the admin Data retention
// page): closed tickets, attachments and audit logs past their periods are
// deleted, and so are expired guest verification codes. Every period defaults
// to "never", which deletes nothing. With dryRun it only counts. Laravel's
// escalated:purge-expired; call it from the host's scheduler, daily being the
// convention, or use StartRetentionPurger.
func (e *Escalated) PurgeExpired(ctx context.Context, dryRun bool) (*services.RetentionReport, error) {
	return services.NewRetentionService(e.Config.DB, e.Store).WithPrefix(e.Config.TablePrefix).PurgeExpired(ctx, time.Now(), dryRun)
}

// StartRetentionPurger runs PurgeExpired every interval (daily when <= 0)
// until ctx is done or the returned function is called. Errors are logged
// and the next tick runs as usual.
func (e *Escalated) StartRetentionPurger(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		interval = 24 * time.Hour
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
				if _, err := e.PurgeExpired(ctx, false); err != nil {
					log.Printf("escalated: retention purge: %v", err)
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

// Roles returns the roles & permissions store over the host database: seed
// it with Roles().Seed and give the first admin a role with
// Roles().Assign(ctx, userID, "admin") before switching roles & permissions
// on. Nil without a database.
func (e *Escalated) Roles() *access.Store {
	if e.Config.DB == nil {
		return nil
	}
	return &access.Store{DB: e.Config.DB, Prefix: e.Config.TablePrefix}
}

// checkAccess refuses a configuration that switches a feature on without
// what it needs to be enforced.
func checkAccess(e *Escalated) (*Escalated, error) {
	if e.Features.SSO {
		o := e.SSOOptions()
		switch {
		case o.SignIn == nil:
			return nil, fmt.Errorf("escalated: SSO is on but Config.Access.SSOOptions.SignIn is nil; Escalated owns no sessions, so it needs your sign-in (docs/access-control.md)")
		case e.Config.UserIDByEmail == nil:
			return nil, fmt.Errorf("escalated: SSO is on but Config.UserIDByEmail is nil; it matches the identity's email to your users (docs/access-control.md)")
		case o.BaseURL == "":
			return nil, fmt.Errorf("escalated: SSO is on but no base URL is set (Config.Access.SSOOptions.BaseURL, Config.Notifications.BaseURL or ESCALATED_SSO_BASE_URL); the IdP needs absolute URLs (docs/access-control.md)")
		case *o.CreateUsers && o.CreateUser == nil:
			return nil, fmt.Errorf("escalated: SSO creates users but Config.Access.SSOOptions.CreateUser is nil (docs/access-control.md)")
		case len(e.accessKey()) == 0:
			return nil, fmt.Errorf("escalated: SSO is on but neither Config.Access.Secret nor Config.GuestAccess.Secret is set (docs/access-control.md)")
		}
	}
	if e.Features.TwoFactor {
		if e.Config.Access.SessionID == nil {
			return nil, fmt.Errorf("escalated: two-factor is on but Config.Access.SessionID is nil; it is needed so every new login is challenged (docs/access-control.md)")
		}
		if len(e.accessKey()) == 0 {
			return nil, fmt.Errorf("escalated: two-factor is on but neither Config.Access.Secret nor Config.GuestAccess.Secret is set (docs/access-control.md)")
		}
	}
	return e, nil
}

func (e *Escalated) accessKey() []byte {
	if e.Config.Access.Secret != "" {
		return []byte(e.Config.Access.Secret)
	}
	return []byte(e.Config.GuestAccess.Secret)
}

// TwoFactor returns the two-factor enrolments over the host database. Use
// TwoFactor().Disable(ctx, userID) to clear a user's enrolment after a lost
// device (escalated-laravel's escalated:two-factor:reset). Nil without a
// database.
func (e *Escalated) TwoFactor() *access.TwoFactor {
	if e.Config.DB == nil {
		return nil
	}
	return &access.TwoFactor{DB: e.Config.DB, Prefix: e.Config.TablePrefix, Key: e.accessKey(), Issuer: e.Config.Access.TwoFactorIssuer}
}

// TwoFactorRequired reports whether staff must enrol: the
// two_factor_required setting when an admin has saved it, else
// Config.Access.TwoFactorRequired (ESCALATED_TWO_FACTOR_REQUIRED). Always
// false while two-factor is off.
func (e *Escalated) TwoFactorRequired(ctx context.Context) bool {
	if !e.Features.TwoFactor {
		return false
	}
	if v, err := e.Store.GetSetting(ctx, handlers.SettingTwoFactorRequired); err == nil && v != "" {
		return v == "1" || strings.EqualFold(v, "true")
	}
	return e.Config.Access.TwoFactorRequiredDefault()
}

// TwoFactorGate is the challenge: the middleware AccessMiddleware runs and
// what the challenge and enrolment screens use. One per Escalated, so the
// challenge's attempt limit is shared by both routers.
func (e *Escalated) TwoFactorGate() *access.TwoFactorGate {
	e.twoFactorOnce.Do(func() {
		cfg := e.Config
		e.twoFactorGate = &access.TwoFactorGate{
			Prefix:            cfg.RoutePrefix,
			Enabled:           e.Features.TwoFactor,
			Service:           e.TwoFactor(),
			Key:               e.accessKey(),
			UserID:            cfg.UserIDFunc,
			IsStaff:           func(r *http.Request) bool { return cfg.AgentCheck(r) || cfg.AdminCheck(r) },
			IsToken:           func(r *http.Request) bool { return handlers.APITokenFrom(r.Context()) != nil },
			Required:          func(r *http.Request) bool { return e.TwoFactorRequired(r.Context()) },
			SessionID:         cfg.Access.SessionID,
			RegenerateSession: cfg.Access.RegenerateSession,
		}
	})
	return e.twoFactorGate
}

// AccessMiddleware shares escalated.features, escalated.permissions and
// escalated.two_factor_required with every page; while two-factor is on,
// challenges enrolled staff (TwoFactorGate); and while roles & permissions
// is on, requires each admin, agent and staff API route's permission
// (access.RoutePermissions). The routers mount it under the route prefix
// after APITokenMiddleware.
func (e *Escalated) AccessMiddleware(next http.Handler) http.Handler {
	cfg := e.Config
	statuses := e.TicketStatuses()
	enforcer := &access.Enforcer{
		Prefix:   cfg.RoutePrefix,
		Features: e.Features,
		Store:    e.Roles(),
		UserID:   cfg.UserIDFunc,
		Status: func(ctx context.Context, value any) (int, bool) {
			status, _, ok, err := statuses.Resolve(ctx, value)
			return status, ok && err == nil
		},
	}
	return e.AccessSharer().Middleware(e.TwoFactorGate().Middleware(enforcer.Middleware(next)))
}

// AccessSharer is what AccessMiddleware shares with the pages; Allows
// answers a permission question for a request.
func (e *Escalated) AccessSharer() *access.Sharer {
	cfg := e.Config
	isStaff := func(r *http.Request) bool { return cfg.AgentCheck(r) || cfg.AdminCheck(r) }
	return &access.Sharer{
		Features: e.Features,
		Store:    e.Roles(),
		IsStaff:  isStaff,
		UserID:   cfg.UserIDFunc,
		// Whether the signed-in staff user must still enrol.
		TwoFactorRequired: func(r *http.Request) bool {
			if !e.TwoFactorRequired(r.Context()) || !isStaff(r) {
				return false
			}
			enrolment, err := e.TwoFactor().Confirmed(r.Context(), cfg.UserIDFunc(r))
			return err == nil && enrolment == nil
		},
	}
}

// SSOOptions are Config.Access.SSOOptions with the environment and defaults
// applied: the base URL falls back to Notifications.BaseURL.
func (e *Escalated) SSOOptions() access.SSOOptions {
	o := e.Config.Access.SSOOptions
	if o.BaseURL == "" {
		o.BaseURL = e.Config.Notifications.BaseURL
	}
	return o.Resolved()
}

// SSO returns single sign-on over the settings store: the provider chosen
// in Admin > Settings > SSO and the install-level SSOOptions. Nil without a
// database.
func (e *Escalated) SSO() *access.SSO {
	if e.Config.DB == nil {
		return nil
	}
	o := e.SSOOptions()
	base := o.BaseURL + e.Config.RoutePrefix + "/sso"
	return &access.SSO{
		DB:       e.Config.DB,
		Prefix:   e.Config.TablePrefix,
		Settings: e.Store,
		Endpoints: access.SSOEndpoints{
			Login:         base + "/login",
			Metadata:      base + "/saml/metadata",
			ACS:           base + "/saml/acs",
			OAuthRedirect: base + "/oauth/callback",
			JWTCallback:   base + "/jwt/callback",
		},
		SPEntityID:        o.SPEntityID,
		AllowIdPInitiated: *o.AllowIdPInitiated,
		JWTAudience:       o.JWTAudience,
		ClockSkew:         o.ClockSkew(),
		HTTPClient:        o.HTTPClient,
	}
}

// SSOUsers is how an SSO identity becomes one of your users.
func (e *Escalated) SSOUsers() *access.SSOUsers {
	o := e.SSOOptions()
	return &access.SSOUsers{
		ByEmail:     e.Config.UserIDByEmail,
		Create:      o.CreateUser,
		CreateUsers: *o.CreateUsers,
		SyncRoles:   *o.SyncRoles && e.Features.RolesPermissions,
		Roles:       e.Roles(),
	}
}

// SSOStateKey is the secret the SSO state cookie is sealed with (a key is
// derived from it for that purpose alone).
func (e *Escalated) SSOStateKey() []byte { return e.accessKey() }
