// Package store defines the Store interface for Escalated data persistence
// and provides PostgreSQL and SQLite implementations.
package store

import (
	"context"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// Store is the interface that all Escalated storage backends must implement.
type Store interface {
	// Tickets
	CreateTicket(ctx context.Context, t *models.Ticket) error
	GetTicket(ctx context.Context, id int64) (*models.Ticket, error)
	GetTicketByReference(ctx context.Context, ref string) (*models.Ticket, error)
	UpdateTicket(ctx context.Context, t *models.Ticket) error
	ListTickets(ctx context.Context, f models.TicketFilters) ([]*models.Ticket, int, error)
	DeleteTicket(ctx context.Context, id int64) error

	// Replies
	CreateReply(ctx context.Context, r *models.Reply) error
	GetReply(ctx context.Context, id int64) (*models.Reply, error)
	ListReplies(ctx context.Context, f models.ReplyFilters) ([]*models.Reply, error)
	UpdateReply(ctx context.Context, r *models.Reply) error
	DeleteReply(ctx context.Context, id int64) error

	// Departments
	CreateDepartment(ctx context.Context, d *models.Department) error
	GetDepartment(ctx context.Context, id int64) (*models.Department, error)
	ListDepartments(ctx context.Context, activeOnly bool) ([]*models.Department, error)
	UpdateDepartment(ctx context.Context, d *models.Department) error
	DeleteDepartment(ctx context.Context, id int64) error

	// Tags
	CreateTag(ctx context.Context, t *models.Tag) error
	GetTag(ctx context.Context, id int64) (*models.Tag, error)
	ListTags(ctx context.Context) ([]*models.Tag, error)
	UpdateTag(ctx context.Context, t *models.Tag) error
	DeleteTag(ctx context.Context, id int64) error

	// Ticket-Tag associations
	AddTagToTicket(ctx context.Context, ticketID, tagID int64) error
	RemoveTagFromTicket(ctx context.Context, ticketID, tagID int64) error
	GetTicketTags(ctx context.Context, ticketID int64) ([]*models.Tag, error)

	// SLA Policies
	CreateSLAPolicy(ctx context.Context, s *models.SLAPolicy) error
	GetSLAPolicy(ctx context.Context, id int64) (*models.SLAPolicy, error)
	GetDefaultSLAPolicy(ctx context.Context) (*models.SLAPolicy, error)
	ListSLAPolicies(ctx context.Context, activeOnly bool) ([]*models.SLAPolicy, error)
	UpdateSLAPolicy(ctx context.Context, s *models.SLAPolicy) error
	DeleteSLAPolicy(ctx context.Context, id int64) error

	// Snooze
	ListSnoozedDueBefore(ctx context.Context, before time.Time) ([]*models.Ticket, error)
	// Saved Views
	CreateSavedView(ctx context.Context, sv *models.SavedView) error
	GetSavedView(ctx context.Context, id int64) (*models.SavedView, error)
	ListSavedViews(ctx context.Context, userID int64, includeShared bool) ([]*models.SavedView, error)
	UpdateSavedView(ctx context.Context, sv *models.SavedView) error
	DeleteSavedView(ctx context.Context, id int64) error
	ReorderSavedViews(ctx context.Context, userID int64, ids []int64) error

	// Chat Sessions
	CreateChatSession(ctx context.Context, s *models.ChatSession) error
	GetChatSession(ctx context.Context, id int64) (*models.ChatSession, error)
	GetChatSessionByTicket(ctx context.Context, ticketID int64) (*models.ChatSession, error)
	UpdateChatSession(ctx context.Context, s *models.ChatSession) error
	ListChatSessions(ctx context.Context, f models.ChatSessionFilters) ([]*models.ChatSession, error)

	// Chat Routing Rules
	CreateChatRoutingRule(ctx context.Context, r *models.ChatRoutingRule) error
	GetChatRoutingRule(ctx context.Context, id int64) (*models.ChatRoutingRule, error)
	ListActiveChatRoutingRules(ctx context.Context, departmentID *int64) ([]*models.ChatRoutingRule, error)
	UpdateChatRoutingRule(ctx context.Context, r *models.ChatRoutingRule) error
	DeleteChatRoutingRule(ctx context.Context, id int64) error
	CountActiveChatsForAgent(ctx context.Context, agentID int64) (int, error)

	// Chat Messages
	CreateChatMessage(ctx context.Context, m *models.ChatMessage) error
	ListChatMessages(ctx context.Context, chatSessionID int64) ([]models.ChatMessage, error)

	// Ticket Counts / Relations
	CountTicketsByRequester(ctx context.Context, requesterType string, requesterID int64) (int, error)
	ListRelatedTickets(ctx context.Context, ticketID int64) ([]models.RelatedTicket, error)

	// Activities
	CreateActivity(ctx context.Context, a *models.Activity) error
	ListActivities(ctx context.Context, ticketID int64, limit int) ([]*models.Activity, error)

	// Attachments
	CreateAttachment(ctx context.Context, a *models.Attachment) error
	GetAttachmentByID(ctx context.Context, id int64) (*models.Attachment, error)
	GetAttachmentsByTicketID(ctx context.Context, ticketID int64) ([]*models.Attachment, error)
	GetAttachmentsByReplyID(ctx context.Context, replyID int64) ([]*models.Attachment, error)

	// Contacts (Pattern B public-ticket dedupe — see docs/superpowers/plans/2026-04-24-public-tickets-rollout-status.md)
	GetContactByEmail(ctx context.Context, normalizedEmail string) (*models.Contact, error)
	CreateContact(ctx context.Context, c *models.Contact) error
	UpdateContactName(ctx context.Context, id int64, name string) error

	// Settings — key/value runtime configuration.
	// GetSetting returns "" with nil error when the key is missing so
	// callers can chain a default value without checking a boolean.
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}
