package plugins

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
	"github.com/escalated-dev/escalated-go/store"
	"github.com/escalated-dev/escalated-go/ticketaccess"
)

// Agent is one of ctx.agents' answers.
type Agent struct {
	ID    any     `json:"id"`
	Name  string  `json:"name"`
	Email *string `json:"email"`
}

// Domain is what ctx.tickets, ctx.replies, ctx.contacts, ctx.tags,
// ctx.departments, ctx.agents and ctx.broadcast reach. escalated.New wires
// it; writes go through the ticket service, so they fire the same events,
// webhooks, workflows and emails as anything else.
type Domain struct {
	DB          *sql.DB
	TablePrefix string
	Store       store.Store
	Tickets     *services.TicketService
	// Agents lists the host's agents (Config.SkillAgentDirectory). Nil means
	// ctx.agents lists none.
	Agents func(ctx context.Context) ([]Agent, error)
	// Broadcaster carries ctx.broadcast. Nil makes it a no-op.
	Broadcaster *services.Broadcaster
}

// SetDomain wires the ctx domain APIs. escalated.New calls it. A nil
// Broadcaster takes Config.Plugins.Broadcaster.
func (h *Host) SetDomain(d Domain) {
	if h == nil {
		return
	}
	if d.Broadcaster == nil {
		d.Broadcaster = h.broadcaster
	}
	h.domain = &d
}

// maxQueryRows caps ctx.tickets.query and ctx.replies.query.
const maxQueryRows = 500

func (c *Context) domain() (*Domain, error) {
	if c.host.domain == nil || c.host.domain.Store == nil || c.host.domain.Tickets == nil {
		return nil, errors.New("the plugin context has no ticket service on this host")
	}
	return c.host.domain, nil
}

// actor is the signed-in user of the request being served, or nil: a plugin
// never names who it acts as.
func (c *Context) actor() *models.UserID {
	if c.request == nil {
		return nil
	}
	id := c.host.userID(c.request)
	if id.Empty() {
		return nil
	}
	return &id
}

// unscoped runs domain calls as the host, not as the request's agent: a
// plugin is host code, and ticket visibility does not narrow it.
func unscoped(ctx context.Context) context.Context {
	return ticketaccess.WithoutVisibility(ctx)
}

func idOf(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return int64(x), true
		}
	case int:
		return int64(x), true
	case int64:
		return x, true
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n, err == nil
	}
	return 0, false
}

func checkKeys(method string, data map[string]any, allowed ...string) error {
	ok := map[string]bool{}
	for _, k := range allowed {
		ok[k] = true
	}
	var bad []string
	for k := range data {
		if !ok[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("%s does not accept %s", method, strings.Join(bad, ", "))
	}
	return nil
}

func stringField(data map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := data[k]; ok && v != nil {
			s, isString := v.(string)
			if !isString {
				return "", false
			}
			return s, true
		}
	}
	return "", false
}

// TicketsAPI is ctx.tickets.
type TicketsAPI struct{ c *Context }

// Tickets is ctx.tickets.
func (c *Context) Tickets() TicketsAPI { return TicketsAPI{c: c} }

// Find returns a ticket by id (or reference), or nil.
func (a TicketsAPI) Find(ctx context.Context, id any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	ctx = unscoped(ctx)
	var t *models.Ticket
	if n, ok := idOf(id); ok {
		t, err = d.Store.GetTicket(ctx, n)
	} else if ref, isString := id.(string); isString && ref != "" {
		t, err = d.Store.GetTicketByReference(ctx, ref)
	}
	if err != nil || t == nil {
		return nil, err
	}
	if tags, err := d.Store.GetTicketTags(ctx, t.ID); err == nil {
		for _, tag := range tags {
			t.Tags = append(t.Tags, *tag)
		}
	}
	return ticketObject(t), nil
}

