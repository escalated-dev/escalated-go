package services

import (
	"html/template"
	"strings"
	"testing"
)

func TestGenerateMessageID(t *testing.T) {
	tests := []struct {
		name      string
		ticketRef string
		replyID   int64
		domain    string
		want      string
	}{
		{
			name:      "standard message ID",
			ticketRef: "ESC-2604-A1B2C3",
			replyID:   42,
			domain:    "support.example.com",
			want:      "<ticket-esc-2604-a1b2c3-reply-42@support.example.com>",
		},
		{
			name:      "different domain",
			ticketRef: "ACME-0001",
			replyID:   1,
			domain:    "acme.io",
			want:      "<ticket-acme-0001-reply-1@acme.io>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateMessageID(tt.ticketRef, tt.replyID, tt.domain)
			if got != tt.want {
				t.Errorf("GenerateMessageID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateTicketMessageID(t *testing.T) {
	got := GenerateTicketMessageID("ESC-0001", "example.com")
	want := "<ticket-esc-0001@example.com>"
	if got != want {
		t.Errorf("GenerateTicketMessageID() = %q, want %q", got, want)
	}
}

func TestBuildThreadingHeaders(t *testing.T) {
	tests := []struct {
		name            string
		ticketRef       string
		replyID         int64
		domain          string
		parentMessageID string
		previousRefs    []string
		wantInReplyTo   string
		wantRefsLen     int
		wantRefsContain string
	}{
		{
			name:            "first reply in thread",
			ticketRef:       "ESC-0001",
			replyID:         1,
			domain:          "example.com",
			parentMessageID: "<ticket-esc-0001@example.com>",
			previousRefs:    nil,
			wantInReplyTo:   "<ticket-esc-0001@example.com>",
			wantRefsLen:     1,
			wantRefsContain: "<ticket-esc-0001@example.com>",
		},
		{
			name:            "subsequent reply with history",
			ticketRef:       "ESC-0001",
			replyID:         3,
			domain:          "example.com",
			parentMessageID: "<ticket-esc-0001-reply-2@example.com>",
			previousRefs:    []string{"<ticket-esc-0001@example.com>", "<ticket-esc-0001-reply-1@example.com>"},
			wantInReplyTo:   "<ticket-esc-0001-reply-2@example.com>",
			wantRefsLen:     3,
			wantRefsContain: "<ticket-esc-0001-reply-2@example.com>",
		},
		{
			name:            "deduplicates references",
			ticketRef:       "ESC-0001",
			replyID:         2,
			domain:          "example.com",
			parentMessageID: "<ticket-esc-0001@example.com>",
			previousRefs:    []string{"<ticket-esc-0001@example.com>"},
			wantInReplyTo:   "<ticket-esc-0001@example.com>",
			wantRefsLen:     1,
		},
		{
			name:          "empty parent",
			ticketRef:     "ESC-0001",
			replyID:       1,
			domain:        "example.com",
			wantInReplyTo: "",
			wantRefsLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := BuildThreadingHeaders(tt.ticketRef, tt.replyID, tt.domain, tt.parentMessageID, tt.previousRefs)

			if headers.InReplyTo != tt.wantInReplyTo {
				t.Errorf("InReplyTo = %q, want %q", headers.InReplyTo, tt.wantInReplyTo)
			}
			if len(headers.References) != tt.wantRefsLen {
				t.Errorf("References length = %d, want %d", len(headers.References), tt.wantRefsLen)
			}
			if tt.wantRefsContain != "" {
				found := false
				for _, ref := range headers.References {
					if ref == tt.wantRefsContain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("References should contain %q", tt.wantRefsContain)
				}
			}
			// MessageID should always be set
			if headers.MessageID == "" {
				t.Error("MessageID should not be empty")
			}
		})
	}
}

func TestDefaultEmailBranding(t *testing.T) {
	b := DefaultEmailBranding()
	if b.AccentColor == "" {
		t.Error("AccentColor should have a default")
	}
	if b.FooterText == "" {
		t.Error("FooterText should have a default")
	}
}

func TestEmailServiceRenderHTML(t *testing.T) {
	tests := []struct {
		name        string
		branding    EmailBranding
		data        EmailData
		wantContain []string
	}{
		{
			name: "basic email with branding",
			branding: EmailBranding{
				AccentColor: "#FF5733",
				FooterText:  "Acme Corp Support",
				CompanyName: "Acme",
			},
			data: EmailData{
				Subject:   "Your ticket update",
				Body:      template.HTML("<p>Hello, your ticket has been updated.</p>"),
				TicketRef: "ESC-0001",
				TicketURL: "https://support.acme.com/tickets/1",
			},
			wantContain: []string{
				"#FF5733",
				"Acme Corp Support",
				"Your ticket update",
				"ESC-0001",
				"https://support.acme.com/tickets/1",
				"View Ticket",
			},
		},
		{
			name: "email with logo",
			branding: EmailBranding{
				LogoURL:     "https://acme.com/logo.png",
				AccentColor: "#000000",
				CompanyName: "Acme",
			},
			data: EmailData{
				Subject: "Test",
				Body:    template.HTML("body content"),
			},
			wantContain: []string{
				"https://acme.com/logo.png",
				"body content",
			},
		},
		{
			name:     "email with unsubscribe link",
			branding: DefaultEmailBranding(),
			data: EmailData{
				Subject:  "Update",
				Body:     template.HTML("text"),
				UnsubURL: "https://example.com/unsub",
			},
			wantContain: []string{
				"https://example.com/unsub",
				"Unsubscribe",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEmailService(tt.branding, "example.com")
			html, err := svc.RenderHTML(tt.data)
			if err != nil {
				t.Fatalf("RenderHTML() error: %v", err)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(html, want) {
					t.Errorf("rendered HTML should contain %q", want)
				}
			}
			// Should be valid HTML structure
			if !strings.Contains(html, "<!DOCTYPE html>") {
				t.Error("should have DOCTYPE")
			}
			if !strings.Contains(html, "</html>") {
				t.Error("should have closing html tag")
			}
		})
	}
}

func TestEmailServiceThreadingHeaders(t *testing.T) {
	svc := NewEmailService(DefaultEmailBranding(), "mail.example.com")

	// Test ticket headers
	ticketHeaders := svc.ThreadingHeadersForTicket("ESC-0001")
	if ticketHeaders.MessageID != "<ticket-esc-0001@mail.example.com>" {
		t.Errorf("ticket MessageID = %q", ticketHeaders.MessageID)
	}

	// Test reply headers
	replyHeaders := svc.ThreadingHeadersForReply("ESC-0001", 5, ticketHeaders.MessageID, nil)
	if replyHeaders.InReplyTo != ticketHeaders.MessageID {
		t.Errorf("reply InReplyTo = %q, want %q", replyHeaders.InReplyTo, ticketHeaders.MessageID)
	}
	if len(replyHeaders.References) != 1 {
		t.Errorf("reply References len = %d, want 1", len(replyHeaders.References))
	}
}
