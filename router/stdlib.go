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
	subjectSvc := services.NewTicketSubjectService(s, cfg.TicketSubjectTypes, cfg.TicketSubjectResolver)

	// Outbound webhooks: the dispatcher is shared between the ticket service
	// (which fires lifecycle events) and the admin CRUD handler.
	webhookDispatcher := services.NewWebhookDispatcher(cfg.DB, nil)
	ticketSvc.Webhooks = webhookDispatcher

	// Event-driven Workflows: the runner fires active workflows on the ticket
	// lifecycle (created/replied/status-changed/assigned) off the request path,
	// mirroring the webhook dispatcher. The admin CRUD handler manages the rows.
	ticketSvc.Workflows = services.NewWorkflowRunner(cfg.DB, nil)

	apiH := handlers.NewAPIHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	apiH.Subjects = subjectSvc
	subjectH := handlers.NewTicketSubjectHandler(subjectSvc, ticketSvc)
	agentH := handlers.NewAgentHandler(s, ticketSvc, assignSvc, rend, cfg.UserIDFunc)
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
	cannedH := handlers.NewCannedResponseHandler(cfg.DB, services.NewCannedResponseService(cfg.DB, nil))
	articleH := handlers.NewArticleHandler(cfg.DB, services.NewArticleService(cfg.DB, nil))
	webhookH := handlers.NewWebhookHandler(cfg.DB, webhookDispatcher)
	workflowH := handlers.NewWorkflowHandler(cfg.DB)
	userH := handlers.NewUserHandler(cfg.UserDirectory, rend, cfg.UserIDFunc)
	skillsH := handlers.NewSkillsHandler(cfg.DB, cfg.TablePrefix, rend, cfg.SkillAgentDirectory)
	reportH := handlers.NewReportHandler(cfg.DB, cfg.TablePrefix)
	var newsletterH *handlers.NewsletterHandler
	if cfg.EnableNewsletters {
		newsletterH, _ = newNewsletterStack(esc)
	}

	prefix := cfg.RoutePrefix

	// Attachment downloads — always mounted
	mux.HandleFunc("GET "+prefix+"/attachments/{id}/download", attachH.Download)

	// JSON API — always mounted
	mux.HandleFunc("GET "+prefix+"/api/tickets", apiH.ListTickets)
	mux.HandleFunc("POST "+prefix+"/api/tickets", apiH.CreateTicket)
	mux.HandleFunc("GET "+prefix+"/api/tickets/{id}", apiH.ShowTicket)
	mux.HandleFunc("PATCH "+prefix+"/api/tickets/{id}", apiH.UpdateTicket)
	mux.HandleFunc("POST "+prefix+"/api/tickets/{id}/replies", apiH.CreateReply)
	mux.HandleFunc("POST "+prefix+"/api/tickets/{id}/subjects", subjectH.AttachSubject)
	mux.HandleFunc("DELETE "+prefix+"/api/tickets/{id}/subjects/{subject}", subjectH.DetachSubject)
	mux.HandleFunc("GET "+prefix+"/api/departments", apiH.ListDepartments)
	mux.HandleFunc("GET "+prefix+"/api/tags", apiH.ListTags)

	// Authentication — delegated to host-app callbacks (handlers.APIAuth).
	mux.HandleFunc("POST "+prefix+"/api/auth/login", authH.Login)
	mux.HandleFunc("POST "+prefix+"/api/auth/register", authH.Register)
	mux.HandleFunc("POST "+prefix+"/api/auth/logout", authH.Logout)
	mux.HandleFunc("POST "+prefix+"/api/auth/refresh", authH.Refresh)
	mux.HandleFunc("GET "+prefix+"/api/auth/me", authH.Me)
	mux.HandleFunc("PATCH "+prefix+"/api/auth/profile", authH.Profile)
	mux.HandleFunc("POST "+prefix+"/api/auth/validate", authH.Validate)

	// Anonymous (guest) ticket submission + lookup by token.
	mux.HandleFunc("POST "+prefix+"/api/guest/tickets", guestH.Create)
	mux.HandleFunc("GET "+prefix+"/api/guest/tickets/{token}", guestH.Show)
	mux.HandleFunc("POST "+prefix+"/api/guest/tickets/{token}/rate", satH.GuestRate)

	// Public knowledge base (published only).
	mux.HandleFunc("GET "+prefix+"/api/kb/articles", kbH.ListArticles)
	mux.HandleFunc("GET "+prefix+"/api/kb/categories", kbH.ListCategories)
	mux.HandleFunc("GET "+prefix+"/api/kb/articles/{slug}", kbH.ShowArticle)

	if cfg.EnableNewsletters {
		mux.HandleFunc("GET "+prefix+"/n/o/{token}", newsletterH.OpenPixel)
		mux.HandleFunc("GET "+prefix+"/n/c/{token}", newsletterH.Click)
		mux.HandleFunc("GET "+prefix+"/n/u/{token}", newsletterH.UnsubscribeShow)
		mux.HandleFunc("POST "+prefix+"/n/u/{token}", newsletterH.UnsubscribeStore)
		mux.HandleFunc("GET "+prefix+"/n/v/{token}", newsletterH.ViewInBrowser)
		mux.HandleFunc("POST "+prefix+"/webhooks/newsletter/postmark", newsletterH.WebhookPostmark)
		mux.HandleFunc("POST "+prefix+"/webhooks/newsletter/mailgun", newsletterH.WebhookMailgun)
		mux.HandleFunc("POST "+prefix+"/webhooks/newsletter/ses", newsletterH.WebhookSES)
		mux.HandleFunc("POST "+prefix+"/webhooks/newsletter/sendgrid", newsletterH.WebhookSendgrid)
	}

	if cfg.UIEnabled {
		// Customer routes
		mux.HandleFunc("GET "+prefix+"/tickets", customerH.Index)
		mux.HandleFunc("POST "+prefix+"/tickets", customerH.Create)
		mux.HandleFunc("GET "+prefix+"/tickets/{id}", customerH.Show)
		mux.HandleFunc("POST "+prefix+"/tickets/{id}/replies", customerH.Reply)
		mux.HandleFunc("POST "+prefix+"/tickets/{id}/rate", satH.Rate)

		// Agent routes (wrapped with agent middleware)
		agentMW := middleware.RequireAgent(cfg.AgentCheck)
		mux.Handle("GET "+prefix+"/agent/", agentMW(http.HandlerFunc(agentH.Dashboard)))
		mux.Handle("GET "+prefix+"/agent/tickets", agentMW(http.HandlerFunc(agentH.ListTickets)))
		mux.Handle("GET "+prefix+"/agent/tickets/{id}", agentMW(http.HandlerFunc(agentH.ShowTicket)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/assign", agentMW(http.HandlerFunc(agentH.AssignTicket)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/replies", agentMW(http.HandlerFunc(agentH.Reply)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/status", agentMW(http.HandlerFunc(agentH.ChangeStatus)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/subjects", agentMW(http.HandlerFunc(subjectH.AttachSubject)))
		mux.Handle("DELETE "+prefix+"/agent/tickets/{id}/subjects/{subject}", agentMW(http.HandlerFunc(subjectH.DetachSubject)))
		mux.Handle("GET "+prefix+"/agent/tickets/{id}/links", agentMW(http.HandlerFunc(linkH.List)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/links", agentMW(http.HandlerFunc(linkH.Create)))
		mux.Handle("DELETE "+prefix+"/agent/tickets/{id}/links/{linkId}", agentMW(http.HandlerFunc(linkH.Delete)))
		mux.Handle("GET "+prefix+"/agent/tickets/{id}/side-conversations", agentMW(http.HandlerFunc(scH.List)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/side-conversations", agentMW(http.HandlerFunc(scH.Create)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/side-conversations/{scId}/reply", agentMW(http.HandlerFunc(scH.Reply)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/side-conversations/{scId}/close", agentMW(http.HandlerFunc(scH.Close)))

		// Macros (agent-applied one-click bundles).
		mux.Handle("GET "+prefix+"/agent/macros", agentMW(http.HandlerFunc(macroH.AgentList)))
		mux.Handle("POST "+prefix+"/agent/tickets/{ticketId}/macros/{macroId}/apply", agentMW(http.HandlerFunc(macroH.AgentApply)))

		// Canned responses visible to this agent (shared + own).
		mux.Handle("GET "+prefix+"/agent/canned-responses", agentMW(http.HandlerFunc(cannedH.AgentList)))

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
		mux.Handle("POST "+prefix+"/admin/data-retention/purge", adminMW(http.HandlerFunc(retentionH.Purge)))

		// Event-driven admin Workflows (fire on ticket lifecycle events).
		mux.Handle("GET "+prefix+"/admin/workflows", adminMW(http.HandlerFunc(workflowH.List)))
		mux.Handle("POST "+prefix+"/admin/workflows", adminMW(http.HandlerFunc(workflowH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/workflows/{id}", adminMW(http.HandlerFunc(workflowH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/workflows/{id}", adminMW(http.HandlerFunc(workflowH.Delete)))
		mux.Handle("GET "+prefix+"/admin/workflows/{id}/logs", adminMW(http.HandlerFunc(workflowH.Logs)))

		// Time-based admin Automations.
		mux.Handle("GET "+prefix+"/admin/automations", adminMW(http.HandlerFunc(autoH.List)))
		mux.Handle("POST "+prefix+"/admin/automations", adminMW(http.HandlerFunc(autoH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Delete)))
		mux.Handle("POST "+prefix+"/admin/automations/run", adminMW(http.HandlerFunc(autoH.Run)))

		// Time-based admin Escalation rules.
		mux.Handle("GET "+prefix+"/admin/escalation-rules", adminMW(http.HandlerFunc(escH.List)))
		mux.Handle("POST "+prefix+"/admin/escalation-rules", adminMW(http.HandlerFunc(escH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/escalation-rules/{id}", adminMW(http.HandlerFunc(escH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/escalation-rules/{id}", adminMW(http.HandlerFunc(escH.Delete)))
		mux.Handle("POST "+prefix+"/admin/escalation-rules/run", adminMW(http.HandlerFunc(escH.Run)))

		// Per-agent ticket capacity (load-aware assignment).
		mux.Handle("GET "+prefix+"/admin/capacity", adminMW(http.HandlerFunc(capH.List)))
		mux.Handle("PATCH "+prefix+"/admin/capacity/{id}", adminMW(http.HandlerFunc(capH.Update)))

		// Macros admin CRUD.
		mux.Handle("GET "+prefix+"/admin/macros", adminMW(http.HandlerFunc(macroH.AdminList)))
		mux.Handle("POST "+prefix+"/admin/macros", adminMW(http.HandlerFunc(macroH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Delete)))

		// Canned responses admin CRUD.
		mux.Handle("GET "+prefix+"/admin/canned-responses", adminMW(http.HandlerFunc(cannedH.AdminList)))
		mux.Handle("POST "+prefix+"/admin/canned-responses", adminMW(http.HandlerFunc(cannedH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/canned-responses/{id}", adminMW(http.HandlerFunc(cannedH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/canned-responses/{id}", adminMW(http.HandlerFunc(cannedH.Delete)))

		// Knowledge-base authoring admin CRUD (articles + categories). The
		// public read path is /api/kb/... (published only); this is the admin
		// authoring counterpart.
		mux.Handle("GET "+prefix+"/admin/kb/articles", adminMW(http.HandlerFunc(articleH.AdminList)))
		mux.Handle("POST "+prefix+"/admin/kb/articles", adminMW(http.HandlerFunc(articleH.Create)))
		mux.Handle("GET "+prefix+"/admin/kb/articles/{id}", adminMW(http.HandlerFunc(articleH.Show)))
		mux.Handle("PATCH "+prefix+"/admin/kb/articles/{id}", adminMW(http.HandlerFunc(articleH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/kb/articles/{id}", adminMW(http.HandlerFunc(articleH.Delete)))
		mux.Handle("GET "+prefix+"/admin/kb/categories", adminMW(http.HandlerFunc(articleH.ListCategories)))
		mux.Handle("POST "+prefix+"/admin/kb/categories", adminMW(http.HandlerFunc(articleH.CreateCategory)))
		mux.Handle("PATCH "+prefix+"/admin/kb/categories/{id}", adminMW(http.HandlerFunc(articleH.UpdateCategory)))
		mux.Handle("DELETE "+prefix+"/admin/kb/categories/{id}", adminMW(http.HandlerFunc(articleH.DeleteCategory)))

		// Outbound webhooks admin CRUD + per-delivery retry.
		mux.Handle("GET "+prefix+"/admin/webhooks", adminMW(http.HandlerFunc(webhookH.List)))
		mux.Handle("POST "+prefix+"/admin/webhooks", adminMW(http.HandlerFunc(webhookH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/webhooks/{id}", adminMW(http.HandlerFunc(webhookH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/webhooks/{id}", adminMW(http.HandlerFunc(webhookH.Delete)))
		mux.Handle("GET "+prefix+"/admin/webhooks/{id}/deliveries", adminMW(http.HandlerFunc(webhookH.Deliveries)))
		mux.Handle("POST "+prefix+"/admin/webhooks/deliveries/{id}/retry", adminMW(http.HandlerFunc(webhookH.Retry)))

		// Skills admin (explicit routing + agent proficiency). Register
		// /skills/new before /skills/{id}/… so "new" is not parsed as an id.
		mux.Handle("GET "+prefix+"/admin/skills", adminMW(http.HandlerFunc(skillsH.ListSkills)))
		mux.Handle("GET "+prefix+"/admin/skills/new", adminMW(http.HandlerFunc(skillsH.NewSkillForm)))
		mux.Handle("POST "+prefix+"/admin/skills", adminMW(http.HandlerFunc(skillsH.StoreSkill)))
		mux.Handle("GET "+prefix+"/admin/skills/{id}/edit", adminMW(http.HandlerFunc(skillsH.EditSkill)))
		mux.Handle("PUT "+prefix+"/admin/skills/{id}", adminMW(http.HandlerFunc(skillsH.UpdateSkill)))
		mux.Handle("PATCH "+prefix+"/admin/skills/{id}", adminMW(http.HandlerFunc(skillsH.UpdateSkill)))
		mux.Handle("DELETE "+prefix+"/admin/skills/{id}", adminMW(http.HandlerFunc(skillsH.DestroySkill)))

		// Reporting analytics (read-only). Exposes the reporting service as
		// per-report JSON endpoints; ?days defaults to 30. Mirrors the Laravel
		// reference ReportController surface.
		mux.Handle("GET "+prefix+"/admin/reports", adminMW(http.HandlerFunc(reportH.Index)))
		mux.Handle("GET "+prefix+"/admin/reports/first-response-time", adminMW(http.HandlerFunc(reportH.FirstResponseTime)))
		mux.Handle("GET "+prefix+"/admin/reports/resolution-time", adminMW(http.HandlerFunc(reportH.ResolutionTime)))
		mux.Handle("GET "+prefix+"/admin/reports/agent-ranking", adminMW(http.HandlerFunc(reportH.AgentRanking)))
		mux.Handle("GET "+prefix+"/admin/reports/sla", adminMW(http.HandlerFunc(reportH.SLA)))
		mux.Handle("GET "+prefix+"/admin/reports/csat", adminMW(http.HandlerFunc(reportH.CSAT)))
		mux.Handle("GET "+prefix+"/admin/reports/period-comparison", adminMW(http.HandlerFunc(reportH.PeriodComparison)))

		mux.Handle("GET "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.GetPublicTicketsSettings)))
		mux.Handle("PUT "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.UpdatePublicTicketsSettings)))

		// Users (host User table: list + grant/revoke admin/agent).
		mux.Handle("GET "+prefix+"/admin/users", adminMW(http.HandlerFunc(userH.Index)))
		mux.Handle("PATCH "+prefix+"/admin/users/{user}/role", adminMW(http.HandlerFunc(userH.UpdateRole)))

		if cfg.EnableNewsletters {
			mux.Handle("GET "+prefix+"/admin/newsletters", adminMW(http.HandlerFunc(newsletterH.CampaignIndex)))
			mux.Handle("GET "+prefix+"/admin/newsletters/new", adminMW(http.HandlerFunc(newsletterH.CampaignCreate)))
			mux.Handle("POST "+prefix+"/admin/newsletters", adminMW(http.HandlerFunc(newsletterH.CampaignStore)))
			mux.Handle("POST "+prefix+"/admin/newsletters/preview", adminMW(http.HandlerFunc(newsletterH.CampaignPreview)))
			mux.Handle("POST "+prefix+"/admin/newsletters/test", adminMW(http.HandlerFunc(newsletterH.CampaignTestSend)))
			mux.Handle("GET "+prefix+"/admin/newsletters/lists", adminMW(http.HandlerFunc(newsletterH.ListIndex)))
			mux.Handle("GET "+prefix+"/admin/newsletters/lists/new", adminMW(http.HandlerFunc(newsletterH.ListCreate)))
			mux.Handle("POST "+prefix+"/admin/newsletters/lists", adminMW(http.HandlerFunc(newsletterH.ListStore)))
			mux.Handle("GET "+prefix+"/admin/newsletters/lists/{list}", adminMW(http.HandlerFunc(newsletterH.ListShow)))
			mux.Handle("PUT "+prefix+"/admin/newsletters/lists/{list}", adminMW(http.HandlerFunc(newsletterH.ListUpdate)))
			mux.Handle("DELETE "+prefix+"/admin/newsletters/lists/{list}", adminMW(http.HandlerFunc(newsletterH.ListDestroy)))
			mux.Handle("POST "+prefix+"/admin/newsletters/lists/{list}/members", adminMW(http.HandlerFunc(newsletterH.ListAddMember)))
			mux.Handle("DELETE "+prefix+"/admin/newsletters/lists/{list}/members/{contactId}", adminMW(http.HandlerFunc(newsletterH.ListRemoveMember)))
			mux.Handle("POST "+prefix+"/admin/newsletters/lists/{list}/import", adminMW(http.HandlerFunc(newsletterH.ListImportCSV)))
			mux.Handle("GET "+prefix+"/admin/newsletters/templates", adminMW(http.HandlerFunc(newsletterH.TemplateIndex)))
			mux.Handle("GET "+prefix+"/admin/newsletters/templates/new", adminMW(http.HandlerFunc(newsletterH.TemplateCreate)))
			mux.Handle("POST "+prefix+"/admin/newsletters/templates", adminMW(http.HandlerFunc(newsletterH.TemplateStore)))
			mux.Handle("GET "+prefix+"/admin/newsletters/templates/{template}", adminMW(http.HandlerFunc(newsletterH.TemplateShow)))
			mux.Handle("PUT "+prefix+"/admin/newsletters/templates/{template}", adminMW(http.HandlerFunc(newsletterH.TemplateUpdate)))
			mux.Handle("DELETE "+prefix+"/admin/newsletters/templates/{template}", adminMW(http.HandlerFunc(newsletterH.TemplateDestroy)))
			mux.Handle("GET "+prefix+"/admin/newsletters/settings", adminMW(http.HandlerFunc(newsletterH.SettingsShow)))
			mux.Handle("PUT "+prefix+"/admin/newsletters/settings", adminMW(http.HandlerFunc(newsletterH.SettingsUpdate)))
			mux.Handle("GET "+prefix+"/admin/newsletters/{newsletter}", adminMW(http.HandlerFunc(newsletterH.CampaignShow)))
			mux.Handle("GET "+prefix+"/admin/newsletters/{newsletter}/edit", adminMW(http.HandlerFunc(newsletterH.CampaignEdit)))
			mux.Handle("PUT "+prefix+"/admin/newsletters/{newsletter}", adminMW(http.HandlerFunc(newsletterH.CampaignUpdate)))
			mux.Handle("DELETE "+prefix+"/admin/newsletters/{newsletter}", adminMW(http.HandlerFunc(newsletterH.CampaignDestroy)))
		}
	}
}