// ticketColumns are the only keys ctx.tickets.query filters on, and the
// columns they mean.
var ticketColumns = map[string]string{
	"id": "id", "reference": "reference", "status": "status", "priority": "priority",
	"channel": "", "ticket_type": "ticket_type", "department_id": "department_id",
	"requester_id": "requester_id", "contact_id": "contact_id", "assigned_to": "assigned_to",
}

// Query returns up to 500 tickets matching every key of filter. Only the
// contract's keys are accepted; any other is an error, never a column name.
func (a TicketsAPI) Query(ctx context.Context, filter map[string]any) ([]map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(filter))
	for k := range filter {
		if _, ok := ticketColumns[k]; !ok {
			return nil, fmt.Errorf("ctx.tickets.query cannot filter on %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var where []string
	var args []any
	for _, k := range keys {
		v := filter[k]
		switch k {
		case "channel":
			return nil, errors.New("ctx.tickets.query: this host stores no ticket channel")
		case "status":
			n, ok := services.StatusFromValue(v)
			if !ok {
				return nil, fmt.Errorf("ctx.tickets.query: unknown status %v", v)
			}
			v = n
		case "priority":
			n, ok := services.PriorityFromValue(v)
			if !ok {
				return nil, fmt.Errorf("ctx.tickets.query: unknown priority %v", v)
			}
			v = n
		case "id", "department_id", "contact_id":
			n, ok := idOf(v)
			if !ok {
				return nil, fmt.Errorf("ctx.tickets.query: %s must be a number", k)
			}
			v = n
		default:
			if f, ok := v.(float64); ok {
				v = strconv.FormatFloat(f, 'f', -1, 64)
			}
			if _, ok := v.(string); !ok {
				return nil, fmt.Errorf("ctx.tickets.query: %s must be a string or number", k)
			}
		}
		where = append(where, ticketColumns[k]+" = ?")
		args = append(args, v)
	}
	q := "SELECT id FROM " + d.TablePrefix + "tickets"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY id LIMIT %d", maxQueryRows)
	ids, err := queryIDs(ctx, d.DB, q, args...)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, id := range ids {
		t, err := d.Store.GetTicket(unscoped(ctx), id)
		if err != nil {
			return nil, err
		}
		if t != nil {
			out = append(out, ticketObject(t))
		}
	}
	return out, nil
}

func queryIDs(ctx context.Context, db *sql.DB, q string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, sqldialect.Rebind(db, q), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

var ticketWriteKeys = []string{"subject", "title", "body", "description", "priority", "status", "department_id", "ticket_type", "metadata"}

// Create opens a ticket through the ticket service, so ticket.created fires.
// It takes subject (or title), description (or body), priority, status (a
// slug), department_id, ticket_type, channel, metadata, and contact_id or
// requester_email/requester_name (a contact, found or created).
func (a TicketsAPI) Create(ctx context.Context, data map[string]any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errors.New("ctx.tickets.create requires an object")
	}
	if err := checkKeys("ctx.tickets.create", data, append(ticketWriteKeys, "channel", "contact_id", "requester_email", "requester_name")...); err != nil {
		return nil, err
	}
	ctx = unscoped(ctx)
	in := services.CreateTicketInput{Priority: models.PriorityMedium}
	subject, _ := stringField(data, "subject", "title")
	if strings.TrimSpace(subject) == "" {
		return nil, errors.New("ctx.tickets.create requires a subject")
	}
	in.Subject = subject
	in.Description, _ = stringField(data, "description", "body")
	if v, ok := data["priority"]; ok && v != nil {
		p, ok := services.PriorityFromValue(v)
		if !ok {
			return nil, fmt.Errorf("unknown priority %v", v)
		}
		in.Priority = p
	}
	if v, ok := data["ticket_type"]; ok && v != nil {
		tt, _ := v.(string)
		if !validTicketType(tt) {
			return nil, fmt.Errorf("unknown ticket_type %v", v)
		}
		in.TicketType = tt
	}
	if v, ok := data["department_id"]; ok && v != nil {
		id, err := a.department(ctx, d, v)
		if err != nil {
			return nil, err
		}
		in.DepartmentID = &id
	}
	if v, ok := data["metadata"]; ok && v != nil {
		raw, err := json.Marshal(v)
		if err != nil || asRecord(v) == nil {
			return nil, errors.New("metadata must be an object")
		}
		in.Metadata = raw
	}
	if v, ok := data["contact_id"]; ok && v != nil {
		contact, err := a.c.Contacts().contact(ctx, d, v)
		if err != nil {
			return nil, err
		}
		if contact == nil {
			return nil, fmt.Errorf("no contact %v", v)
		}
		in.GuestEmail = &contact.Email
		in.GuestName = contact.Name
	} else if email, ok := stringField(data, "requester_email"); ok && email != "" {
		normalized := models.NormalizeEmail(email)
		in.GuestEmail = &normalized
		if name, ok := stringField(data, "requester_name"); ok && name != "" {
			in.GuestName = &name
		}
	}
	t, err := d.Tickets.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	if v, ok := data["status"]; ok && v != nil {
		if err := d.Tickets.ChangeStatusValue(ctx, t.ID, v, a.c.actor()); err != nil {
			return nil, err
		}
	}
	return a.Find(ctx, t.ID)
}

func validTicketType(tt string) bool {
	for _, known := range models.TicketTypes {
		if tt == known {
			return true
		}
	}
	return false
}

func (a TicketsAPI) department(ctx context.Context, d *Domain, v any) (int64, error) {
	id, ok := idOf(v)
	if !ok {
		return 0, errors.New("department_id must be a number")
	}
	dept, err := d.Store.GetDepartment(ctx, id)
	if err != nil {
		return 0, err
	}
	if dept == nil {
		return 0, fmt.Errorf("no department %d", id)
	}
	return id, nil
}

// Update changes a ticket through the ticket service, so the matching events
// fire. It takes what Create takes, minus channel and the requester.
func (a TicketsAPI) Update(ctx context.Context, id any, data map[string]any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errors.New("ctx.tickets.update requires an object")
	}
	if err := checkKeys("ctx.tickets.update", data, ticketWriteKeys...); err != nil {
		return nil, err
	}
	ticketID, ok := idOf(id)
	if !ok {
		return nil, errors.New("ctx.tickets.update needs a ticket id")
	}
	ctx = unscoped(ctx)
	if t, err := d.Store.GetTicket(ctx, ticketID); err != nil || t == nil {
		if err == nil {
			err = fmt.Errorf("no ticket %d", ticketID)
		}
		return nil, err
	}
	actor := a.c.actor()

	subject, hasSubject := stringField(data, "subject", "title")
	description, hasDescription := stringField(data, "description", "body")
	ticketType, hasType := stringField(data, "ticket_type")
	if hasType && !validTicketType(ticketType) {
		return nil, fmt.Errorf("unknown ticket_type %v", ticketType)
	}
	if hasSubject || hasDescription || hasType {
		var s, desc, tt *string
		if hasSubject {
			s = &subject
		}
		if hasDescription {
			desc = &description
		}
		if hasType {
			tt = &ticketType
		}
		if _, err := d.Tickets.UpdateDetails(ctx, ticketID, s, desc, tt, actor); err != nil {
			return nil, err
		}
	}
	if v, ok := data["priority"]; ok && v != nil {
		p, ok := services.PriorityFromValue(v)
		if !ok {
			return nil, fmt.Errorf("unknown priority %v", v)
		}
		if err := d.Tickets.ChangePriority(ctx, ticketID, p, actor); err != nil {
			return nil, err
		}
	}
	if v, ok := data["status"]; ok && v != nil {
		if err := d.Tickets.ChangeStatusValue(ctx, ticketID, v, actor); err != nil {
			return nil, err
		}
	}
	if v, ok := data["department_id"]; ok && v != nil {
		dept, err := a.department(ctx, d, v)
		if err != nil {
			return nil, err
		}
		if err := d.Tickets.ChangeDepartment(ctx, ticketID, dept, actor); err != nil {
			return nil, err
		}
	}
	if v, ok := data["metadata"]; ok && v != nil {
		raw, err := json.Marshal(v)
		if err != nil || asRecord(v) == nil {
			return nil, errors.New("metadata must be an object")
		}
		if _, err := d.Tickets.UpdateMetadata(ctx, ticketID, raw, actor); err != nil {
			return nil, err
		}
	}
	return a.Find(ctx, ticketID)
}

// RepliesAPI is ctx.replies.
type RepliesAPI struct{ c *Context }

// Replies is ctx.replies.
func (c *Context) Replies() RepliesAPI { return RepliesAPI{c: c} }

// Find returns a reply by id, or nil.
func (a RepliesAPI) Find(ctx context.Context, id any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	n, ok := idOf(id)
	if !ok {
		return nil, nil
	}
	r, err := d.Store.GetReply(unscoped(ctx), n)
	if err != nil || r == nil {
		return nil, err
	}
	return replyObject(r), nil
}

// Query returns up to 500 replies, filtered on ticket_id, is_internal and type
// (reply, note or system). Any other key is an error.
func (a RepliesAPI) Query(ctx context.Context, filter map[string]any) ([]map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	if err := checkKeys("ctx.replies.query", filter, "ticket_id", "is_internal", "type"); err != nil {
		return nil, err
	}
	var where []string
	var args []any
	if v, ok := filter["ticket_id"]; ok {
		n, ok := idOf(v)
		if !ok {
			return nil, errors.New("ctx.replies.query: ticket_id must be a number")
		}
		where, args = append(where, "ticket_id = ?"), append(args, n)
	}
	if v, ok := filter["is_internal"]; ok {
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("ctx.replies.query: is_internal must be true or false")
		}
		where, args = append(where, "is_internal = ?"), append(args, b)
	}
	if v, ok := filter["type"]; ok {
		switch v {
		case "reply":
			where, args = append(where, "is_internal = ? AND is_system = ?"), append(args, false, false)
		case "note":
			where, args = append(where, "is_internal = ? AND is_system = ?"), append(args, true, false)
		case "system":
			where, args = append(where, "is_system = ?"), append(args, true)
		default:
			return nil, fmt.Errorf("ctx.replies.query: unknown type %v", v)
		}
	}
	q := "SELECT id FROM " + d.TablePrefix + "replies"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY id LIMIT %d", maxQueryRows)
	ids, err := queryIDs(ctx, d.DB, q, args...)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, id := range ids {
		r, err := d.Store.GetReply(unscoped(ctx), id)
		if err != nil {
			return nil, err
		}
		if r != nil {
			out = append(out, replyObject(r))
		}
	}
	return out, nil
}

