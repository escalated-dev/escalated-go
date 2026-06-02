package models

import "time"

// TicketSubject is implemented by host-app models attached to a ticket as its
// subject (the entity the ticket is about — a Project, Customer, asset, …).
type TicketSubject interface {
	TicketSubjectTitle() string
	TicketSubjectSubtitle() *string
	TicketSubjectURL() *string
	TicketSubjectColor() *string
	TicketSubjectIcon() *string
}

// TicketSubjectLink is a join row linking a ticket to one host-app subject.
type TicketSubjectLink struct {
	ID          int64     `json:"id"`
	TicketID    int64     `json:"ticket_id"`
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	Role        *string   `json:"role,omitempty"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TicketSubjectView is the serialized subject payload on ticket JSON responses.
type TicketSubjectView struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	Role     *string `json:"role,omitempty"`
	Title    string  `json:"title"`
	Subtitle *string `json:"subtitle,omitempty"`
	URL      *string `json:"url,omitempty"`
	Color    *string `json:"color,omitempty"`
	Icon     *string `json:"icon,omitempty"`
	Missing  bool    `json:"missing"`
}

// SerializeTicketSubjects maps links through resolver into API views.
// When resolver is nil or returns false, title falls back to type#id and missing is true.
func SerializeTicketSubjects(
	links []*TicketSubjectLink,
	resolver func(subjectType, subjectID string) (TicketSubject, bool),
) []TicketSubjectView {
	if len(links) == 0 {
		return nil
	}
	out := make([]TicketSubjectView, 0, len(links))
	for _, link := range links {
		view := TicketSubjectView{
			Type: link.SubjectType,
			ID:   link.SubjectID,
			Role: link.Role,
		}
		if resolver != nil {
			if subject, ok := resolver(link.SubjectType, link.SubjectID); ok && subject != nil {
				view.Title = subject.TicketSubjectTitle()
				view.Subtitle = subject.TicketSubjectSubtitle()
				view.URL = subject.TicketSubjectURL()
				view.Color = subject.TicketSubjectColor()
				view.Icon = subject.TicketSubjectIcon()
			} else {
				view.Title = link.SubjectType + "#" + link.SubjectID
				view.Missing = true
			}
		} else {
			view.Title = link.SubjectType + "#" + link.SubjectID
			view.Missing = true
		}
		out = append(out, view)
	}
	return out
}
