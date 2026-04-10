package models

import (
	"fmt"
	"time"
)

// EmailChannel represents an email address configuration for a department.
type EmailChannel struct {
	ID             int64     `json:"id"`
	EmailAddress   string    `json:"email_address"`
	DisplayName    *string   `json:"display_name,omitempty"`
	DepartmentID   *int64    `json:"department_id,omitempty"`
	IsDefault      bool      `json:"is_default"`
	IsVerified     bool      `json:"is_verified"`
	DkimStatus     string    `json:"dkim_status"`
	DkimPublicKey  *string   `json:"dkim_public_key,omitempty"`
	DkimSelector   *string   `json:"dkim_selector,omitempty"`
	ReplyToAddress *string   `json:"reply_to_address,omitempty"`
	SmtpProtocol   string    `json:"smtp_protocol"`
	SmtpHost       *string   `json:"smtp_host,omitempty"`
	SmtpPort       *int      `json:"smtp_port,omitempty"`
	SmtpUsername   *string   `json:"smtp_username,omitempty"`
	SmtpPassword   *string   `json:"smtp_password,omitempty"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// FormattedSender returns the formatted sender string.
func (e *EmailChannel) FormattedSender() string {
	if e.DisplayName != nil && *e.DisplayName != "" {
		return fmt.Sprintf("%s <%s>", *e.DisplayName, e.EmailAddress)
	}
	return e.EmailAddress
}
