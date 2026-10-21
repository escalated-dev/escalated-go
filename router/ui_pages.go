package router

import (
	"net/http"
	"strconv"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
)

// uiPageDeps are the services the shared pages read from besides the store.
type uiPageDeps struct {
	subjects *services.TicketSubjectService
	macros   *services.MacroService
	canned   *services.CannedResponseService

	// The admin handlers that serve a page beside their JSON.
	escalations *handlers.EscalationHandler
	macroAdmin  *handlers.MacroHandler
	cannedAdmin *handlers.CannedResponseHandler
	webhooks    *handlers.WebhookHandler
}

// wireUIPages gives the page handlers what they need to build the props the
// shared Vue pages read: the database for counts, the host's user lookups,
// the route prefix for links, and the ticket screen's panels.
func wireUIPages(esc *escalated.Escalated, agentH *handlers.AgentHandler, customerH *handlers.CustomerHandler, adminH *handlers.AdminHandler, deps uiPageDeps) {
	cfg := esc.Config
	users := handlers.UserLookup{Name: cfg.UserName, Email: cfg.UserEmail}

	agentH.DB = cfg.DB
	agentH.TablePrefix = cfg.TablePrefix
	agentH.Users = users
	agentH.RoutePrefix = cfg.RoutePrefix
	agentH.Subjects = deps.subjects
	agentH.Macros = deps.macros
	agentH.CannedResponses = deps.canned
	agentH.Agents = cfg.SkillAgentDirectory
	agentH.IsAdmin = cfg.AdminCheck

	customerH.DB = cfg.DB
	customerH.TablePrefix = cfg.TablePrefix
	customerH.Users = users
	customerH.RoutePrefix = cfg.RoutePrefix

	adminH.DB = cfg.DB
	adminH.TablePrefix = cfg.TablePrefix
	adminH.RoutePrefix = cfg.RoutePrefix
	adminH.TargetedSLAs = cfg.TargetedSLAs

	pages := handlers.PageSupport{Renderer: esc.Renderer, RoutePrefix: cfg.RoutePrefix, UserID: cfg.UserIDFunc, Users: users}
	if deps.escalations != nil {
		deps.escalations.Pages = pages
	}
	if deps.macroAdmin != nil {
		deps.macroAdmin.Pages = pages
	}
	if deps.cannedAdmin != nil {
		deps.cannedAdmin.Pages = pages
	}
	if deps.webhooks != nil {
		deps.webhooks.Pages = pages
	}
}

// ticketRoutes is the part of a router the ticket routes are added to: a chi
// router (chiGroup), or a ServeMux under a prefix and middleware (muxGroup).
type ticketRoutes interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
	Delete(pattern string, h http.HandlerFunc)
}

// muxGroup adds routes to a ServeMux under a path prefix, behind middleware.
type muxGroup struct {
	mux    *http.ServeMux
	prefix string
	mw     func(http.Handler) http.Handler
}

func (g muxGroup) handle(method, pattern string, h http.HandlerFunc) {
	g.mux.Handle(method+" "+g.prefix+pattern, g.mw(h))
}

func (g muxGroup) Get(pattern string, h http.HandlerFunc)    { g.handle(http.MethodGet, pattern, h) }
func (g muxGroup) Post(pattern string, h http.HandlerFunc)   { g.handle(http.MethodPost, pattern, h) }
func (g muxGroup) Put(pattern string, h http.HandlerFunc)    { g.handle(http.MethodPut, pattern, h) }
func (g muxGroup) Delete(pattern string, h http.HandlerFunc) { g.handle(http.MethodDelete, pattern, h) }

// chiRoutes is the subset of chi.Router the ticket routes use.
type chiRoutes interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
	Delete(pattern string, h http.HandlerFunc)
}

// chiGroup adds routes to a chi router under a path prefix.
type chiGroup struct {
	r      chiRoutes
	prefix string
}

func (g chiGroup) Get(p string, h http.HandlerFunc)    { g.r.Get(g.prefix+p, h) }
func (g chiGroup) Post(p string, h http.HandlerFunc)   { g.r.Post(g.prefix+p, h) }
func (g chiGroup) Put(p string, h http.HandlerFunc)    { g.r.Put(g.prefix+p, h) }
func (g chiGroup) Delete(p string, h http.HandlerFunc) { g.r.Delete(g.prefix+p, h) }

// mountTicketWrites adds the ticket screen's writes, the same under /agent and
// /admin (escalated.agent.tickets.* and escalated.admin.tickets.*). Patterns
// are relative to the tickets collection.
func mountTicketWrites(r ticketRoutes, h *handlers.AgentHandler) {
	r.Put("/{id}", h.UpdateTicket)
	r.Post("/{id}/reply", h.PageReply)
	r.Post("/{id}/note", h.PageNote)
	r.Post("/{id}/assign", h.AssignTicket)
	r.Post("/{id}/status", h.ChangeStatus)
	r.Post("/{id}/priority", h.ChangePriority)
	r.Post("/{id}/tags", h.UpdateTags)
	r.Post("/{id}/department", h.ChangeDepartment)
	r.Post("/{id}/macro", h.ApplyMacro)
	r.Post("/{id}/follow", h.ToggleFollow)
	r.Post("/{id}/replies/{reply}/pin", h.TogglePin)
}

// mountTicketPanels adds the ticket screen's panels (subjects, typed links,
// side conversations). Their handlers parse a numeric {id}; byReference lets
// the pages name the ticket by reference as well.
func mountTicketPanels(r ticketRoutes, s store.Store, subjects *handlers.TicketSubjectHandler, links *handlers.TicketLinkHandler, side *handlers.SideConversationHandler) {
	ref := func(h http.HandlerFunc) http.HandlerFunc { return byReference(s, h) }
	r.Post("/{id}/subjects", ref(subjects.AttachSubject))
	r.Delete("/{id}/subjects/{subject}", ref(subjects.DetachSubject))
	r.Get("/{id}/links", ref(links.List))
	r.Post("/{id}/links", ref(links.Create))
	r.Delete("/{id}/links/{linkId}", ref(links.Delete))
	r.Get("/{id}/side-conversations", ref(side.List))
	r.Post("/{id}/side-conversations", ref(side.Create))
	r.Post("/{id}/side-conversations/{scId}/reply", ref(side.Reply))
	r.Post("/{id}/side-conversations/{scId}/close", ref(side.Close))
}

// byReference turns a ticket reference in the {id} path value into the
// ticket's id, so handlers that parse a numeric id accept the references the
// pages link with. An unknown reference is left as it is for the handler to
// answer.
func byReference(s store.Store, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if v := r.PathValue("id"); v != "" {
			if _, err := strconv.ParseInt(v, 10, 64); err != nil {
				if t, err := s.GetTicketByReference(r.Context(), v); err == nil && t != nil {
					r.SetPathValue("id", strconv.FormatInt(t.ID, 10))
				}
			}
		}
		next(w, r)
	}
}
