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
// With Config.EnableWidget it also mounts the support widget under
// {RoutePrefix}/widget (see docs/embedding-the-widget.md).
func MountChi(r chi.Router, esc *escalated.Escalated) {
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

	actionRegistry := actions.NewRegistry(cfg.TicketActions)

	apiH := handlers.NewAPIHandler(s, ticketSvc, rend, cfg.UserIDFunc)
	apiH.Subjects = subjectSvc
	apiH.CustomFields = customFieldService(esc)
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

	// Support widget — only with EnableWidget. Mounted beside the main prefix
	// router so WidgetCORS runs before the ticket-access middleware (an
	// allowed cross-origin request is then scoped as a guest) and sees every
	// OPTIONS preflight. embed.js stays outside WidgetCORS: it is public.
	if cfg.EnableWidget {
		stack := newWidgetStack(esc, ticketSvc)
		r.Get(cfg.RoutePrefix+"/widget/embed.js", handlers.WidgetScript)
		r.Route(cfg.RoutePrefix+"/widget", func(r chi.Router) {
			r.Use(stack.wrap)
			for _, route := range stack.routes {
				r.MethodFunc(route.method, route.path, route.handler)
			}
		})
	}

	r.Route(cfg.RoutePrefix, func(r chi.Router) {
		// API tokens authenticate first, so ticket visibility is the token owner's.
		r.Use(esc.APITokenMiddleware)
		r.Use(esc.TicketAccessMiddleware)
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

		// Attachment downloads — always mounted. The handler authorizes each
		// download through the attachment's ticket (see AttachmentHandler).
		r.Get("/attachments/{id}/download", attachH.Download)

		// JSON API — always mounted
		r.Route("/api", func(r chi.Router) {
			// Tickets, departments and tags are agent-level, like /agent: these
			// routes list every ticket, return internal notes and change status
			// and priority. The auth, guest and knowledge-base routes below stay
			// public.
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireAgentOrAdmin(cfg.AgentCheck, cfg.AdminCheck))
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
			})

			// Authentication — delegated to host-app callbacks (handlers.APIAuth).
			r.Post("/auth/login", authH.Login)
			r.Post("/auth/register", authH.Register)
			r.Post("/auth/logout", authH.Logout)
			r.Post("/auth/refresh", authH.Refresh)
			r.Get("/auth/me", authH.Me)
			r.Patch("/auth/profile", authH.Profile)
			r.Post("/auth/validate", authH.Validate)

			// Anonymous (guest) ticket submission + lookup by token.
			// Creating a ticket needs an email code from /guest/verification;
			// {token} is the expiring grant it returns (docs/guest-access.md).
			r.With(guestLimit.Middleware(middleware.GuestTicketScope)).Post("/guest/verification", guestVerifyH.Request)
			r.With(guestLimit.Middleware(middleware.GuestReplyScope)).Post("/guest/lookup", guestVerifyH.Lookup)
			r.With(guestLimit.Middleware(middleware.GuestTicketScope)).Post("/guest/tickets", guestH.Create)
			r.With(guestLimit.Middleware(middleware.GuestRequestScope)).Get("/guest/tickets/{token}", guestH.Show)
			r.With(guestLimit.Middleware(middleware.GuestReplyScope)).Post("/guest/tickets/{token}/replies", guestH.Reply)
			r.With(guestLimit.Middleware(middleware.GuestReplyScope)).Post("/guest/tickets/{token}/rate", satH.GuestRate)

			// Public knowledge base (published only), behind the KB enabled
			// and public settings.
			r.Group(func(r chi.Router) {
				r.Use(kbGuard)
				r.Get("/kb/articles", kbH.ListArticles)
				r.Get("/kb/categories", kbH.ListCategories)
				r.Get("/kb/articles/{slug}", kbH.ShowArticle)
			})
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
			// Customer routes — a signed-in user only. Show and Reply are further
			// limited to the ticket's requester in the handler.
			r.Route("/tickets", func(r chi.Router) {
				r.Use(middleware.RequireUser(cfg.UserIDFunc))
				r.Get("/", customerH.Index)
				r.Post("/", customerH.Create)
				r.Get("/create", customerH.CreateForm)
				r.Get("/{id}", customerH.Show)
				r.Post("/{id}/replies", customerH.Reply)
				r.Post("/{id}/rate", customerH.Rate)
				// The customer ticket screen's writes (Laravel route names
				// escalated.customer.tickets.reply / close / reopen).
				r.Post("/{id}/reply", customerH.Reply)
				r.Post("/{id}/close", customerH.Close)
				r.Post("/{id}/reopen", customerH.Reopen)
			})

			// The guest ticket pages (escalated.guest.tickets.*) on the
			// verified guest flow, behind the guest rate limits.
			r.Route("/guest", func(r chi.Router) {
				r.Get("/create", guestPages.Create)
				r.With(guestLimit.Middleware(middleware.GuestTicketScope)).Post("/", guestPages.Submit)
				r.With(guestLimit.Middleware(middleware.GuestRequestScope)).Get("/{token}", guestPages.Show)
				r.With(guestLimit.Middleware(middleware.GuestReplyScope)).Post("/{token}/reply", guestPages.Reply)
				r.With(guestLimit.Middleware(middleware.GuestReplyScope)).Post("/{token}/rate", guestPages.Rate)
			})

			// The knowledge base reader pages (escalated.customer.kb.*), behind
			// the knowledge base enabled and public settings.
			r.Group(func(r chi.Router) {
				r.Use(kbGuard)
				r.Get("/kb", kbPages.Index)
				r.Get("/kb/{slug}", kbPages.Article)
				r.Post("/kb/{slug}/feedback", kbPages.Feedback)
			})

			// Agent routes
			r.Route("/agent", func(r chi.Router) {
				r.Use(middleware.RequireAgent(cfg.AgentCheck))
				r.Get("/", agentH.Dashboard)
				r.Get("/tickets", agentH.ListTickets)
				r.Get("/tickets/{id}", agentH.ShowTicket)
				r.Post("/tickets/{id}/replies", agentH.Reply)
				r.Post("/tickets/{id}/actions/{action}", agentH.CustomAction)
				r.Post("/tickets/bulk", agentH.Bulk)
				// The ticket screen's writes (escalated.agent.tickets.*) and
				// panels: subjects, typed links, side conversations.
				mountTicketWrites(chiGroup{r: r, prefix: "/tickets"}, agentH)
				mountTicketPanels(chiGroup{r: r, prefix: "/tickets"}, s, subjectH, linkH, scH)

				// Macros (agent-applied one-click bundles).
				r.Get("/macros", macroH.AgentList)
				r.Post("/tickets/{ticketId}/macros/{macroId}/apply", macroH.AgentApply)

				// Canned responses visible to this agent (shared + own).
				r.Get("/canned-responses", cannedH.AgentList)

				// Live chat console: {id} is the chat ticket's id.
				r.Get("/chat/sessions", chatH.ListSessions)
				r.Post("/chat/sessions/{id}/accept", chatH.AcceptSession)
				r.Post("/chat/sessions/{id}/message", chatH.SendMessage)
				r.Post("/chat/sessions/{id}/end", chatH.EndSession)
			})

			// Admin routes
			r.Route("/admin", func(r chi.Router) {
				r.Use(middleware.RequireAdmin(cfg.AdminCheck))
				r.Get("/departments", adminH.ListDepartments)
				r.Post("/departments", adminH.CreateDepartment)
				r.Get("/departments/create", adminH.CreateDepartmentForm)
				r.Get("/departments/{id}/edit", adminH.EditDepartmentForm)
				r.Put("/departments/{id}", adminH.UpdateDepartment)
				r.Patch("/departments/{id}", adminH.UpdateDepartment)
				r.Delete("/departments/{id}", adminH.DeleteDepartment)
				r.Get("/tags", adminH.ListTags)
				r.Post("/tags", adminH.CreateTag)
				r.Put("/tags/{id}", adminH.UpdateTag)
				r.Patch("/tags/{id}", adminH.UpdateTag)
				r.Delete("/tags/{id}", adminH.DeleteTag)
				// Ticket merge (Laravel TicketMergeController).
				r.Get("/tickets/merge-search", mergeH.Search)
				r.Post("/tickets/{id}/merge", mergeH.Merge)

				// The admin ticket pages and their writes
				// (escalated.admin.tickets.*).
				r.Get("/tickets", agentH.AdminListTickets)
				r.Post("/tickets/bulk", agentH.Bulk)
				r.Get("/tickets/{id}", agentH.ShowTicket)
				r.Post("/tickets/{id}/split", agentH.Split)
				mountTicketWrites(chiGroup{r: r, prefix: "/tickets"}, agentH)
				mountTicketPanels(chiGroup{r: r, prefix: "/tickets"}, s, subjectH, linkH, scH)
				r.Get("/sla-policies", adminH.ListSLAPolicies)
				r.Post("/sla-policies", adminH.CreateSLAPolicy)
				r.Get("/sla-policies/create", adminH.CreateSLAPolicyForm)
				r.Get("/sla-policies/{id}/edit", adminH.EditSLAPolicyForm)
				r.Put("/sla-policies/{id}", adminH.UpdateSLAPolicy)
				r.Patch("/sla-policies/{id}", adminH.UpdateSLAPolicy)
				r.Delete("/sla-policies/{id}", adminH.DeleteSLAPolicy)

				// Business hours and holidays (escalated.admin.business-hours.*).
				r.Get("/business-hours", adminH.ListBusinessHours)
				r.Post("/business-hours", adminH.StoreBusinessHours)
				r.Get("/business-hours/create", adminH.CreateBusinessHoursForm)
				r.Get("/business-hours/{id}/edit", adminH.EditBusinessHoursForm)
				r.Put("/business-hours/{id}", adminH.UpdateBusinessHours)
				r.Patch("/business-hours/{id}", adminH.UpdateBusinessHours)
				r.Delete("/business-hours/{id}", adminH.DeleteBusinessHours)

				// Custom fields (escalated.admin.custom-fields.*) and custom
				// objects with their records (escalated.admin.custom-objects.*).
				r.Get("/custom-fields", adminH.ListCustomFields)
				r.Post("/custom-fields", adminH.StoreCustomField)
				r.Get("/custom-fields/create", adminH.CreateCustomFieldForm)
				r.Post("/custom-fields/reorder", adminH.ReorderCustomFields)
				r.Get("/custom-fields/{id}/edit", adminH.EditCustomFieldForm)
				r.Put("/custom-fields/{id}", adminH.UpdateCustomField)
				r.Patch("/custom-fields/{id}", adminH.UpdateCustomField)
				r.Delete("/custom-fields/{id}", adminH.DeleteCustomField)
				r.Get("/custom-objects", adminH.ListCustomObjects)
				r.Post("/custom-objects", adminH.StoreCustomObject)
				r.Get("/custom-objects/create", adminH.CreateCustomObjectForm)
				r.Get("/custom-objects/{id}/edit", adminH.EditCustomObjectForm)
				r.Put("/custom-objects/{id}", adminH.UpdateCustomObject)
				r.Patch("/custom-objects/{id}", adminH.UpdateCustomObject)
				r.Delete("/custom-objects/{id}", adminH.DeleteCustomObject)
				r.Get("/custom-objects/{id}/records", adminH.CustomObjectRecords)
				r.Post("/custom-objects/{id}/records", adminH.StoreCustomObjectRecord)
				r.Put("/custom-objects/{id}/records/{record}", adminH.UpdateCustomObjectRecord)
				r.Patch("/custom-objects/{id}/records/{record}", adminH.UpdateCustomObjectRecord)
				r.Delete("/custom-objects/{id}/records/{record}", adminH.DeleteCustomObjectRecord)

				// Custom ticket statuses (escalated.admin.statuses.*).
				r.Get("/statuses", adminH.ListStatuses)
				r.Post("/statuses", adminH.StoreStatus)
				r.Get("/statuses/create", adminH.CreateStatusForm)
				r.Get("/statuses/{id}/edit", adminH.EditStatusForm)
				r.Put("/statuses/{id}", adminH.UpdateStatus)
				r.Patch("/statuses/{id}", adminH.UpdateStatus)
				r.Delete("/statuses/{id}", adminH.DeleteStatus)

				// Import wizard (escalated.admin.import.*, Laravel's paths).
				importH := newImportHandler(esc)
				r.Get("/import", importH.Index)
				r.Get("/import/{a}/{b}", importH.Get)
				r.Post("/import/{a}/{b}", importH.Post)

				// Data-retention purge (attachments + audit logs per policy).
				r.Post("/data-retention/purge", retentionH.Purge)

				// Event-driven admin Workflows (fire on ticket lifecycle events;
				// distinct from time-based Automations and agent-applied Macros —
				// see escalated-developer-context).
				r.Get("/workflows", workflowH.List)
				r.Get("/workflows/create", workflowH.CreateForm)
				r.Get("/workflows/logs", workflowH.LogsPage)
				r.Post("/workflows", workflowH.Create)
				r.Post("/workflows/reorder", workflowH.Reorder)
				r.Get("/workflows/{id}", workflowH.Edit)
				r.Put("/workflows/{id}", workflowH.Update)
				r.Patch("/workflows/{id}", workflowH.Update)
				r.Delete("/workflows/{id}", workflowH.Delete)
				r.Post("/workflows/{id}/toggle", workflowH.Toggle)
				r.Get("/workflows/{id}/logs", workflowH.Logs)

				// Time-based admin Automations (distinct from event-driven
				// Workflows and agent-applied Macros — see
				// escalated-developer-context).
				r.Get("/automations", autoH.List)
				r.Post("/automations", autoH.Create)
				r.Get("/automations/create", autoH.CreateForm)
				r.Get("/automations/{id}/edit", autoH.EditForm)
				r.Put("/automations/{id}", autoH.Update)
				r.Patch("/automations/{id}", autoH.Update)
				r.Delete("/automations/{id}", autoH.Delete)
				r.Post("/automations/run", autoH.Run)

				// Time-based admin Escalation rules.
				r.Get("/escalation-rules", escH.List)
				r.Post("/escalation-rules", escH.Create)
				r.Get("/escalation-rules/create", escH.CreateForm)
				r.Get("/escalation-rules/{id}/edit", escH.EditForm)
				r.Put("/escalation-rules/{id}", escH.Update)
				r.Patch("/escalation-rules/{id}", escH.Update)
				r.Delete("/escalation-rules/{id}", escH.Delete)
				r.Post("/escalation-rules/run", escH.Run)

				// Per-agent ticket capacity (load-aware assignment).
				r.Get("/capacity", capH.List)
				r.Put("/capacity/{id}", capH.Update)
				r.Patch("/capacity/{id}", capH.Update)

				// Macros admin CRUD.
				r.Get("/macros", macroH.AdminList)
				r.Post("/macros", macroH.Create)
				r.Put("/macros/{id}", macroH.Update)
				r.Patch("/macros/{id}", macroH.Update)
				r.Delete("/macros/{id}", macroH.Delete)

				// Canned responses admin CRUD.
				r.Get("/canned-responses", cannedH.AdminList)
				r.Post("/canned-responses", cannedH.Create)
				r.Put("/canned-responses/{id}", cannedH.Update)
				r.Patch("/canned-responses/{id}", cannedH.Update)
				r.Delete("/canned-responses/{id}", cannedH.Delete)

				// Knowledge-base authoring admin CRUD (articles + categories).
				// The public read path is /api/kb/... (published only); this is
				// the admin authoring counterpart.
				// Laravel's resource paths for the knowledge base pages
				// (escalated.admin.kb-articles.*, kb-categories.*).
				r.Get("/kb-articles", articleH.AdminList)
				r.Get("/kb-articles/create", articleH.CreateForm)
				r.Post("/kb-articles", articleH.Create)
				r.Get("/kb-articles/{id}/edit", articleH.EditForm)
				r.Put("/kb-articles/{id}", articleH.Update)
				r.Patch("/kb-articles/{id}", articleH.Update)
				r.Delete("/kb-articles/{id}", articleH.Delete)
				r.Get("/kb-categories", articleH.ListCategories)
				r.Post("/kb-categories", articleH.CreateCategory)
				r.Put("/kb-categories/{id}", articleH.UpdateCategory)
				r.Patch("/kb-categories/{id}", articleH.UpdateCategory)
				r.Delete("/kb-categories/{id}", articleH.DeleteCategory)
				r.Get("/kb/articles", articleH.AdminList)
				r.Post("/kb/articles", articleH.Create)
				r.Get("/kb/articles/{id}", articleH.Show)
				r.Patch("/kb/articles/{id}", articleH.Update)
				r.Delete("/kb/articles/{id}", articleH.Delete)
				r.Get("/kb/categories", articleH.ListCategories)
				r.Post("/kb/categories", articleH.CreateCategory)
				r.Patch("/kb/categories/{id}", articleH.UpdateCategory)
				r.Delete("/kb/categories/{id}", articleH.DeleteCategory)

				// Outbound webhooks admin CRUD + per-delivery retry.
				// API tokens (escalated.admin.api-tokens.*).
				if tokenH := newAPITokenHandler(esc); tokenH != nil {
					r.Get("/api-tokens", tokenH.Index)
					r.Post("/api-tokens", tokenH.Store)
					r.Patch("/api-tokens/{id}", tokenH.Update)
					r.Put("/api-tokens/{id}", tokenH.Update)
					r.Delete("/api-tokens/{id}", tokenH.Destroy)
				}

				r.Get("/webhooks", webhookH.List)
				r.Post("/webhooks", webhookH.Create)
				r.Get("/webhooks/create", webhookH.CreateForm)
				r.Get("/webhooks/{id}/edit", webhookH.EditForm)
				r.Put("/webhooks/{id}", webhookH.Update)
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

				// Reporting analytics (read-only). Exposes the reporting service
				// as per-report JSON endpoints; ?days defaults to 30. See
				// escalated-developer-context — mirrors the Laravel reference
				// ReportController surface.
				r.Get("/reports", reportH.Index)
				r.Get("/reports/first-response-time", reportH.FirstResponseTime)
				r.Get("/reports/resolution-time", reportH.ResolutionTime)
				r.Get("/reports/agent-ranking", reportH.AgentRanking)
				r.Get("/reports/sla", reportH.SLA)
				r.Get("/reports/csat", reportH.CSAT)
				r.Get("/reports/period-comparison", reportH.PeriodComparison)
				// The report pages at Laravel's paths (escalated.admin.reports.*).
				r.Get("/reports/dashboard", reportH.DashboardPage)
				r.Get("/reports/agents", reportH.AgentsPage)
				r.Get("/reports/sla-trends", reportH.SLATrendsPage)
				r.Get("/reports/frt", reportH.ResponseTimesPage)
				r.Get("/reports/response-times", reportH.ResponseTimesPage)
				r.Get("/reports/resolution", reportH.ResolutionTimesPage)
				r.Get("/reports/resolution-times", reportH.ResolutionTimesPage)
				r.Get("/reports/cohorts", reportH.CohortsPage)
				r.Get("/reports/agent/{id}/detail", reportH.AgentDetailPage)
				r.Get("/reports/comparison", reportH.ComparisonPage)

				r.Get("/settings/public-tickets", adminH.GetPublicTicketsSettings)
				r.Put("/settings/public-tickets", adminH.UpdatePublicTicketsSettings)
				r.Post("/settings/public-tickets", adminH.UpdatePublicTicketsSettings)
				// The Settings sub-pages (escalated.admin.settings.*).
				r.Get("/settings", adminH.SettingsPage)
				r.Post("/settings", adminH.UpdateSettings)
				r.Get("/settings/csat", adminH.CsatSettingsPage)
				r.Post("/settings/csat", adminH.UpdateCsatSettings)
				r.Get("/settings/data-retention", adminH.DataRetentionPage)
				r.Post("/settings/data-retention", adminH.UpdateDataRetention)
				r.Get("/settings/database", adminH.DatabaseConnectionPage)
				r.Post("/settings/database", adminH.UpdateDatabaseConnection)
				r.Post("/settings/database/test", adminH.TestDatabaseConnection)
				r.Get("/settings/email", adminH.EmailSettingsPage)
				r.Post("/settings/email", adminH.UpdateEmailSettings)
				r.Get("/settings/widget", adminH.GetWidgetSettings)
				r.Put("/settings/widget", adminH.UpdateWidgetSettings)

				// Users (host User table: list + grant/revoke admin/agent).
				// Audit log of admin changes (Laravel AuditLogController).
				r.Get("/audit-log", auditH.Index)

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
