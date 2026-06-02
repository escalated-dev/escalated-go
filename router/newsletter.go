package router

import (
	"context"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/services/newsletter"
)

type newsletterMailerAdapter struct {
	mailer escalated.NewsletterMailer
}

func (a newsletterMailerAdapter) SendNewsletter(ctx context.Context, msg newsletter.MailMessage) error {
	return a.mailer.SendNewsletter(ctx, escalated.NewsletterMail{
		To:       msg.To,
		From:     msg.From,
		ReplyTo:  msg.ReplyTo,
		Subject:  msg.Subject,
		HTML:     msg.HTML,
		Headers:  msg.Headers,
		TestSend: msg.TestSend,
	})
}

func newNewsletterStack(esc *escalated.Escalated) (*handlers.NewsletterHandler, *newsletter.Worker) {
	cfg := esc.Config
	store := newsletter.NewSQLStore(cfg.DB, cfg.TablePrefix, cfg.DatabaseDialect)
	bounces := newsletter.NewBounceSuppressionStore(store)
	segments := newsletter.NewContactSegmentResolver(store)
	planner := newsletter.NewNewsletterPlanner(store, segments, bounces)
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
		mailer = newsletterMailerAdapter{mailer: cfg.NewsletterMailer}
	}
	dispatcher := newsletter.NewNewsletterDispatcher(store, renderer, mailer, newsletter.DispatcherConfig{
		EnableNewsletters:   cfg.EnableNewsletters,
		BatchSize:           cfg.Newsletters.BatchSize,
		RateLimitPerMinute:  cfg.Newsletters.RateLimitPerMinute,
		ClaimTimeoutMinutes: cfg.Newsletters.ClaimTimeoutMinutes,
		AutoPauseBounceRate: cfg.Newsletters.AutoPauseBounceRate,
		AutoPauseThreshold:  cfg.Newsletters.AutoPauseThreshold,
		BaseURL:             cfg.Newsletters.BaseURL,
	})
	tracker := newsletter.NewNewsletterTracker(store, bounces)
	h := handlers.NewNewsletterHandler(store, esc.Renderer, renderer, planner, tracker, mailer, cfg.UserIDFunc, cfg.NewsletterPermissionCheck, handlers.NewsletterHandlerConfig{
		Enabled:            cfg.EnableNewsletters,
		DefaultFrom:        cfg.Newsletters.DefaultFrom,
		DefaultReplyTo:     cfg.Newsletters.DefaultReplyTo,
		DefaultTheme:       cfg.Newsletters.DefaultTheme,
		TrackingEnabled:    cfg.Newsletters.TrackingEnabled,
		ThemesDir:          cfg.Newsletters.ThemesDir,
		RateLimitPerMinute: cfg.Newsletters.RateLimitPerMinute,
		BatchSize:          cfg.Newsletters.BatchSize,
	})
	worker := newsletter.NewWorker(store, planner, dispatcher, func() bool { return cfg.EnableNewsletters })
	return h, worker
}
