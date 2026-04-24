package email

import (
	"testing"
)

const postmarkSamplePayload = `{
  "FromName": "Customer",
  "MessageID": "22c74902-a0c1-4511-804f2-341342852c90",
  "FromFull": {"Email": "customer@example.com", "Name": "Customer"},
  "To": "support+abc@support.example.com",
  "ToFull": [{"Email": "support+abc@support.example.com", "Name": ""}],
  "OriginalRecipient": "support+abc@support.example.com",
  "Subject": "[ESC-00042] Help",
  "TextBody": "Plain body",
  "HtmlBody": "<p>HTML body</p>",
  "Headers": [
    {"Name": "Message-ID", "Value": "<abc@mail.client>"},
    {"Name": "In-Reply-To", "Value": "<ticket-42@support.example.com>"},
    {"Name": "References", "Value": "<ticket-42@support.example.com>"}
  ],
  "Attachments": [
    {"Name": "report.pdf", "Content": "aGVsbG8=", "ContentType": "application/pdf", "ContentLength": 5}
  ]
}`

func TestPostmarkParser_Name(t *testing.T) {
	if got := (PostmarkInboundParser{}).Name(); got != "postmark" {
		t.Fatalf("Name() = %q, want postmark", got)
	}
}

func TestPostmarkParser_ExtractsCoreFields(t *testing.T) {
	m, err := PostmarkInboundParser{}.Parse([]byte(postmarkSamplePayload))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
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

func TestPostmarkParser_ExtractsThreadingHeaders(t *testing.T) {
	m, _ := PostmarkInboundParser{}.Parse([]byte(postmarkSamplePayload))
	if m.InReplyTo != "<ticket-42@support.example.com>" {
		t.Errorf("InReplyTo = %q", m.InReplyTo)
	}
	if m.References != "<ticket-42@support.example.com>" {
		t.Errorf("References = %q", m.References)
	}
}

func TestPostmarkParser_ExtractsAttachments(t *testing.T) {
	m, _ := PostmarkInboundParser{}.Parse([]byte(postmarkSamplePayload))
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
	if a.SizeBytes != 5 {
		t.Errorf("SizeBytes = %d", a.SizeBytes)
	}
	if string(a.Content) != "hello" {
		t.Errorf("Content = %q", string(a.Content))
	}
}

func TestPostmarkParser_MinimalPayload(t *testing.T) {
	payload := `{
    "FromFull": {"Email": "a@b.com"},
    "ToFull": [{"Email": "c@d.com"}],
    "Subject": "minimal"
  }`
	m, err := PostmarkInboundParser{}.Parse([]byte(payload))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if m.FromEmail != "a@b.com" {
		t.Errorf("FromEmail = %q", m.FromEmail)
	}
	if m.FromName != "" {
		t.Errorf("FromName should be empty, got %q", m.FromName)
	}
	if m.ToEmail != "c@d.com" {
		t.Errorf("ToEmail = %q", m.ToEmail)
	}
	if m.Subject != "minimal" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if m.InReplyTo != "" {
		t.Errorf("InReplyTo should be empty, got %q", m.InReplyTo)
	}
}

func TestPostmarkParser_AcceptsStringPayload(t *testing.T) {
	m, err := PostmarkInboundParser{}.Parse(postmarkSamplePayload)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if m.FromEmail != "customer@example.com" {
		t.Errorf("FromEmail = %q", m.FromEmail)
	}
}

func TestPostmarkParser_AcceptsMapPayload(t *testing.T) {
	// Simulate an already-decoded map (e.g. net/http with
	// middleware that decoded JSON for us).
	payload := map[string]interface{}{
		"FromFull": map[string]interface{}{"Email": "x@y.com"},
		"ToFull":   []interface{}{map[string]interface{}{"Email": "z@w.com"}},
		"Subject":  "mapped",
	}
	m, err := PostmarkInboundParser{}.Parse(payload)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if m.FromEmail != "x@y.com" || m.Subject != "mapped" {
		t.Errorf("got %+v", m)
	}
}

func TestPostmarkParser_RejectsUnsupportedType(t *testing.T) {
	_, err := PostmarkInboundParser{}.Parse(42)
	if err == nil {
		t.Fatalf("expected error for unsupported payload type")
	}
}
