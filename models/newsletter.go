// Package models contains the Escalated domain entities.
//
// The newsletter system entities are defined here (NewsletterList,
// NewsletterListMember, NewsletterTemplate, Newsletter, NewsletterDelivery)
// and the Contact struct in contact.go gains a MarketingOptOutAt field.
// Feature gating happens at the dispatcher level via config.EnableNewsletters.
package models

import "time"

// NewsletterListKind identifies how a list's recipients are resolved.
type NewsletterListKind string

const (
	NewsletterListStatic  NewsletterListKind = "static"
	NewsletterListDynamic NewsletterListKind = "dynamic"
)

// NewsletterList is the targeted recipient bucket for a newsletter.
type NewsletterList struct {
	ID          int64              `json:"id"`
	Name        string             `json:"name"`
	Description *string            `json:"description,omitempty"`
	Kind        NewsletterListKind `json:"kind"`
	FilterJSON  map[string]any     `json:"filter_json,omitempty"`
	CreatedBy   *UserID            `json:"created_by,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// NewsletterListMember binds a contact to a static list.
type NewsletterListMember struct {
	ID        int64     `json:"id"`
	ListID    int64     `json:"list_id"`
	ContactID int64     `json:"contact_id"`
	AddedAt   time.Time `json:"added_at"`
	AddedBy   *UserID   `json:"added_by,omitempty"`
}

// NewsletterTemplate is a reusable Markdown body + theme combination.
type NewsletterTemplate struct {
	ID                int64          `json:"id"`
	Name              string         `json:"name"`
	Theme             string         `json:"theme"`
	SubjectTemplate   *string        `json:"subject_template,omitempty"`
	BodyMarkdown      string         `json:"body_markdown"`
	MergeFieldsSchema map[string]any `json:"merge_fields_schema,omitempty"`
	CreatedBy         *UserID        `json:"created_by,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// NewsletterStatus is the lifecycle status of a newsletter.
type NewsletterStatus string

const (
	NewsletterDraft     NewsletterStatus = "draft"
	NewsletterScheduled NewsletterStatus = "scheduled"
	NewsletterSending   NewsletterStatus = "sending"
	NewsletterSent      NewsletterStatus = "sent"
	NewsletterPaused    NewsletterStatus = "paused"
	NewsletterFailed    NewsletterStatus = "failed"
)

// Newsletter is one campaign send.
type Newsletter struct {
	ID                int64            `json:"id"`
	Subject           string           `json:"subject"`
	FromEmail         string           `json:"from_email"`
	FromName          *string          `json:"from_name,omitempty"`
	ReplyTo           *string          `json:"reply_to,omitempty"`
	TargetListID      int64            `json:"target_list_id"`
	TemplateID        *int64           `json:"template_id,omitempty"`
	Theme             *string          `json:"theme,omitempty"`
	BodyMarkdown      *string          `json:"body_markdown,omitempty"`
	Status            NewsletterStatus `json:"status"`
	ScheduledAt       *time.Time       `json:"scheduled_at,omitempty"`
	SentAt            *time.Time       `json:"sent_at,omitempty"`
	CreatedBy         *UserID          `json:"created_by,omitempty"`
	SentBy            *UserID          `json:"sent_by,omitempty"`
	SummaryTotal      int              `json:"summary_total"`
	SummarySent       int              `json:"summary_sent"`
	SummaryOpened     int              `json:"summary_opened"`
	SummaryClicked    int              `json:"summary_clicked"`
	SummaryBounced    int              `json:"summary_bounced"`
	SummaryComplained int              `json:"summary_complained"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// NewsletterDeliveryStatus is the lifecycle status of a single delivery row.
type NewsletterDeliveryStatus string

const (
	DeliveryPending    NewsletterDeliveryStatus = "pending"
	DeliveryQueued     NewsletterDeliveryStatus = "queued"
	DeliverySent       NewsletterDeliveryStatus = "sent"
	DeliveryBounced    NewsletterDeliveryStatus = "bounced"
	DeliveryComplained NewsletterDeliveryStatus = "complained"
	DeliverySuppressed NewsletterDeliveryStatus = "suppressed"
	DeliveryFailed     NewsletterDeliveryStatus = "failed"
)

// NewsletterDelivery is one row per recipient per campaign.
type NewsletterDelivery struct {
	ID            int64                    `json:"id"`
	NewsletterID  int64                    `json:"newsletter_id"`
	ContactID     int64                    `json:"contact_id"`
	EmailAtSend   string                   `json:"email_at_send"`
	Status        NewsletterDeliveryStatus `json:"status"`
	TrackingToken string                   `json:"tracking_token"`
	SentAt        *time.Time               `json:"sent_at,omitempty"`
	OpenedAt      *time.Time               `json:"opened_at,omitempty"`
	LastClickedAt *time.Time               `json:"last_clicked_at,omitempty"`
	ClicksCount   int                      `json:"clicks_count"`
	BounceReason  *string                  `json:"bounce_reason,omitempty"`
	FailureReason *string                  `json:"failure_reason,omitempty"`
	AttemptCount  int                      `json:"attempt_count"`
	ClaimedAt     *time.Time               `json:"claimed_at,omitempty"`
	NextAttemptAt *time.Time               `json:"next_attempt_at,omitempty"`
	IsTest        bool                     `json:"is_test"`
	CreatedAt     time.Time                `json:"created_at"`
}
