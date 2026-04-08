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

	// Activities
	CreateActivity(ctx context.Context, a *models.Activity) error
	ListActivities(ctx context.Context, ticketID int64, limit int) ([]*models.Activity, error)
}
