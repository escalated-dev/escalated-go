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
//
// With Config.EnableWidget it also mounts the support widget under
// {RoutePrefix}/widget: embed.js, and every other widget route behind
// handlers.WidgetCORS (outside the ticket-access middleware).
func MountStdlib(mux *http.ServeMux, esc *escalated.Escalated) {
	inner := http.NewServeMux()
	ticketSvc := mountStdlib(inner, esc)
	prefix := esc.Config.RoutePrefix
	mux.Handle(prefix+"/", esc.TicketAccessMiddleware(inner))

	if esc.Config.EnableWidget {
		stack := newWidgetStack(esc, ticketSvc)
		widget := http.NewServeMux()
		for _, route := range stack.routes {
			widget.HandleFunc(route.method+" "+prefix+"/widget"+route.path, route.handler)
		}
		mux.HandleFunc("GET "+prefix+"/widget/embed.js", handlers.WidgetScript)
		mux.Handle(prefix+"/widget/", stack.wrap(widget))
	}
}

func mountStdlib(mux *http.ServeMux, esc *escalated.Escalated) *services.TicketService {
	cfg := esc.Config
	s := esc.Store
	rend := esc.Renderer

	// The ticket service fires lifecycle events to outbound webhooks and
	// event-driven Workflows (see Escalated.NewTicketService).
	ticketSvc := esc.NewTicketService()
	assignSvc := services.NewAssignmentService(s)
	assignSvc.Tickets = ticketSvc
	mergeH := &handlers.TicketMergeHandler{
		Merges: &services.TicketMergeService{DB: cfg.DB, Prefix: cfg.TablePrefix, Tickets: ticketSvc},
		Store:  s,
		UserID: cfg.UserIDFunc,
	}
	subjectSvc := services.NewTicketSubjectService(s, cfg.TicketSubjectTypes, cfg.TicketSubjectResolver)

	// The dispatcher is shared between the ticket service and the admin CRUD
	// handler.
	webhookDispatcher := ticketSvc.Webhooks

	apiH := handlers.NewAPIHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	apiH.Subjects = subjectSvc
	apiH.CustomFields = customFieldService(esc)
	subjectH := handlers.NewTicketSubjectHandler(subjectSvc, ticketSvc)
	agentH := handlers.NewAgentHandler(s, ticketSvc, assignSvc, rend, cfg.UserIDFunc)
	customerH := handlers.NewCustomerHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	adminH := handlers.NewAdminHandler(s, rend)
	// Audit log: admin changes are recorded and listed at /admin/audit-log.
	auditStore := &services.SQLAuditLogStore{DB: cfg.DB, Prefix: cfg.TablePrefix}
	adminH.Audit = services.NewAuditLogService(auditStore)
	adminH.UserID = cfg.UserIDFunc
	auditH := &handlers.AuditLogHandler{Store: auditStore, Renderer: rend, Users: cfg.UserDirectory}
	// Agent live-chat console (accept, message, end).
	chatSvc := services.NewChatSessionService(s, services.NewChatRoutingService(s), nil)
	chatSvc.Tickets = ticketSvc
	chatH := handlers.NewChatHandler(s, chatSvc, cfg.UserIDFunc)
	attachH := handlers.NewAttachmentHandler(s, cfg.RoutePrefix)
	attachH.AgentCheck = cfg.AgentCheck
	attachH.AdminCheck = cfg.AdminCheck
	attachH.UserID = cfg.UserIDFunc
	autoRunner := services.NewAutomationRunner(cfg.DB, nil, esc.Access)
	autoRunner.Tickets = ticketSvc
	autoH := handlers.NewAutomationHandler(cfg.DB, autoRunner)
	escSvc := services.NewEscalationService(cfg.DB, nil, esc.Access)
	escSvc.Tickets = ticketSvc
	escH := handlers.NewEscalationHandler(cfg.DB, escSvc)
	satH := handlers.NewSatisfactionHandler(cfg.DB)
	satH.UserID = cfg.UserIDFunc
	capH := handlers.NewCapacityHandler(cfg.DB)
	linkH := handlers.NewTicketLinkHandler(cfg.DB)
	scH := handlers.NewSideConversationHandler(cfg.DB)
	authH := handlers.NewAuthHandler(cfg.APIAuth)
	guestH := handlers.NewGuestTicketHandler(s, ticketSvc)
	// Per-client-IP limits on the unauthenticated guest endpoints.
	guestLimit := middleware.NewGuestRateLimiter(cfg.GuestRateLimit)
	guestH.Access = esc.GuestAccess
	satH.GuestTicketID = esc.GuestAccess.ResolveTicketID
	guestVerifyH := &handlers.GuestVerificationHandler{Access: esc.GuestAccess, ClientIP: guestLimit.ClientIP}
	kbH := handlers.NewKBHandler(cfg.DB)
	kbGuard := kbPublicGuard(esc)
	retentionH := handlers.NewRetentionHandler(services.NewRetentionService(cfg.DB, s).WithPrefix(cfg.TablePrefix))
	macroSvc := services.NewMacroService(cfg.DB, nil, esc.Access)
	macroSvc.Tickets = ticketSvc
	macroH := handlers.NewMacroHandler(cfg.DB, macroSvc)
	cannedSvc := services.NewCannedResponseService(cfg.DB, nil)
	cannedH := handlers.NewCannedResponseHandler(cfg.DB, cannedSvc)
	articleH := handlers.NewArticleHandler(cfg.DB, services.NewArticleService(cfg.DB, nil))
	webhookH := handlers.NewWebhookHandler(cfg.DB, webhookDispatcher)
	kbPages := &handlers.KBPages{DB: cfg.DB, Store: s}
	guestPages := &handlers.GuestPages{Store: s, Tickets: ticketSvc, Access: esc.GuestAccess, DB: cfg.DB, Prefix: cfg.TablePrefix}
	workflowH := handlers.NewWorkflowHandler(cfg.DB)
	userH := handlers.NewUserHandler(cfg.UserDirectory, rend, cfg.UserIDFunc)
	skillsH := handlers.NewSkillsHandler(cfg.DB, cfg.TablePrefix, rend, cfg.SkillAgentDirectory)
	reportH := handlers.NewReportHandler(cfg.DB, cfg.TablePrefix)
	wireUIPages(esc, agentH, customerH, adminH, uiPageDeps{
		subjects: subjectSvc, macros: macroSvc, canned: cannedSvc,
		escalations: escH, macroAdmin: macroH, cannedAdmin: cannedH, webhooks: webhookH,
		articles: articleH, kbPages: kbPages, guestPages: guestPages, workflows: workflowH, reports: reportH, automations: autoH, capacity: capH,
	})
	var newsletterH *handlers.NewsletterHandler
	if cfg.EnableNewsletters {
		newsletterH, _ = newNewsletterStack(esc)
	}

	prefix := cfg.RoutePrefix

	// Attachment downloads — always mounted. The handler authorizes each
	// download through the attachment's ticket (see AttachmentHandler).
	mux.HandleFunc("GET "+prefix+"/attachments/{id}/download", attachH.Download)

	// JSON API — always mounted. Tickets, departments and tags are agent-level,
	// like /agent: these routes list every ticket, return internal notes and
	// change status and priority. The auth, guest and knowledge-base routes
	// below stay public.
	apiMW := middleware.RequireAgentOrAdmin(cfg.AgentCheck, cfg.AdminCheck)
	mux.Handle("GET "+prefix+"/api/tickets", apiMW(http.HandlerFunc(apiH.ListTickets)))
	mux.Handle("POST "+prefix+"/api/tickets", apiMW(http.HandlerFunc(apiH.CreateTicket)))
	mux.Handle("GET "+prefix+"/api/tickets/{id}", apiMW(http.HandlerFunc(apiH.ShowTicket)))
	mux.Handle("PATCH "+prefix+"/api/tickets/{id}", apiMW(http.HandlerFunc(apiH.UpdateTicket)))
	mux.Handle("POST "+prefix+"/api/tickets/{id}/replies", apiMW(http.HandlerFunc(apiH.CreateReply)))
	mux.Handle("POST "+prefix+"/api/tickets/{id}/subjects", apiMW(http.HandlerFunc(subjectH.AttachSubject)))
	mux.Handle("DELETE "+prefix+"/api/tickets/{id}/subjects/{subject}", apiMW(http.HandlerFunc(subjectH.DetachSubject)))
	mux.Handle("GET "+prefix+"/api/departments", apiMW(http.HandlerFunc(apiH.ListDepartments)))
	mux.Handle("GET "+prefix+"/api/tags", apiMW(http.HandlerFunc(apiH.ListTags)))

	// Authentication — delegated to host-app callbacks (handlers.APIAuth).
	mux.HandleFunc("POST "+prefix+"/api/auth/login", authH.Login)
	mux.HandleFunc("POST "+prefix+"/api/auth/register", authH.Register)
	mux.HandleFunc("POST "+prefix+"/api/auth/logout", authH.Logout)
	mux.HandleFunc("POST "+prefix+"/api/auth/refresh", authH.Refresh)
	mux.HandleFunc("GET "+prefix+"/api/auth/me", authH.Me)
	mux.HandleFunc("PATCH "+prefix+"/api/auth/profile", authH.Profile)
	mux.HandleFunc("POST "+prefix+"/api/auth/validate", authH.Validate)

	// Anonymous (guest) ticket submission + lookup by token.
	// Creating a ticket needs an email code from /api/guest/verification;
	// {token} is the expiring grant it returns (docs/guest-access.md).
	mux.Handle("POST "+prefix+"/api/guest/verification", guestLimit.Middleware(middleware.GuestTicketScope)(http.HandlerFunc(guestVerifyH.Request)))
	mux.Handle("POST "+prefix+"/api/guest/lookup", guestLimit.Middleware(middleware.GuestReplyScope)(http.HandlerFunc(guestVerifyH.Lookup)))
	mux.Handle("POST "+prefix+"/api/guest/tickets", guestLimit.Middleware(middleware.GuestTicketScope)(http.HandlerFunc(guestH.Create)))
	mux.Handle("GET "+prefix+"/api/guest/tickets/{token}", guestLimit.Middleware(middleware.GuestRequestScope)(http.HandlerFunc(guestH.Show)))
	mux.Handle("POST "+prefix+"/api/guest/tickets/{token}/replies", guestLimit.Middleware(middleware.GuestReplyScope)(http.HandlerFunc(guestH.Reply)))
	mux.Handle("POST "+prefix+"/api/guest/tickets/{token}/rate", guestLimit.Middleware(middleware.GuestReplyScope)(http.HandlerFunc(satH.GuestRate)))

	// Public knowledge base (published only).
	mux.Handle("GET "+prefix+"/api/kb/articles", kbGuard(http.HandlerFunc(kbH.ListArticles)))
	mux.Handle("GET "+prefix+"/api/kb/categories", kbGuard(http.HandlerFunc(kbH.ListCategories)))
	mux.Handle("GET "+prefix+"/api/kb/articles/{slug}", kbGuard(http.HandlerFunc(kbH.ShowArticle)))

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
		// Customer routes — a signed-in user only. Show and Reply are further
		// limited to the ticket's requester in the handler.
		userMW := middleware.RequireUser(cfg.UserIDFunc)
		mux.Handle("GET "+prefix+"/tickets", userMW(http.HandlerFunc(customerH.Index)))
		mux.Handle("POST "+prefix+"/tickets", userMW(http.HandlerFunc(customerH.Create)))
		mux.Handle("GET "+prefix+"/tickets/create", userMW(http.HandlerFunc(customerH.CreateForm)))
		mux.Handle("GET "+prefix+"/tickets/{id}", userMW(http.HandlerFunc(customerH.Show)))
		mux.Handle("POST "+prefix+"/tickets/{id}/replies", userMW(http.HandlerFunc(customerH.Reply)))
		mux.Handle("POST "+prefix+"/tickets/{id}/rate", userMW(http.HandlerFunc(customerH.Rate)))
		mux.Handle("POST "+prefix+"/tickets/{id}/reply", userMW(http.HandlerFunc(customerH.Reply)))
		mux.Handle("POST "+prefix+"/tickets/{id}/close", userMW(http.HandlerFunc(customerH.Close)))
		mux.Handle("POST "+prefix+"/tickets/{id}/reopen", userMW(http.HandlerFunc(customerH.Reopen)))

		// The guest ticket pages (escalated.guest.tickets.*) on the verified
		// guest flow, behind the guest rate limits.
		mux.HandleFunc("GET "+prefix+"/guest/create", guestPages.Create)
		mux.Handle("POST "+prefix+"/guest", guestLimit.Middleware(middleware.GuestTicketScope)(http.HandlerFunc(guestPages.Submit)))
		mux.Handle("GET "+prefix+"/guest/{token}", guestLimit.Middleware(middleware.GuestRequestScope)(http.HandlerFunc(guestPages.Show)))
		mux.Handle("POST "+prefix+"/guest/{token}/reply", guestLimit.Middleware(middleware.GuestReplyScope)(http.HandlerFunc(guestPages.Reply)))
		mux.Handle("POST "+prefix+"/guest/{token}/rate", guestLimit.Middleware(middleware.GuestReplyScope)(http.HandlerFunc(guestPages.Rate)))

		// The knowledge base reader pages (escalated.customer.kb.*), behind the
		// knowledge base enabled and public settings.
		mux.Handle("GET "+prefix+"/kb", kbGuard(http.HandlerFunc(kbPages.Index)))
		mux.Handle("GET "+prefix+"/kb/{slug}", kbGuard(http.HandlerFunc(kbPages.Article)))
		mux.Handle("POST "+prefix+"/kb/{slug}/feedback", kbGuard(http.HandlerFunc(kbPages.Feedback)))

		// Agent routes (wrapped with agent middleware)
		agentMW := middleware.RequireAgent(cfg.AgentCheck)
		mux.Handle("GET "+prefix+"/agent/", agentMW(http.HandlerFunc(agentH.Dashboard)))
		mux.Handle("GET "+prefix+"/agent/tickets", agentMW(http.HandlerFunc(agentH.ListTickets)))
		mux.Handle("GET "+prefix+"/agent/tickets/{id}", agentMW(http.HandlerFunc(agentH.ShowTicket)))
		mux.Handle("POST "+prefix+"/agent/tickets/{id}/replies", agentMW(http.HandlerFunc(agentH.Reply)))
		mux.Handle("POST "+prefix+"/agent/tickets/bulk", agentMW(http.HandlerFunc(agentH.Bulk)))
		agentTickets := muxGroup{mux: mux, prefix: prefix + "/agent/tickets", mw: agentMW}
		mountTicketWrites(agentTickets, agentH)
		mountTicketPanels(agentTickets, s, subjectH, linkH, scH)

		// Macros (agent-applied one-click bundles).
		mux.Handle("GET "+prefix+"/agent/macros", agentMW(http.HandlerFunc(macroH.AgentList)))
		mux.Handle("POST "+prefix+"/agent/tickets/{ticketId}/macros/{macroId}/apply", agentMW(http.HandlerFunc(macroH.AgentApply)))

		// Canned responses visible to this agent (shared + own).
		mux.Handle("GET "+prefix+"/agent/canned-responses", agentMW(http.HandlerFunc(cannedH.AgentList)))

		// Live chat console: {id} is the chat ticket's id.
		mux.Handle("GET "+prefix+"/agent/chat/sessions", agentMW(http.HandlerFunc(chatH.ListSessions)))
		mux.Handle("POST "+prefix+"/agent/chat/sessions/{id}/accept", agentMW(http.HandlerFunc(chatH.AcceptSession)))
		mux.Handle("POST "+prefix+"/agent/chat/sessions/{id}/message", agentMW(http.HandlerFunc(chatH.SendMessage)))
		mux.Handle("POST "+prefix+"/agent/chat/sessions/{id}/end", agentMW(http.HandlerFunc(chatH.EndSession)))

		// Admin routes (wrapped with admin middleware)
		adminMW := middleware.RequireAdmin(cfg.AdminCheck)
		mux.Handle("GET "+prefix+"/admin/departments", adminMW(http.HandlerFunc(adminH.ListDepartments)))
		mux.Handle("POST "+prefix+"/admin/departments", adminMW(http.HandlerFunc(adminH.CreateDepartment)))
		mux.Handle("PATCH "+prefix+"/admin/departments/{id}", adminMW(http.HandlerFunc(adminH.UpdateDepartment)))
		mux.Handle("PUT "+prefix+"/admin/departments/{id}", adminMW(http.HandlerFunc(adminH.UpdateDepartment)))
		mux.Handle("GET "+prefix+"/admin/departments/create", adminMW(http.HandlerFunc(adminH.CreateDepartmentForm)))
		mux.Handle("GET "+prefix+"/admin/departments/{id}/edit", adminMW(http.HandlerFunc(adminH.EditDepartmentForm)))
		mux.Handle("DELETE "+prefix+"/admin/departments/{id}", adminMW(http.HandlerFunc(adminH.DeleteDepartment)))
		mux.Handle("GET "+prefix+"/admin/tags", adminMW(http.HandlerFunc(adminH.ListTags)))
		mux.Handle("POST "+prefix+"/admin/tags", adminMW(http.HandlerFunc(adminH.CreateTag)))
		mux.Handle("DELETE "+prefix+"/admin/tags/{id}", adminMW(http.HandlerFunc(adminH.DeleteTag)))
		mux.Handle("PUT "+prefix+"/admin/tags/{id}", adminMW(http.HandlerFunc(adminH.UpdateTag)))
		mux.Handle("PATCH "+prefix+"/admin/tags/{id}", adminMW(http.HandlerFunc(adminH.UpdateTag)))
		// Ticket merge (Laravel TicketMergeController).
		mux.Handle("GET "+prefix+"/admin/tickets/merge-search", adminMW(http.HandlerFunc(mergeH.Search)))
		mux.Handle("POST "+prefix+"/admin/tickets/{id}/merge", adminMW(http.HandlerFunc(mergeH.Merge)))
		// The admin ticket pages and their writes (escalated.admin.tickets.*).
		mux.Handle("GET "+prefix+"/admin/tickets", adminMW(http.HandlerFunc(agentH.AdminListTickets)))
		mux.Handle("POST "+prefix+"/admin/tickets/bulk", adminMW(http.HandlerFunc(agentH.Bulk)))
		mux.Handle("GET "+prefix+"/admin/tickets/{id}", adminMW(http.HandlerFunc(agentH.ShowTicket)))
		mux.Handle("POST "+prefix+"/admin/tickets/{id}/split", adminMW(http.HandlerFunc(agentH.Split)))
		adminTickets := muxGroup{mux: mux, prefix: prefix + "/admin/tickets", mw: adminMW}
		mountTicketWrites(adminTickets, agentH)
		mountTicketPanels(adminTickets, s, subjectH, linkH, scH)
		mux.Handle("GET "+prefix+"/admin/sla-policies", adminMW(http.HandlerFunc(adminH.ListSLAPolicies)))
		mux.Handle("POST "+prefix+"/admin/sla-policies", adminMW(http.HandlerFunc(adminH.CreateSLAPolicy)))
		mux.Handle("GET "+prefix+"/admin/sla-policies/create", adminMW(http.HandlerFunc(adminH.CreateSLAPolicyForm)))
		mux.Handle("GET "+prefix+"/admin/sla-policies/{id}/edit", adminMW(http.HandlerFunc(adminH.EditSLAPolicyForm)))
		mux.Handle("PUT "+prefix+"/admin/sla-policies/{id}", adminMW(http.HandlerFunc(adminH.UpdateSLAPolicy)))
		mux.Handle("PATCH "+prefix+"/admin/sla-policies/{id}", adminMW(http.HandlerFunc(adminH.UpdateSLAPolicy)))
		mux.Handle("DELETE "+prefix+"/admin/sla-policies/{id}", adminMW(http.HandlerFunc(adminH.DeleteSLAPolicy)))
		// Business hours and holidays (escalated.admin.business-hours.*).
		mux.Handle("GET "+prefix+"/admin/business-hours", adminMW(http.HandlerFunc(adminH.ListBusinessHours)))
		mux.Handle("POST "+prefix+"/admin/business-hours", adminMW(http.HandlerFunc(adminH.StoreBusinessHours)))
		mux.Handle("GET "+prefix+"/admin/business-hours/create", adminMW(http.HandlerFunc(adminH.CreateBusinessHoursForm)))
		mux.Handle("GET "+prefix+"/admin/business-hours/{id}/edit", adminMW(http.HandlerFunc(adminH.EditBusinessHoursForm)))
		mux.Handle("PUT "+prefix+"/admin/business-hours/{id}", adminMW(http.HandlerFunc(adminH.UpdateBusinessHours)))
		mux.Handle("PATCH "+prefix+"/admin/business-hours/{id}", adminMW(http.HandlerFunc(adminH.UpdateBusinessHours)))
		mux.Handle("DELETE "+prefix+"/admin/business-hours/{id}", adminMW(http.HandlerFunc(adminH.DeleteBusinessHours)))

		// Custom fields (escalated.admin.custom-fields.*) and custom objects
		// with their records (escalated.admin.custom-objects.*).
		mux.Handle("GET "+prefix+"/admin/custom-fields", adminMW(http.HandlerFunc(adminH.ListCustomFields)))
		mux.Handle("POST "+prefix+"/admin/custom-fields", adminMW(http.HandlerFunc(adminH.StoreCustomField)))
		mux.Handle("GET "+prefix+"/admin/custom-fields/create", adminMW(http.HandlerFunc(adminH.CreateCustomFieldForm)))
		mux.Handle("POST "+prefix+"/admin/custom-fields/reorder", adminMW(http.HandlerFunc(adminH.ReorderCustomFields)))
		mux.Handle("GET "+prefix+"/admin/custom-fields/{id}/edit", adminMW(http.HandlerFunc(adminH.EditCustomFieldForm)))
		mux.Handle("PUT "+prefix+"/admin/custom-fields/{id}", adminMW(http.HandlerFunc(adminH.UpdateCustomField)))
		mux.Handle("PATCH "+prefix+"/admin/custom-fields/{id}", adminMW(http.HandlerFunc(adminH.UpdateCustomField)))
		mux.Handle("DELETE "+prefix+"/admin/custom-fields/{id}", adminMW(http.HandlerFunc(adminH.DeleteCustomField)))
		mux.Handle("GET "+prefix+"/admin/custom-objects", adminMW(http.HandlerFunc(adminH.ListCustomObjects)))
		mux.Handle("POST "+prefix+"/admin/custom-objects", adminMW(http.HandlerFunc(adminH.StoreCustomObject)))
		mux.Handle("GET "+prefix+"/admin/custom-objects/create", adminMW(http.HandlerFunc(adminH.CreateCustomObjectForm)))
		mux.Handle("GET "+prefix+"/admin/custom-objects/{id}/edit", adminMW(http.HandlerFunc(adminH.EditCustomObjectForm)))
		mux.Handle("PUT "+prefix+"/admin/custom-objects/{id}", adminMW(http.HandlerFunc(adminH.UpdateCustomObject)))
		mux.Handle("PATCH "+prefix+"/admin/custom-objects/{id}", adminMW(http.HandlerFunc(adminH.UpdateCustomObject)))
		mux.Handle("DELETE "+prefix+"/admin/custom-objects/{id}", adminMW(http.HandlerFunc(adminH.DeleteCustomObject)))
		mux.Handle("GET "+prefix+"/admin/custom-objects/{id}/records", adminMW(http.HandlerFunc(adminH.CustomObjectRecords)))
		mux.Handle("POST "+prefix+"/admin/custom-objects/{id}/records", adminMW(http.HandlerFunc(adminH.StoreCustomObjectRecord)))
		mux.Handle("PUT "+prefix+"/admin/custom-objects/{id}/records/{record}", adminMW(http.HandlerFunc(adminH.UpdateCustomObjectRecord)))
		mux.Handle("PATCH "+prefix+"/admin/custom-objects/{id}/records/{record}", adminMW(http.HandlerFunc(adminH.UpdateCustomObjectRecord)))
		mux.Handle("DELETE "+prefix+"/admin/custom-objects/{id}/records/{record}", adminMW(http.HandlerFunc(adminH.DeleteCustomObjectRecord)))

		// Custom ticket statuses (escalated.admin.statuses.*).
		mux.Handle("GET "+prefix+"/admin/statuses", adminMW(http.HandlerFunc(adminH.ListStatuses)))
		mux.Handle("POST "+prefix+"/admin/statuses", adminMW(http.HandlerFunc(adminH.StoreStatus)))
		mux.Handle("GET "+prefix+"/admin/statuses/create", adminMW(http.HandlerFunc(adminH.CreateStatusForm)))
		mux.Handle("GET "+prefix+"/admin/statuses/{id}/edit", adminMW(http.HandlerFunc(adminH.EditStatusForm)))
		mux.Handle("PUT "+prefix+"/admin/statuses/{id}", adminMW(http.HandlerFunc(adminH.UpdateStatus)))
		mux.Handle("PATCH "+prefix+"/admin/statuses/{id}", adminMW(http.HandlerFunc(adminH.UpdateStatus)))
		mux.Handle("DELETE "+prefix+"/admin/statuses/{id}", adminMW(http.HandlerFunc(adminH.DeleteStatus)))

		// Import wizard (escalated.admin.import.*, Laravel's paths).
		importH := newImportHandler(esc)
		mux.Handle("GET "+prefix+"/admin/import", adminMW(http.HandlerFunc(importH.Index)))
		mux.Handle("GET "+prefix+"/admin/import/{a}/{b}", adminMW(http.HandlerFunc(importH.Get)))
		mux.Handle("POST "+prefix+"/admin/import/{a}/{b}", adminMW(http.HandlerFunc(importH.Post)))
		mux.Handle("POST "+prefix+"/admin/data-retention/purge", adminMW(http.HandlerFunc(retentionH.Purge)))

		// Event-driven admin Workflows (fire on ticket lifecycle events).
		mux.Handle("GET "+prefix+"/admin/workflows", adminMW(http.HandlerFunc(workflowH.List)))
		mux.Handle("GET "+prefix+"/admin/workflows/create", adminMW(http.HandlerFunc(workflowH.CreateForm)))
		mux.Handle("GET "+prefix+"/admin/workflows/logs", adminMW(http.HandlerFunc(workflowH.LogsPage)))
		mux.Handle("POST "+prefix+"/admin/workflows", adminMW(http.HandlerFunc(workflowH.Create)))
		mux.Handle("POST "+prefix+"/admin/workflows/reorder", adminMW(http.HandlerFunc(workflowH.Reorder)))
		mux.Handle("GET "+prefix+"/admin/workflows/{id}", adminMW(http.HandlerFunc(workflowH.Edit)))
		mux.Handle("PUT "+prefix+"/admin/workflows/{id}", adminMW(http.HandlerFunc(workflowH.Update)))
		mux.Handle("POST "+prefix+"/admin/workflows/{id}/toggle", adminMW(http.HandlerFunc(workflowH.Toggle)))
		mux.Handle("PATCH "+prefix+"/admin/workflows/{id}", adminMW(http.HandlerFunc(workflowH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/workflows/{id}", adminMW(http.HandlerFunc(workflowH.Delete)))
		mux.Handle("GET "+prefix+"/admin/workflows/{id}/logs", adminMW(http.HandlerFunc(workflowH.Logs)))

		// Time-based admin Automations.
		mux.Handle("GET "+prefix+"/admin/automations", adminMW(http.HandlerFunc(autoH.List)))
		mux.Handle("POST "+prefix+"/admin/automations", adminMW(http.HandlerFunc(autoH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Update)))
		mux.Handle("PUT "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Update)))
		mux.Handle("GET "+prefix+"/admin/automations/create", adminMW(http.HandlerFunc(autoH.CreateForm)))
		mux.Handle("GET "+prefix+"/admin/automations/{id}/edit", adminMW(http.HandlerFunc(autoH.EditForm)))
		mux.Handle("DELETE "+prefix+"/admin/automations/{id}", adminMW(http.HandlerFunc(autoH.Delete)))
		mux.Handle("POST "+prefix+"/admin/automations/run", adminMW(http.HandlerFunc(autoH.Run)))

		// Time-based admin Escalation rules.
		mux.Handle("GET "+prefix+"/admin/escalation-rules", adminMW(http.HandlerFunc(escH.List)))
		mux.Handle("POST "+prefix+"/admin/escalation-rules", adminMW(http.HandlerFunc(escH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/escalation-rules/{id}", adminMW(http.HandlerFunc(escH.Update)))
		mux.Handle("PUT "+prefix+"/admin/escalation-rules/{id}", adminMW(http.HandlerFunc(escH.Update)))
		mux.Handle("GET "+prefix+"/admin/escalation-rules/create", adminMW(http.HandlerFunc(escH.CreateForm)))
		mux.Handle("GET "+prefix+"/admin/escalation-rules/{id}/edit", adminMW(http.HandlerFunc(escH.EditForm)))
		mux.Handle("DELETE "+prefix+"/admin/escalation-rules/{id}", adminMW(http.HandlerFunc(escH.Delete)))
		mux.Handle("POST "+prefix+"/admin/escalation-rules/run", adminMW(http.HandlerFunc(escH.Run)))

		// Per-agent ticket capacity (load-aware assignment).
		mux.Handle("GET "+prefix+"/admin/capacity", adminMW(http.HandlerFunc(capH.List)))
		mux.Handle("PATCH "+prefix+"/admin/capacity/{id}", adminMW(http.HandlerFunc(capH.Update)))
		mux.Handle("PUT "+prefix+"/admin/capacity/{id}", adminMW(http.HandlerFunc(capH.Update)))

		// Macros admin CRUD.
		mux.Handle("GET "+prefix+"/admin/macros", adminMW(http.HandlerFunc(macroH.AdminList)))
		mux.Handle("POST "+prefix+"/admin/macros", adminMW(http.HandlerFunc(macroH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Update)))
		mux.Handle("PUT "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/macros/{id}", adminMW(http.HandlerFunc(macroH.Delete)))

		// Canned responses admin CRUD.
		mux.Handle("GET "+prefix+"/admin/canned-responses", adminMW(http.HandlerFunc(cannedH.AdminList)))
		mux.Handle("POST "+prefix+"/admin/canned-responses", adminMW(http.HandlerFunc(cannedH.Create)))
		mux.Handle("PATCH "+prefix+"/admin/canned-responses/{id}", adminMW(http.HandlerFunc(cannedH.Update)))
		mux.Handle("PUT "+prefix+"/admin/canned-responses/{id}", adminMW(http.HandlerFunc(cannedH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/canned-responses/{id}", adminMW(http.HandlerFunc(cannedH.Delete)))

		// Knowledge-base authoring admin CRUD (articles + categories). The
		// public read path is /api/kb/... (published only); this is the admin
		// authoring counterpart.
		mux.Handle("GET "+prefix+"/admin/kb-articles", adminMW(http.HandlerFunc(articleH.AdminList)))
		mux.Handle("GET "+prefix+"/admin/kb-articles/create", adminMW(http.HandlerFunc(articleH.CreateForm)))
		mux.Handle("POST "+prefix+"/admin/kb-articles", adminMW(http.HandlerFunc(articleH.Create)))
		mux.Handle("GET "+prefix+"/admin/kb-articles/{id}/edit", adminMW(http.HandlerFunc(articleH.EditForm)))
		mux.Handle("PUT "+prefix+"/admin/kb-articles/{id}", adminMW(http.HandlerFunc(articleH.Update)))
		mux.Handle("PATCH "+prefix+"/admin/kb-articles/{id}", adminMW(http.HandlerFunc(articleH.Update)))
		mux.Handle("DELETE "+prefix+"/admin/kb-articles/{id}", adminMW(http.HandlerFunc(articleH.Delete)))
		mux.Handle("GET "+prefix+"/admin/kb-categories", adminMW(http.HandlerFunc(articleH.ListCategories)))
		mux.Handle("POST "+prefix+"/admin/kb-categories", adminMW(http.HandlerFunc(articleH.CreateCategory)))
		mux.Handle("PUT "+prefix+"/admin/kb-categories/{id}", adminMW(http.HandlerFunc(articleH.UpdateCategory)))
		mux.Handle("PATCH "+prefix+"/admin/kb-categories/{id}", adminMW(http.HandlerFunc(articleH.UpdateCategory)))
		mux.Handle("DELETE "+prefix+"/admin/kb-categories/{id}", adminMW(http.HandlerFunc(articleH.DeleteCategory)))
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
		mux.Handle("PUT "+prefix+"/admin/webhooks/{id}", adminMW(http.HandlerFunc(webhookH.Update)))
		mux.Handle("GET "+prefix+"/admin/webhooks/create", adminMW(http.HandlerFunc(webhookH.CreateForm)))
		mux.Handle("GET "+prefix+"/admin/webhooks/{id}/edit", adminMW(http.HandlerFunc(webhookH.EditForm)))
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
		mux.Handle("GET "+prefix+"/admin/reports/dashboard", adminMW(http.HandlerFunc(reportH.DashboardPage)))
		mux.Handle("GET "+prefix+"/admin/reports/agents", adminMW(http.HandlerFunc(reportH.AgentsPage)))
		mux.Handle("GET "+prefix+"/admin/reports/sla-trends", adminMW(http.HandlerFunc(reportH.SLATrendsPage)))
		mux.Handle("GET "+prefix+"/admin/reports/frt", adminMW(http.HandlerFunc(reportH.ResponseTimesPage)))
		mux.Handle("GET "+prefix+"/admin/reports/response-times", adminMW(http.HandlerFunc(reportH.ResponseTimesPage)))
		mux.Handle("GET "+prefix+"/admin/reports/resolution", adminMW(http.HandlerFunc(reportH.ResolutionTimesPage)))
		mux.Handle("GET "+prefix+"/admin/reports/resolution-times", adminMW(http.HandlerFunc(reportH.ResolutionTimesPage)))
		mux.Handle("GET "+prefix+"/admin/reports/cohorts", adminMW(http.HandlerFunc(reportH.CohortsPage)))
		mux.Handle("GET "+prefix+"/admin/reports/agent/{id}/detail", adminMW(http.HandlerFunc(reportH.AgentDetailPage)))
		mux.Handle("GET "+prefix+"/admin/reports/comparison", adminMW(http.HandlerFunc(reportH.ComparisonPage)))

		mux.Handle("GET "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.GetPublicTicketsSettings)))
		mux.Handle("PUT "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.UpdatePublicTicketsSettings)))
		mux.Handle("POST "+prefix+"/admin/settings/public-tickets", adminMW(http.HandlerFunc(adminH.UpdatePublicTicketsSettings)))
		// The Settings sub-pages (escalated.admin.settings.*).
		mux.Handle("GET "+prefix+"/admin/settings/csat", adminMW(http.HandlerFunc(adminH.CsatSettingsPage)))
		mux.Handle("POST "+prefix+"/admin/settings/csat", adminMW(http.HandlerFunc(adminH.UpdateCsatSettings)))
		mux.Handle("GET "+prefix+"/admin/settings/data-retention", adminMW(http.HandlerFunc(adminH.DataRetentionPage)))
		mux.Handle("POST "+prefix+"/admin/settings/data-retention", adminMW(http.HandlerFunc(adminH.UpdateDataRetention)))
		mux.Handle("GET "+prefix+"/admin/settings/database", adminMW(http.HandlerFunc(adminH.DatabaseConnectionPage)))
		mux.Handle("POST "+prefix+"/admin/settings/database", adminMW(http.HandlerFunc(adminH.UpdateDatabaseConnection)))
		mux.Handle("POST "+prefix+"/admin/settings/database/test", adminMW(http.HandlerFunc(adminH.TestDatabaseConnection)))
		mux.Handle("GET "+prefix+"/admin/settings/email", adminMW(http.HandlerFunc(adminH.EmailSettingsPage)))
		mux.Handle("POST "+prefix+"/admin/settings/email", adminMW(http.HandlerFunc(adminH.UpdateEmailSettings)))
		mux.Handle("GET "+prefix+"/admin/settings/widget", adminMW(http.HandlerFunc(adminH.GetWidgetSettings)))
		mux.Handle("PUT "+prefix+"/admin/settings/widget", adminMW(http.HandlerFunc(adminH.UpdateWidgetSettings)))

		// Users (host User table: list + grant/revoke admin/agent).
		// Audit log of admin changes (Laravel AuditLogController).
		mux.Handle("GET "+prefix+"/admin/audit-log", adminMW(http.HandlerFunc(auditH.Index)))
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
	return ticketSvc
}
