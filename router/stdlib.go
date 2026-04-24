package router

import (
	"net/http"

	escalated "github.com/escalated-dev/escalated-go"
	"github.com/escalated-dev/escalated-go/handlers"
	"github.com/escalated-dev/escalated-go/middleware"
	"github.com/escalated-dev/escalated-go/services"
)

// MountStdlib mounts all Escalated routes on a standard library ServeMux.
// Requires Go 1.22+ for method-based routing patterns.
func MountStdlib(mux *http.ServeMux, esc *escalated.Escalated) {
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

	prefix := cfg.RoutePrefix

	// Attachment downloads — always mounted
	mux.HandleFunc("GET "+prefix+"/attachments/{id}/download", attachH.Download)

	// JSON API — always mounted
	mux.HandleFunc("GET "+prefix+"/api/tickets", apiH.ListTickets)
	mux.HandleFunc("POST "+prefix+"/api/tickets", apiH.CreateTicket)
	mux.HandleFunc("GET "+prefix+"/api/tickets/{id}", apiH.ShowTicket)
	mux.HandleFunc("PATCH "+prefix+"/api/tickets/{id}", apiH.UpdateTicket)
	mux.HandleFunc("POST "+prefix+"/api/tickets/{id}/replies", apiH.CreateReply)
	mux.HandleFunc("GET "+prefix+"/api/departments", apiH.ListDepartments)
	mux.HandleFunc("GET "+prefix+"/api/tags", apiH.ListTags)

	if cfg.UIEnabled {
		// Customer routes
		mux.HandleFunc("GET "+prefix+"/tickets", customerH.Index)
		mux.HandleFunc("POST "+prefix+"/tickets", customerH.Create)
		mux.HandleFunc("GET "+prefix+"/tickets/{id}", customerH.Show)
		mux.HandleFunc("POST "+prefix+"/tickets/{id}/replies", customerH.Reply)

		// Agent routes (wrapped with agent middleware)
		agentMW := middleware.RequireAgent(cfg.AgentCheck)
		mux.Handle("GET "+prefix+"/agent/", agentMW(http.HandlerFunc(agentH.Dashboard)))
		mux.Handle("GET "+prefix+"/agent/tickets", agentMW(http.HandlerFunc(agentH.ListTickets)))
		mux.Handle("GET "+prefix+"/agent/tickets/{id}", agentMW(http.HandlerFunc(agentH.ShowTicket)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/assign", agentMW(http.HandlerFunc(agentH.AssignTicket)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/replies", agentMW(http.HandlerFunc(agentH.Reply)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/status", agentMW(http.HandlerFunc(agentH.ChangeStatus)))

		// Macros (agent-applied one-click bundles).
		mux.Handle("GET "+prefix+"/agent/macros", agentMW(http.HandlerFunc(macroH.AgentList)))
		mux.Handle("POST "+prefix+"/agent/tickets/{ticketId}/macros/{macroId}/apply", agentMW(http.HandlerFunc(macroH.AgentApply)))

		// Admin routes (wrapped with admin middleware)
		adminMW := middleware.RequireAdmin(cfg.AdminCheck)
		mux.Handle("GET "+prefix+"/admin/departments", adminMW(http.HandlerFunc(adminH.ListDepartments)))
		mux.Handle("POST "+prefix+"/admin/departments", adminMW(http.HandlerFunc(adminH.CreateDepartment)))
		mux.Handle("PATCH "+prefix+"/admin/departments/{id}", adminMW(http.HandlerFunc(adminH.UpdateDepartment)))
		mux.Handle("DELETE "+prefix+"/admin/departments/{id}", adminMW(http.HandlerFunc(adminH.DeleteDepartment)))
		mux.Handle("GET "+prefix+"/admin/tags", adminMW(http.HandlerFunc(adminH.ListTags)))
		mux.Handle("POST "+prefix+"/admin/tags", adminMW(http.HandlerFunc(adminH.CreateTag)))
		mux.Handle("DELETE "+prefix+"/admin/tags/{id}", adminMW(http.HandlerFunc(adminH.DeleteTag)))
		mux.Handle("GET "+prefix+"/admin/sla-policies", adminMW(http.HandlerFunc(adminH.ListSLAPolicies)))
		mux.Handle("POST "+prefix+"/admin/sla-policies", adminMW(http.HandlerFunc(adminH.CreateSLAPolicy)))
		mux.Handle("DELETE "+prefix+"/admin/sla-policies/{id}", adminMW(http.HandlerFunc(adminH.DeleteSLAPolicy)))

		// Time-based admin Automations.
		mux.Handle("GET "+prefix+"/admin/automations", adminMW(http.HandlerFunc(autoH.List)))
		mux.Handle("POST "+prefix+"/admin/automations", adminMW(http.HandlerFunc(autoH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Delete)))
		mux.Handle("POST "+prefix+"/admin/automations/run", adminMW(http.HandlerFunc(autoH.Run)))

		// Macros admin CRUD.
		mux.Handle("GET "+prefix+"/admin/macros", adminMW(http.HandlerFunc(macroH.AdminList)))
		mux.Handle("POST "+prefix+"/admin/macros", adminMW(http.HandlerFunc(macroH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Delete)))

		mux.Handle("GET "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.GetPublicTicketsSettings)))
		mux.Handle("PUT "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.UpdatePublicTicketsSettings)))
	}
}