// Create adds a reply through the ticket service (reply.created), or a note
// (note.created) with is_internal. It needs ticket_id and body; the author is
// the signed-in user of the request being served, or nobody.
func (a RepliesAPI) Create(ctx context.Context, data map[string]any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, errors.New("ctx.replies.create requires an object")
	}
	if err := checkKeys("ctx.replies.create", data, "ticket_id", "body", "is_internal"); err != nil {
		return nil, err
	}
	ticketID, ok := idOf(data["ticket_id"])
	if !ok {
		return nil, errors.New("ctx.replies.create needs ticket_id")
	}
	body, _ := stringField(data, "body")
	if strings.TrimSpace(body) == "" {
		return nil, errors.New("ctx.replies.create needs a body")
	}
	internal, _ := data["is_internal"].(bool)
	actor := a.c.actor()
	var authorType *string
	if actor != nil {
		user := "User"
		authorType = &user
	}
	r, err := d.Tickets.AddReply(unscoped(ctx), ticketID, body, authorType, actor, internal)
	if err != nil {
		return nil, err
	}
	return replyObject(r), nil
}

// ContactsAPI is ctx.contacts: Escalated's contacts, not the host's users.
type ContactsAPI struct{ c *Context }

// Contacts is ctx.contacts.
func (c *Context) Contacts() ContactsAPI { return ContactsAPI{c: c} }

