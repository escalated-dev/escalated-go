// Package router provides helpers to mount Escalated routes on popular Go routers.
package router

import (
	"github.com/go-chi/chi/v5"

	escalated "github.com/escalated-dev/escalated-go"
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

	apiH := handlers.NewAPIHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	agentH := handlers.NewAgentHandler(s, ticketSvc, assignSvc, rend, cfg.UserIDFunc)
	customerH := handlers.NewCustomerHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	adminH := handlers.NewAdminHandler(s, rend)
	attachH := handlers.NewAttachmentHandler(s, cfg.RoutePrefix)
	autoH := handlers.NewAutomationHandler(cfg.DB, services.NewAutomationRunner(cfg.DB, nil))
	macroH := handlers.NewMacroHandler(cfg.DB, services.NewMacroService(cfg.DB, nil))

	r.Route(cfg.RoutePrefix, func(r chi.Router) {
		// Attachment downloads — always mounted
		r.Get("/attachments/{id}/download", attachH.Download)

		// JSON API — always mounted
		r.Route("/api", func(r chi.Router) {
			r.Get("/tickets", apiH.ListTickets)
			r.Post("/tickets", apiH.CreateTicket)
			r.Get("/tickets/{id}", apiH.ShowTicket)
			r.Patch("/tickets/{id}", apiH.UpdateTicket)
			r.Post("/tickets/{id}/replies", apiH.CreateReply)
			r.Get("/departments", apiH.ListDepartments)
			r.Get("/tags", apiH.ListTags)
		})

		// UI routes — only when enabled
		if cfg.UIEnabled {
			r.Use(middleware.Inertia(""))

			// Customer routes
			r.Route("/tickets", func(r chi.Router) {
				r.Get("/", customerH.Index)
				r.Post("/", customerH.Create)
				r.Get("/{id}", customerH.Show)
				r.Post("/{id}/replies", customerH.Reply)
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

				// Time-based admin Automations (distinct from event-driven
				// Workflows and agent-applied Macros — see
				// escalated-developer-context).
				r.Get("/automations", autoH.List)
				r.Post("/automations", autoH.Create)
				r.Patch("/automations/{id}", autoH.Update)
				r.Delete("/automations/{id}", autoH.Delete)
				r.Post("/automations/run", autoH.Run)

				// Macros admin CRUD.
				r.Get("/macros", macroH.AdminList)
				r.Post("/macros", macroH.Create)
				r.Patch("/macros/{id}", macroH.Update)
				r.Delete("/macros/{id}", macroH.Delete)
			})
		}
	})
}
