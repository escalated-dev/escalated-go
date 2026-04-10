package services

import (
	"fmt"
	"net"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// EmailChannelStore defines the persistence interface for email channels.
type EmailChannelStore interface {
	CreateEmailChannel(ch *models.EmailChannel) error
	FindEmailChannelByAddress(addr string) (*models.EmailChannel, error)
	FindEmailChannelsByDepartment(departmentID int64) ([]*models.EmailChannel, error)
	GetDefaultEmailChannel() (*models.EmailChannel, error)
	ClearDefaultEmailChannels() error
	UpdateEmailChannel(ch *models.EmailChannel) error
	DeleteEmailChannel(id int64) error
}

// EmailChannelService manages email channel addresses and DKIM validation.
type EmailChannelService struct {
	store EmailChannelStore
}

// NewEmailChannelService creates a new EmailChannelService.
func NewEmailChannelService(store EmailChannelStore) *EmailChannelService {
	return &EmailChannelService{store: store}
}

// Create adds a new email channel.
func (s *EmailChannelService) Create(ch *models.EmailChannel) error {
	if ch.DkimStatus == "" {
		ch.DkimStatus = "pending"
	}
	if ch.SmtpProtocol == "" {
		ch.SmtpProtocol = "tls"
	}
	ch.IsActive = true
	return s.store.CreateEmailChannel(ch)
}

// FindByAddress looks up a channel by email address.
func (s *EmailChannelService) FindByAddress(addr string) (*models.EmailChannel, error) {
	return s.store.FindEmailChannelByAddress(addr)
}

// FindByDepartment returns all channels for a department.
func (s *EmailChannelService) FindByDepartment(departmentID int64) ([]*models.EmailChannel, error) {
	return s.store.FindEmailChannelsByDepartment(departmentID)
}

// GetDefault returns the default active email channel.
func (s *EmailChannelService) GetDefault() (*models.EmailChannel, error) {
	return s.store.GetDefaultEmailChannel()
}

// SetDefault sets a channel as the default, clearing others.
func (s *EmailChannelService) SetDefault(ch *models.EmailChannel) error {
	if err := s.store.ClearDefaultEmailChannels(); err != nil {
		return err
	}
	ch.IsDefault = true
	return s.store.UpdateEmailChannel(ch)
}

// DkimVerifyResult holds the result of a DKIM verification.
type DkimVerifyResult struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
	DNSHost  string `json:"dns_host"`
	Verified bool   `json:"verified"`
}

// VerifyDkim checks DNS for DKIM records matching the channel config.
func (s *EmailChannelService) VerifyDkim(ch *models.EmailChannel) (*DkimVerifyResult, error) {
	parts := strings.SplitN(ch.EmailAddress, "@", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid email address")
	}
	domain := parts[1]
	selector := "escalated"
	if ch.DkimSelector != nil && *ch.DkimSelector != "" {
		selector = *ch.DkimSelector
	}
	dnsHost := fmt.Sprintf("%s._domainkey.%s", selector, domain)

	verified := false
	records, err := net.LookupTXT(dnsHost)
	if err == nil && ch.DkimPublicKey != nil {
		for _, r := range records {
			if strings.Contains(r, "v=DKIM1") && strings.Contains(r, *ch.DkimPublicKey) {
				verified = true
				break
			}
		}
	}

	status := "failed"
	if verified {
		status = "verified"
	}
	ch.DkimStatus = status
	ch.IsVerified = verified
	_ = s.store.UpdateEmailChannel(ch)

	return &DkimVerifyResult{
		Domain:   domain,
		Selector: selector,
		DNSHost:  dnsHost,
		Verified: verified,
	}, nil
}

// Delete removes an email channel.
func (s *EmailChannelService) Delete(id int64) error {
	return s.store.DeleteEmailChannel(id)
}
