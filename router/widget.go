package router

import (
	"net/http"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
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
	chat := handlers.NewWidgetChatHandler(cfg.Widget, s,
		services.NewChatSessionService(s, services.NewChatRoutingService(s), nil),
		services.NewChatAvailabilityService(s))

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
			{http.MethodGet, "/tickets/lookup", widget.LookupTicket},
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
