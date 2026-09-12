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
	"net/http"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/renderer"
	"github.com/escalated-dev/escalated-go/services/newsletter"
	"github.com/escalated-dev/escalated-go/store"
)

// Version is the current version of the Escalated Go package.
const Version = "0.1.0"

// Escalated is the central container that holds the store, renderer, and config.
// Create one with New() and pass it to router.MountChi or router.MountStdlib.
type Escalated struct {
	Config   Config
	Store    store.Store
	Renderer renderer.Renderer
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

	return &Escalated{
		Config:   cfg,
		Store:    s,
		Renderer: rend,
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

	return &Escalated{
		Config:   cfg,
		Store:    s,
		Renderer: rend,
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
