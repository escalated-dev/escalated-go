// Package router provides helpers to mount Escalated routes on popular Go routers.
package router

import (
	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/actions"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/services"
)

// MountChi mounts all Escalated routes on a Chi router under the configured prefix.
func MountChi(r chi.Router, esc *escalated.Escalated) {
	cfg := esc.Config
	s := esc.Store
	rend := esc.Renderer

	ticketSvc := services.NewTicketService(s)
	assignSvc := services.NewAssignmentService(s)
	subjectSvc := services.NewTicketSubjectService(s, cfg.TicketSubjectTypes, cfg.TicketSubjectResolver)

	// Outbound webhooks: the dispatcher is shared between the ticket service
	// (which fires lifecycle events) and the admin CRUD handler.
	webhookDispatcher := services.NewWebhookDispatcher(cfg.DB, nil)
	ticketSvc.Webhooks = webhookDispatcher

	// Event-driven Workflows: the runner fires active workflows on the ticket
	// lifecycle (created/replied/status-changed/assigned) off the request path,
	// mirroring the webhook dispatcher. The admin CRUD handler manages the rows.
	ticketSvc.Workflows = services.NewWorkflowRunner(cfg.DB, nil)

	actionRegistry := actions.NewRegistry(cfg.TicketActions)

	apiH := handlers.NewAPIHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	apiH.Subjects = subjectSvc
	subjectH := handlers.NewTicketSubjectHandler(subjectSvc, ticketSvc)
	apiH.Actions = actionRegistry
	apiH.OnCustomAction = cfg.OnCustomAction
	apiH.RoutePrefix = cfg.RoutePrefix

	agentH := handlers.NewAgentHandler(s, ticketSvc, assignSvc, rend, cfg.UserIDFunc)
	agentH.Actions = actionRegistry
	agentH.OnCustomAction = cfg.OnCustomAction
	agentH.RoutePrefix = cfg.RoutePrefix
	customerH := handlers.NewCustomerHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	adminH := handlers.NewAdminHandler(s, rend)
	attachH := handlers.NewAttachmentHandler(s, cfg.RoutePrefix)
	autoH := handlers.NewAutomationHandler(cfg.DB, services.NewAutomationRunner(cfg.DB, nil))
	escH := handlers.NewEscalationHandler(cfg.DB, services.NewEscalationService(cfg.DB, nil))
	satH := handlers.NewSatisfactionHandler(cfg.DB)
	capH := handlers.NewCapacityHandler(cfg.DB)
	linkH := handlers.NewTicketLinkHandler(cfg.DB)
	scH := handlers.NewSideConversationHandler(cfg.DB)
	authH := handlers.NewAuthHandler(cfg.APIAuth)
	guestH := handlers.NewGuestTicketHandler(s, ticketSvc)
	kbH := handlers.NewKBHandler(cfg.DB)
	retentionH := handlers.NewRetentionHandler(services.NewRetentionService(cfg.DB, s))
	macroH := handlers.NewMacroHandler(cfg.DB, services.NewMacroService(cfg.DB, nil))
	webhookH := handlers.NewWebhookHandler(cfg.DB, webhookDispatcher)
	workflowH := handlers.NewWorkflowHandler(cfg.DB)
	userH := handlers.NewUserHandler(cfg.UserDirectory, rend, cfg.UserIDFunc)
	skillsH := handlers.NewSkillsHandler(cfg.DB, cfg.TablePrefix, rend, cfg.SkillAgentDirectory)
	var newsletterH *handlers.NewsletterHandler
	if cfg.EnableNewsletters {
		newsletterH, _ = newNewsletterStack(esc)
	}

	r.Route(cfg.RoutePrefix, func(r chi.Router) {
		// Inertia protocol middleware scopes to the UI routes below, but chi
		// requires every middleware to be registered before any route is added
		// to a mux — so it must be declared here, ahead of the always-mounted
		// attachment/API routes, rather than inside the UI block. It is a no-op
		// for non-Inertia requests (API/attachment clients send no X-Inertia
		// header), so mounting it at the prefix root does not alter their
		// behavior. Without this, MountChi panics on startup whenever UIEnabled
		// is true, leaving every admin route (webhooks included) unreachable.
		if cfg.UIEnabled {
			r.Use(middleware.Inertia(""))
		}

		// Attachment downloads — always mounted
		r.Get("/attachments/{id}/download", attachH.Download)

		// JSON API — always mounted
		r.Route("/api", func(r chi.Router) {
			r.Get("/tickets", apiH.ListTickets)
			r.Post("/tickets", apiH.CreateTicket)
			r.Get("/tickets/{id}", apiH.ShowTicket)
			r.Patch("/tickets/{id}", apiH.UpdateTicket)
			r.Post("/tickets/{id}/replies", apiH.CreateReply)
			r.Post("/tickets/{id}/subjects", subjectH.AttachSubject)
			r.Delete("/tickets/{id}/subjects/{subject}", subjectH.DetachSubject)
			r.Post("/tickets/{id}/actions/{action}", apiH.CustomAction)
			r.Get("/departments", apiH.ListDepartments)
			r.Get("/tags", apiH.ListTags)

			// Authentication — delegated to host-app callbacks (handlers.APIAuth).
			r.Post("/auth/login", authH.Login)
			r.Post("/auth/register", authH.Register)
			r.Post("/auth/logout", authH.Logout)
			r.Post("/auth/refresh", authH.Refresh)
			r.Get("/auth/me", authH.Me)
			r.Patch("/auth/profile", authH.Profile)
			r.Post("/auth/validate", authH.Validate)

			// Anonymous (guest) ticket submission + lookup by token.
			r.Post("/guest/tickets", guestH.Create)
			r.Get("/guest/tickets/{token}", guestH.Show)
			r.Post("/guest/tickets/{token}/rate", satH.GuestRate)

			// Public knowledge base (published only).
			r.Get("/kb/articles", kbH.ListArticles)
			r.Get("/kb/categories", kbH.ListCategories)
			r.Get("/kb/articles/{slug}", kbH.ShowArticle)
		})

		if cfg.EnableNewsletters {
			r.Route("/n", func(r chi.Router) {
				r.Get("/o/{token}", newsletterH.OpenPixel)
				r.Get("/c/{token}", newsletterH.Click)
				r.Get("/u/{token}", newsletterH.UnsubscribeShow)
				r.Post("/u/{token}", newsletterH.UnsubscribeStore)
				r.Get("/v/{token}", newsletterH.ViewInBrowser)
			})
			r.Route("/webhooks/newsletter", func(r chi.Router) {
				r.Post("/postmark", newsletterH.WebhookPostmark)
				r.Post("/mailgun", newsletterH.WebhookMailgun)
				r.Post("/ses", newsletterH.WebhookSES)
				r.Post("/sendgrid", newsletterH.WebhookSendgrid)
			})
		}

		// UI routes — only when enabled (Inertia middleware registered above).
		if cfg.UIEnabled {
			// Customer routes
			r.Route("/tickets", func(r chi.Router) {
				r.Get("/", customerH.Index)
				r.Post("/", customerH.Create)
				r.Get("/{id}", customerH.Show)
				r.Post("/{id}/replies", customerH.Reply)
				r.Post("/{id}/rate", satH.Rate)
			})

			// Agent routes
			r.Route("/agent", func(r chi.Router) {
				r.Use(middleware.RequireAgent(cfg.AgentCheck))
				r.Get("/", agentH.Dashboard)
				r.Get("/tickets", agentH.ListTickets)
				r.Get("/tickets/{id}", agentH.ShowTicket)
				r.Post("/tickets/{id}/assign", agentH.AssignTicket)
				r.Post("/tickets/{id}/replies", agentH.Reply)
				r.Post("/tickets/{id}/status", agentH.ChangeStatus)
				r.Post("/tickets/{id}/subjects", subjectH.AttachSubject)
				r.Delete("/tickets/{id}/subjects/{subject}", subjectH.DetachSubject)
				r.Post("/tickets/{id}/actions/{action}", agentH.CustomAction)

				// Typed ticket-to-ticket links.
				r.Get("/tickets/{id}/links", linkH.List)
				r.Post("/tickets/{id}/links", linkH.Create)
				r.Delete("/tickets/{id}/links/{linkId}", linkH.Delete)

				// Side conversations (internal / email side-channel threads).
				r.Get("/tickets/{id}/side-conversations", scH.List)
				r.Post("/tickets/{id}/side-conversations", scH.Create)
				r.Post("/tickets/{id}/side-conversations/{scId}/reply", scH.Reply)
				r.Post("/tickets/{id}/side-conversations/{scId}/close", scH.Close)

				// Macros (agent-applied one-click bundles).
				r.Get("/macros", macroH.AgentList)
				r.Post("/tickets/{ticketId}/macros/{macroId}/apply", macroH.AgentApply)
			})

			// Admin routes
			r.Route("/admin", func(r chi.Router) {
				r.Use(middleware.RequireAdmin(cfg.AdminCheck))
				r.Get("/departments", adminH.ListDepartments)
				r.Post("/departments", adminH.CreateDepartment)
				r.Patch("/departments/{id}", adminH.UpdateDepartment)
				r.Delete("/departments/{id}", adminH.DeleteDepartment)
				r.Get("/tags", adminH.ListTags)
				r.Post("/tags", adminH.CreateTag)
				r.Delete("/tags/{id}", adminH.DeleteTag)
				r.Get("/sla-policies", adminH.ListSLAPolicies)
				r.Post("/sla-policies", adminH.CreateSLAPolicy)
				r.Delete("/sla-policies/{id}", adminH.DeleteSLAPolicy)

				// Data-retention purge (attachments + audit logs per policy).
				r.Post("/data-retention/purge", retentionH.Purge)

				// Event-driven admin Workflows (fire on ticket lifecycle events;
				// distinct from time-based Automations and agent-applied Macros —
				// see escalated-developer-context).
				r.Get("/workflows", workflowH.List)
				r.Post("/workflows", workflowH.Create)
				r.Patch("/workflows/{id}", workflowH.Update)
				r.Delete("/workflows/{id}", workflowH.Delete)
				r.Get("/workflows/{id}/logs", workflowH.Logs)

				// Time-based admin Automations (distinct from event-driven
				// Workflows and agent-applied Macros — see
				// escalated-developer-context).
				r.Get("/automations", autoH.List)
				r.Post("/automations", autoH.Create)
				r.Patch("/automations/{id}", autoH.Update)
				r.Delete("/automations/{id}", autoH.Delete)
				r.Post("/automations/run", autoH.Run)

				// Time-based admin Escalation rules.
				r.Get("/escalation-rules", escH.List)
				r.Post("/escalation-rules", escH.Create)
				r.Patch("/escalation-rules/{id}", escH.Update)
				r.Delete("/escalation-rules/{id}", escH.Delete)
				r.Post("/escalation-rules/run", escH.Run)

				// Per-agent ticket capacity (load-aware assignment).
				r.Get("/capacity", capH.List)
				r.Patch("/capacity/{id}", capH.Update)

				// Macros admin CRUD.
				r.Get("/macros", macroH.AdminList)
				r.Post("/macros", macroH.Create)
				r.Patch("/macros/{id}", macroH.Update)
				r.Delete("/macros/{id}", macroH.Delete)

				// Outbound webhooks admin CRUD + per-delivery retry.
				r.Get("/webhooks", webhookH.List)
				r.Post("/webhooks", webhookH.Create)
				r.Patch("/webhooks/{id}", webhookH.Update)
				r.Delete("/webhooks/{id}", webhookH.Delete)
				r.Get("/webhooks/{id}/deliveries", webhookH.Deliveries)
				r.Post("/webhooks/deliveries/{id}/retry", webhookH.Retry)

				// Skills admin — register /skills/new before /skills/{id}/edit.
				r.Get("/skills", skillsH.ListSkills)
				r.Get("/skills/new", skillsH.NewSkillForm)
				r.Post("/skills", skillsH.StoreSkill)
				r.Get("/skills/{id}/edit", skillsH.EditSkill)
				r.Put("/skills/{id}", skillsH.UpdateSkill)
				r.Patch("/skills/{id}", skillsH.UpdateSkill)
				r.Delete("/skills/{id}", skillsH.DestroySkill)

				r.Get("/settings/public-tickets", adminH.GetPublicTicketsSettings)
				r.Put("/settings/public-tickets", adminH.UpdatePublicTicketsSettings)

				// Users (host User table: list + grant/revoke admin/agent).
				r.Get("/users", userH.Index)
				r.Patch("/users/{user}/role", userH.UpdateRole)

				if cfg.EnableNewsletters {
					r.Route("/newsletters", func(r chi.Router) {
						r.Get("/", newsletterH.CampaignIndex)
						r.Get("/new", newsletterH.CampaignCreate)
						r.Post("/", newsletterH.CampaignStore)
						r.Post("/preview", newsletterH.CampaignPreview)
						r.Post("/test", newsletterH.CampaignTestSend)
						r.Get("/lists", newsletterH.ListIndex)
						r.Get("/lists/new", newsletterH.ListCreate)
						r.Post("/lists", newsletterH.ListStore)
						r.Get("/lists/{list}", newsletterH.ListShow)
						r.Put("/lists/{list}", newsletterH.ListUpdate)
						r.Delete("/lists/{list}", newsletterH.ListDestroy)
						r.Post("/lists/{list}/members", newsletterH.ListAddMember)
						r.Delete("/lists/{list}/members/{contactId}", newsletterH.ListRemoveMember)
						r.Post("/lists/{list}/import", newsletterH.ListImportCSV)
						r.Get("/templates", newsletterH.TemplateIndex)
						r.Get("/templates/new", newsletterH.TemplateCreate)
						r.Post("/templates", newsletterH.TemplateStore)
						r.Get("/templates/{template}", newsletterH.TemplateShow)
						r.Put("/templates/{template}", newsletterH.TemplateUpdate)
						r.Delete("/templates/{template}", newsletterH.TemplateDestroy)
						r.Get("/settings", newsletterH.SettingsShow)
						r.Put("/settings", newsletterH.SettingsUpdate)
						r.Get("/{newsletter}", newsletterH.CampaignShow)
						r.Get("/{newsletter}/edit", newsletterH.CampaignEdit)
						r.Put("/{newsletter}", newsletterH.CampaignUpdate)
						r.Delete("/{newsletter}", newsletterH.CampaignDestroy)
					})
				}
			})
		}
	})
}
