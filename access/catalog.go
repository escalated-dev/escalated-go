package access

// PermissionDef is one permission the seed creates.
type PermissionDef struct {
	Slug        string
	Name        string
	Group       string
	Description string
}

// RoleDef is one system role the seed creates. Permissions are slugs,
// "group.*" wildcards, or "*" for every permission.
type RoleDef struct {
	Slug        string
	Name        string
	Description string
	Permissions []string
}

// Permissions is escalated-laravel's PermissionSeeder list. No slug is
// invented here: every route maps onto these.
var Permissions = []PermissionDef{
	{"ticket.view", "View tickets", "Tickets", "View tickets"},
	{"ticket.create", "Create tickets", "Tickets", "Create tickets"},
	{"ticket.edit", "Edit ticket properties", "Tickets", "Edit ticket properties"},
	{"ticket.delete", "Delete tickets", "Tickets", "Delete tickets"},
	{"ticket.assign", "Assign tickets", "Tickets", "Assign tickets to agents"},
	{"ticket.merge", "Merge tickets", "Tickets", "Merge tickets together"},
	{"ticket.close", "Close tickets", "Tickets", "Close and reopen tickets"},
	{"ticket.export", "Export tickets", "Tickets", "Export ticket data"},

	{"reply.create", "Reply to tickets", "Replies", "Reply to tickets"},
	{"reply.create_internal", "Add internal notes", "Replies", "Add internal notes"},
	{"reply.edit", "Edit replies", "Replies", "Edit replies"},
	{"reply.delete", "Delete replies", "Replies", "Delete replies"},

	{"kb.view", "View knowledge base", "Knowledge Base", "View knowledge base"},
	{"kb.create", "Create articles", "Knowledge Base", "Create articles"},
	{"kb.edit", "Edit articles", "Knowledge Base", "Edit articles"},
	{"kb.delete", "Delete articles", "Knowledge Base", "Delete articles"},
	{"kb.publish", "Publish articles", "Knowledge Base", "Publish/unpublish articles"},

	{"department.view", "View departments", "Departments", "View departments"},
	{"department.create", "Create departments", "Departments", "Create departments"},
	{"department.edit", "Edit departments", "Departments", "Edit departments"},
	{"department.delete", "Delete departments", "Departments", "Delete departments"},

	{"report.view", "View reports", "Reports", "View reports and analytics"},
	{"report.export", "Export reports", "Reports", "Export report data"},

	{"sla.view", "View SLA policies", "SLA", "View SLA policies"},
	{"sla.manage", "Manage SLA policies", "SLA", "Create, edit, delete SLA policies"},

	{"automation.view", "View automations", "Automations", "View automations"},
	{"automation.manage", "Manage automations", "Automations", "Create, edit, delete automations"},

	{"escalation.view", "View escalation rules", "Escalation Rules", "View escalation rules"},
	{"escalation.manage", "Manage escalation rules", "Escalation Rules", "Create, edit, delete escalation rules"},

	{"macro.view", "View macros", "Macros", "View macros"},
	{"macro.create", "Create macros", "Macros", "Create personal macros"},
	{"macro.manage", "Manage macros", "Macros", "Create, edit, delete shared macros"},

	{"tag.view", "View tags", "Tags", "View tags"},
	{"tag.manage", "Manage tags", "Tags", "Create, edit, delete tags"},

	{"custom_field.view", "View custom fields", "Custom Fields", "View custom fields"},
	{"custom_field.manage", "Manage custom fields", "Custom Fields", "Create, edit, delete custom fields"},

	{"role.view", "View roles", "Roles", "View roles"},
	{"role.manage", "Manage roles", "Roles", "Create, edit, delete roles and assign permissions"},

	{"user.view", "View users", "Users", "View user profiles"},
	{"user.manage", "Manage users", "Users", "Manage user accounts and agent profiles"},

	{"settings.view", "View settings", "Settings", "View settings"},
	{"settings.manage", "Manage settings", "Settings", "Manage system settings"},

	{"webhook.view", "View webhooks", "Webhooks", "View webhooks"},
	{"webhook.manage", "Manage webhooks", "Webhooks", "Create, edit, delete webhooks"},

	{"api_token.view", "View API tokens", "API Tokens", "View API tokens"},
	{"api_token.manage", "Manage API tokens", "API Tokens", "Create, revoke API tokens"},

	{"audit.view", "View audit log", "Audit Log", "View audit log"},

	{"plugin.view", "View plugins", "Plugins", "View plugins"},
	{"plugin.manage", "Manage plugins", "Plugins", "Install, configure, remove plugins"},

	{"custom_object.view", "View custom objects", "Custom Objects", "View custom objects"},
	{"custom_object.manage", "Manage custom objects", "Custom Objects", "Create, edit, delete custom object schemas"},
	{"custom_object.data", "Manage custom object data", "Custom Objects", "Manage custom object records"},

	{"newsletters.manage", "Manage newsletters", "Newsletters", "Create, edit, delete drafts and lists/templates; send test emails."},
	{"newsletters.send", "Send newsletters", "Newsletters", "Schedule or send newsletters now."},
}

// SystemRoles are escalated-laravel's seeded roles. They cannot be deleted.
var SystemRoles = []RoleDef{
	{
		Slug:        "admin",
		Name:        "Admin",
		Description: "Full access to all features and settings.",
		Permissions: []string{"*"},
	},
	{
		Slug:        "agent",
		Name:        "Agent",
		Description: "Standard agent with ticket handling and limited administrative access.",
		Permissions: []string{
			"ticket.*", "reply.*", "kb.view", "report.view", "macro.view", "macro.create",
			"tag.view", "custom_field.view", "audit.view",
		},
	},
	{
		Slug:        "light_agent",
		Name:        "Light Agent",
		Description: "Limited agent with read-only ticket access and internal note capability.",
		Permissions: []string{
			"ticket.view", "reply.create", "reply.create_internal", "kb.view", "macro.view", "tag.view",
		},
	},
}

// expand resolves a role's patterns against the known slugs.
func expand(patterns []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(slug string) {
		if !seen[slug] {
			seen[slug] = true
			out = append(out, slug)
		}
	}
	for _, p := range patterns {
		for _, def := range Permissions {
			switch {
			case p == "*":
				add(def.Slug)
			case len(p) > 2 && p[len(p)-2:] == ".*":
				if len(def.Slug) > len(p)-1 && def.Slug[:len(p)-1] == p[:len(p)-1] {
					add(def.Slug)
				}
			case p == def.Slug:
				add(def.Slug)
			}
		}
	}
	return out
}