func contactObject(c *models.Contact) map[string]any {
	var name any
	if c.Name != nil {
		name = *c.Name
	}
	return jsonShaped(map[string]any{
		"id": c.ID, "email": c.Email, "name": name, "user_id": userIDValue(c.UserID),
		"created_at": isoTime(c.CreatedAt), "updated_at": isoTime(c.UpdatedAt),
	})
}

func (a ContactsAPI) contact(ctx context.Context, d *Domain, id any) (*models.Contact, error) {
	n, ok := idOf(id)
	if !ok {
		return nil, nil
	}
	var email string
	err := d.DB.QueryRowContext(ctx, sqldialect.Rebind(d.DB, "SELECT email FROM "+d.TablePrefix+"contacts WHERE id = ?"), n).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.Store.GetContactByEmail(unscoped(ctx), email)
}

// Find returns a contact by id, or nil.
func (a ContactsAPI) Find(ctx context.Context, id any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	c, err := a.contact(ctx, d, id)
	if err != nil || c == nil {
		return nil, err
	}
	return contactObject(c), nil
}

// FindByEmail returns the contact with this email, or nil.
func (a ContactsAPI) FindByEmail(ctx context.Context, email string) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	c, err := d.Store.GetContactByEmail(unscoped(ctx), models.NormalizeEmail(email))
	if err != nil || c == nil {
		return nil, err
	}
	return contactObject(c), nil
}

