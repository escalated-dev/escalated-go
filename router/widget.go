package router

import (
	"net/http"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// widgetRoute is one widget endpoint, relative to {RoutePrefix}/widget.
type widgetRoute struct {
	method  string
	path    string
	handler http.HandlerFunc
}

// widgetStack holds what MountChi and MountStdlib need to serve the widget
// when Config.EnableWidget is set.
type widgetStack struct {
	routes []widgetRoute
	// wrap puts WidgetCORS outside the ticket-access middleware, so an
	// allowed cross-origin request is scoped as a guest.
	wrap func(http.Handler) http.Handler
}

func newWidgetStack(esc *escalated.Escalated, ticketSvc *services.TicketService) widgetStack {
	cfg := esc.Config
	s := esc.Store

	widget := handlers.NewWidgetHandler(cfg.Widget, s, ticketSvc)
	widget.DB = cfg.DB
	widget.SignedIn = func(r *http.Request) bool {
		return handlers.WidgetCrossOrigin(r.Context()) == "" && !cfg.UserIDFunc(r).Empty()
	}
	chat := handlers.NewWidgetChatHandler(cfg.Widget, s,
		services.NewChatSessionService(s, services.NewChatRoutingService(s), nil),
		services.NewChatAvailabilityService(s))

	// Every widget route is throttled per client IP with the guest limiter
	// config (ClientIP, Store, Disabled), so behind a trusted proxy each
	// visitor gets their own budget instead of sharing the proxy's.
	limiter := middleware.NewGuestRateLimiter(cfg.GuestRateLimit).WithRequestsPerMinute(cfg.Widget.RateLimitPerMin)
	widget.Limiter = limiter
	chat.Limiter = limiter
	widget.Access = esc.GuestAccess
	chat.Access = esc.GuestAccess
	verify := &handlers.GuestVerificationHandler{
		Access:   esc.GuestAccess,
		ClientIP: limiter.ClientIP,
		Enabled:  func() bool { return cfg.Widget.Enabled },
		Allow: func(w http.ResponseWriter, r *http.Request, scope middleware.GuestScope) bool {
			return limiter.Allow(w, r, middleware.GuestRequestScope, scope)
		},
	}

	cors := handlers.WidgetCORS(cfg.Widget, s)
	return widgetStack{
		wrap: func(next http.Handler) http.Handler {
			return cors(esc.TicketAccessMiddleware(next))
		},
		routes: []widgetRoute{
			{http.MethodGet, "/config", widget.Config},
			{http.MethodGet, "/articles", widget.SearchArticles},
			{http.MethodGet, "/articles/{slug}", widget.ShowArticle},
			{http.MethodPost, "/tickets", widget.CreateTicket},
			// Verified guest access: email codes, lookup, grant-read status.
			{http.MethodPost, "/verification", verify.Request},
			{http.MethodPost, "/lookup", verify.Lookup},
			{http.MethodGet, "/tickets/lookup", widget.LookupTicket},
			{http.MethodGet, "/tickets/{reference}", widget.TicketStatus},
			{http.MethodGet, "/chat/availability", chat.Availability},
			{http.MethodPost, "/chat/start", chat.StartChat},
			// Chats are addressed by the unguessable id /chat/start returns.
			{http.MethodGet, "/chat/{id}/messages", chat.Messages},
			{http.MethodPost, "/chat/{id}/messages", chat.PostMessage},
			{http.MethodPost, "/chat/{id}/typing", chat.Typing},
			{http.MethodPost, "/chat/{id}/end", chat.End},
			{http.MethodPost, "/chat/{id}/rate", chat.Rate},
			// Older reference + X-Guest-Token routes.
			{http.MethodPost, "/chat/sessions/{ref}/messages", chat.SendMessage},
			{http.MethodPost, "/chat/sessions/{ref}/end", chat.EndChat},
		},
	}
}

// kbPublicGuard applies the knowledge_base_enabled / knowledge_base_public
// settings to the public KB API: 404 when off, 403 for a signed-out caller
// when private.
func kbPublicGuard(esc *escalated.Escalated) func(http.Handler) http.Handler {
	cfg := esc.Config
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provider := func() models.KBSettings { return handlers.KBSettingsFromStore(r.Context(), esc.Store) }
			signedIn := func(r *http.Request) bool {
				return !cfg.UserIDFunc(r).Empty() || cfg.AgentCheck(r) || cfg.AdminCheck(r)
			}
			middleware.RequireKBPublic(provider, signedIn)(next).ServeHTTP(w, r)
		})
	}
}
