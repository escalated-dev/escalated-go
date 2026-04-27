package email

import (
	"testing"
)

func mailgunFormData() map[string]string {
	return map[string]string{
		"sender":      "customer@example.com",
		"from":        "Customer <customer@example.com>",
		"recipient":   "support+abc@support.example.com",
		"To":          "support+abc@support.example.com",
		"subject":     "[ESC-00042] Help",
		"body-plain":  "Plain body",
		"body-html":   "<p>HTML body</p>",
		"Message-Id":  "<mailgun-incoming@mail.client>",
		"In-Reply-To": "<ticket-42@support.example.com>",
		"References":  "<ticket-42@support.example.com>",
		"attachments": `[{"name":"report.pdf","content-type":"application/pdf","size":5120,"url":"https://mailgun.example/att/abc"}]`,
	}
}

func TestMailgunParser_Name(t *testing.T) {
	if got := (MailgunInboundParser{}).Name(); got != "mailgun" {
		t.Fatalf("Name() = %q, want mailgun", got)
	}
}

func TestMailgunParser_ExtractsCoreFields(t *testing.T) {
	m, err := MailgunInboundParser{}.Parse(mailgunFormData())
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	if m.FromEmail != "customer@example.com" {
		t.Errorf("FromEmail = %q", m.FromEmail)
	}
	if m.FromName != "Customer" {
		t.Errorf("FromName = %q", m.FromName)
	}
	if m.ToEmail != "support+abc@support.example.com" {
		t.Errorf("ToEmail = %q", m.ToEmail)
	}
	if m.Subject != "[ESC-00042] Help" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if m.BodyText != "Plain body" {
		t.Errorf("BodyText = %q", m.BodyText)
	}
	if m.BodyHTML != "<p>HTML body</p>" {
		t.Errorf("BodyHTML = %q", m.BodyHTML)
	}
}

func TestMailgunParser_ExtractsThreadingHeaders(t *testing.T) {
	m, _ := MailgunInboundParser{}.Parse(mailgunFormData())
	if m.InReplyTo != "<ticket-42@support.example.com>" {
		t.Errorf("InReplyTo = %q", m.InReplyTo)
	}
	if m.References != "<ticket-42@support.example.com>" {
		t.Errorf("References = %q", m.References)
	}
}

func TestMailgunParser_ProviderHostedAttachments(t *testing.T) {
	m, _ := MailgunInboundParser{}.Parse(mailgunFormData())
	if len(m.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d", len(m.Attachments))
	}
	a := m.Attachments[0]
	if a.Name != "report.pdf" {
		t.Errorf("Name = %q", a.Name)
	}
	if a.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q", a.ContentType)
	}
	if a.SizeBytes != 5120 {
		t.Errorf("SizeBytes = %d", a.SizeBytes)
	}
	if a.DownloadURL != "https://mailgun.example/att/abc" {
		t.Errorf("DownloadURL = %q", a.DownloadURL)
	}
	// Mailgun hosts content — no inline bytes.
	if a.Content != nil {
		t.Errorf("Content should be nil, got %d bytes", len(a.Content))
	}
}

func TestMailgunParser_MalformedAttachmentsJson(t *testing.T) {
	data := mailgunFormData()
	data["attachments"] = "not json"
	m, _ := MailgunInboundParser{}.Parse(data)
	if len(m.Attachments) != 0 {
		t.Errorf("len(Attachments) = %d, want 0", len(m.Attachments))
	}
}

func TestMailgunParser_FallsBackSenderToFrom(t *testing.T) {
	data := map[string]string{
		"from":      "only-from@example.com",
		"recipient": "support@example.com",
		"subject":   "hi",
	}
	m, _ := MailgunInboundParser{}.Parse(data)
	if m.FromEmail != "only-from@example.com" {
		t.Errorf("FromEmail = %q", m.FromEmail)
	}
}

func TestMailgunParser_ExtractFromNameReturnsEmptyWithoutAngleBrackets(t *testing.T) {
	data := map[string]string{
		"sender":    "bareemail@example.com",
		"from":      "bareemail@example.com",
		"recipient": "support@example.com",
		"subject":   "hi",
	}
	m, _ := MailgunInboundParser{}.Parse(data)
	if m.FromName != "" {
		t.Errorf("FromName should be empty, got %q", m.FromName)
	}
}

func TestMailgunParser_StripsQuotesFromFromName(t *testing.T) {
	data := map[string]string{
		"sender":    "jane@example.com",
		"from":      `"Jane Doe" <jane@example.com>`,
		"recipient": "support@example.com",
		"subject":   "hi",
	}
	m, _ := MailgunInboundParser{}.Parse(data)
	if m.FromName != "Jane Doe" {
		t.Errorf("FromName = %q, want Jane Doe", m.FromName)
	}
}

func TestMailgunParser_AcceptsMapStringInterface(t *testing.T) {
	data := map[string]interface{}{
		"sender":    "x@y.com",
		"recipient": "z@w.com",
		"subject":   "mapped",
	}
	m, _ := MailgunInboundParser{}.Parse(data)
	if m.FromEmail != "x@y.com" || m.Subject != "mapped" {
		t.Errorf("got FromEmail=%q Subject=%q", m.FromEmail, m.Subject)
	}
}

func TestMailgunParser_AcceptsBytePayload(t *testing.T) {
	payload := []byte(`{"sender":"b@c.com","recipient":"d@e.com","subject":"bytes"}`)
	m, _ := MailgunInboundParser{}.Parse(payload)
	if m.FromEmail != "b@c.com" || m.Subject != "bytes" {
		t.Errorf("got FromEmail=%q Subject=%q", m.FromEmail, m.Subject)
	}
}