var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+$`)

// Create finds the contact with data's email, or creates it with data's name.
func (a ContactsAPI) Create(ctx context.Context, data map[string]any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	if err := checkKeys("ctx.contacts.create", data, "name", "email"); err != nil {
		return nil, err
	}
	email, _ := stringField(data, "email")
	email = models.NormalizeEmail(email)
	if !emailPattern.MatchString(email) {
		return nil, errors.New("ctx.contacts.create needs an email")
	}
	ctx = unscoped(ctx)
	existing, err := d.Store.GetContactByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return contactObject(existing), nil
	}
	c := &models.Contact{Email: email}
	if name, ok := stringField(data, "name"); ok && name != "" {
		c.Name = &name
	}
	if err := d.Store.CreateContact(ctx, c); err != nil {
		return nil, err
	}
	return contactObject(c), nil
}

// TagsAPI is ctx.tags.
type TagsAPI struct{ c *Context }

// Tags is ctx.tags.
func (c *Context) Tags() TagsAPI { return TagsAPI{c: c} }

func tagObject(t *models.Tag) map[string]any {
	var color any
	if t.Color != nil {
		color = *t.Color
	}
	return jsonShaped(map[string]any{"id": t.ID, "name": t.Name, "slug": t.Slug, "color": color})
}

// All lists every tag.
func (a TagsAPI) All(ctx context.Context) ([]map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	tags, err := d.Store.ListTags(unscoped(ctx))
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, t := range tags {
		out = append(out, tagObject(t))
	}
	return out, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Create adds a tag: name, and optionally slug and color.
func (a TagsAPI) Create(ctx context.Context, data map[string]any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	if err := checkKeys("ctx.tags.create", data, "name", "slug", "color"); err != nil {
		return nil, err
	}
	name, _ := stringField(data, "name")
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("ctx.tags.create needs a name")
	}
	slug, _ := stringField(data, "slug")
	if slug == "" {
		slug = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	}
	t := &models.Tag{Name: name, Slug: slug}
	if color, ok := stringField(data, "color"); ok && color != "" {
		t.Color = &color
	}
	if err := d.Store.CreateTag(unscoped(ctx), t); err != nil {
		return nil, err
	}
	return tagObject(t), nil
}

// DepartmentsAPI is ctx.departments.
type DepartmentsAPI struct{ c *Context }

// Departments is ctx.departments.
func (c *Context) Departments() DepartmentsAPI { return DepartmentsAPI{c: c} }

func departmentObject(dep *models.Department) map[string]any {
	var description any
	if dep.Description != nil {
		description = *dep.Description
	}
	return jsonShaped(map[string]any{"id": dep.ID, "name": dep.Name, "slug": dep.Slug, "description": description, "is_active": dep.IsActive})
}

// All lists every department.
func (a DepartmentsAPI) All(ctx context.Context) ([]map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	deps, err := d.Store.ListDepartments(unscoped(ctx), false)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, dep := range deps {
		out = append(out, departmentObject(dep))
	}
	return out, nil
}

// Find returns a department by id, or nil.
func (a DepartmentsAPI) Find(ctx context.Context, id any) (map[string]any, error) {
	d, err := a.c.domain()
	if err != nil {
		return nil, err
	}
	n, ok := idOf(id)
	if !ok {
		return nil, nil
	}
	dep, err := d.Store.GetDepartment(unscoped(ctx), n)
	if err != nil || dep == nil {
		return nil, err
	}
	return departmentObject(dep), nil
}

// AgentsAPI is ctx.agents: the host's agents, from Config.SkillAgentDirectory.
type AgentsAPI struct{ c *Context }

// Agents is ctx.agents.
func (c *Context) Agents() AgentsAPI { return AgentsAPI{c: c} }

// All lists the agents.
func (a AgentsAPI) All(ctx context.Context) ([]Agent, error) {
	d := a.c.host.domain
	if d == nil || d.Agents == nil {
		return []Agent{}, nil
	}
	agents, err := d.Agents(ctx)
	if agents == nil {
		agents = []Agent{}
	}
	return agents, err
}

// Find returns an agent by id, or nil.
func (a AgentsAPI) Find(ctx context.Context, id any) (*Agent, error) {
	agents, err := a.All(ctx)
	if err != nil {
		return nil, err
	}
	want := fmt.Sprint(id)
	if f, ok := id.(float64); ok {
		want = strconv.FormatFloat(f, 'f', -1, 64)
	}
	for i := range agents {
		if fmt.Sprint(agents[i].ID) == want {
			return &agents[i], nil
		}
	}
	return nil, nil
}

// BroadcastAPI is ctx.broadcast, through Config.Plugins.Broadcaster. Without
// one it does nothing.
type BroadcastAPI struct{ c *Context }

// Broadcast is ctx.broadcast.
func (c *Context) Broadcast() BroadcastAPI { return BroadcastAPI{c: c} }

func (a BroadcastAPI) publish(channel, event string, data any) error {
	d := a.c.host.domain
	if d == nil || d.Broadcaster == nil {
		a.c.Log.Debug("ctx.broadcast is a no-op on this host: no broadcaster is configured")
		return nil
	}
	if _, err := d.Broadcaster.Publish(channel, event, data); err != nil {
		a.c.Log.Debug("ctx.broadcast: " + err.Error())
	}
	return nil
}

// ToChannel publishes to a channel.
func (a BroadcastAPI) ToChannel(_ context.Context, channel, event string, data any) error {
	return a.publish(channel, event, data)
}

// ToUser publishes to a user's channel.
func (a BroadcastAPI) ToUser(_ context.Context, userID any, event string, data any) error {
	id := fmt.Sprint(userID)
	if f, ok := userID.(float64); ok {
		id = strconv.FormatFloat(f, 'f', -1, 64)
	}
	return a.publish(services.UserChannel(models.UserID(id)), event, data)
}

// ToTicket publishes to a ticket's channel.
func (a BroadcastAPI) ToTicket(_ context.Context, ticketID any, event string, data any) error {
	n, ok := idOf(ticketID)
	if !ok {
		return errors.New("ctx.broadcast.toTicket needs a ticket id")
	}
	return a.publish(services.TicketChannel(n), event, data)
}
