package services

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/store"
)

var (
	// ErrTicketSubjectTypeNotAllowed is returned when a subject type is outside the allowlist.
	ErrTicketSubjectTypeNotAllowed = errors.New("subject type is not an allowed ticket subject")
	// ErrTicketSubjectAPIDisabled is returned when API attach is attempted with an empty allowlist.
	ErrTicketSubjectAPIDisabled = errors.New("ticket subject API attaching is disabled (empty allowlist)")
	// ErrTicketSubjectNotFound is returned when the resolver cannot find the host subject.
	ErrTicketSubjectNotFound = errors.New("no matching subject was found")
)

// TicketSubjectRef identifies one subject to attach or sync.
type TicketSubjectRef struct {
	Type string
	ID   string
	Role *string
}

// TicketSubjectService manages ticket subject links.
type TicketSubjectService struct {
	store     store.Store
	allowlist []string
	resolver  func(subjectType, subjectID string) (models.TicketSubject, bool)
}

// NewTicketSubjectService creates a TicketSubjectService.
func NewTicketSubjectService(
	s store.Store,
	allowlist []string,
	resolver func(subjectType, subjectID string) (models.TicketSubject, bool),
) *TicketSubjectService {
	return &TicketSubjectService{store: s, allowlist: allowlist, resolver: resolver}
}

// AllowedType reports whether subjectType is in the configured allowlist.
func AllowedTicketSubjectType(allowlist []string, subjectType string) bool {
	return slices.Contains(allowlist, subjectType)
}

// AttachSubject links a host entity to a ticket (idempotent on ticket+type+id).
// When enforceAllowlist is true, an empty allowlist rejects all types and a
// non-empty allowlist must include subjectType. When a resolver is configured,
// the subject must resolve or ErrTicketSubjectNotFound is returned.
func (ss *TicketSubjectService) AttachSubject(
	ctx context.Context,
	ticketID int64,
	subjectType, subjectID string,
	role *string,
	position *int,
	enforceAllowlist bool,
) (*models.TicketSubjectLink, error) {
	if enforceAllowlist {
		if len(ss.allowlist) == 0 {
			return nil, ErrTicketSubjectAPIDisabled
		}
		if !AllowedTicketSubjectType(ss.allowlist, subjectType) {
			return nil, fmt.Errorf("%w: %s", ErrTicketSubjectTypeNotAllowed, subjectType)
		}
	} else if len(ss.allowlist) > 0 && !AllowedTicketSubjectType(ss.allowlist, subjectType) {
		return nil, fmt.Errorf("%w: %s", ErrTicketSubjectTypeNotAllowed, subjectType)
	}

	if ss.resolver != nil {
		if _, ok := ss.resolver(subjectType, subjectID); !ok {
			return nil, ErrTicketSubjectNotFound
		}
	}

	pos := 0
	if position != nil {
		pos = *position
	} else {
		max, err := ss.store.MaxTicketSubjectPosition(ctx, ticketID)
		if err != nil {
			return nil, err
		}
		pos = max + 1
	}

	link := &models.TicketSubjectLink{
		TicketID:    ticketID,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Role:        role,
		Position:    pos,
	}
	if err := ss.store.UpsertTicketSubjectLink(ctx, link); err != nil {
		return nil, err
	}
	return link, nil
}

// DetachSubject removes a subject link by its join-row id. Returns false when not found.
func (ss *TicketSubjectService) DetachSubject(ctx context.Context, ticketID, linkID int64) (bool, error) {
	link, err := ss.store.GetTicketSubjectLink(ctx, linkID)
	if err != nil {
		return false, err
	}
	if link == nil || link.TicketID != ticketID {
		return false, nil
	}
	if err := ss.store.DeleteTicketSubjectLink(ctx, linkID); err != nil {
		return false, err
	}
	return true, nil
}

// SyncSubjects replaces all subjects on a ticket, preserving order.
func (ss *TicketSubjectService) SyncSubjects(
	ctx context.Context,
	ticketID int64,
	subjects []TicketSubjectRef,
	enforceAllowlist bool,
) error {
	if err := ss.store.DeleteTicketSubjectLinksByTicket(ctx, ticketID); err != nil {
		return err
	}
	for i, ref := range subjects {
		pos := i
		if _, err := ss.AttachSubject(ctx, ticketID, ref.Type, ref.ID, ref.Role, &pos, enforceAllowlist); err != nil {
			return err
		}
	}
	return nil
}

// ListViews returns serialized subjects for a ticket.
func (ss *TicketSubjectService) ListViews(ctx context.Context, ticketID int64) ([]models.TicketSubjectView, error) {
	links, err := ss.store.ListTicketSubjectLinks(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	return models.SerializeTicketSubjects(links, ss.resolver), nil
}
