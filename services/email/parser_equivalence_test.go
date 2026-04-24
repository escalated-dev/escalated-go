package email

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Parser-equivalence tests: the same logical email, expressed in each
// provider's native webhook payload shape, should normalize to the
// same InboundMessage for the fields the router cares about (threading
// headers + routing metadata). This cements the parser contract so
// adding a fourth provider can be validated against the existing three.

type logicalEmail struct {
	fromEmail  string
	fromName   string
	toEmail    string
	subject    string
	bodyText   string
	messageID  string
	inReplyTo  string
	references string
}

var sampleReply = logicalEmail{
	fromEmail:  "alice@example.com",
	fromName:   "Alice",
	toEmail:    "support@example.com",
	subject:    "Re: Help with invoice",
	bodyText:   "Thanks for the quick response.",
	messageID:  "<external-reply-xyz@mail.alice.com>",
	inReplyTo:  "<ticket-42@support.example.com>",
	references: "<ticket-42@support.example.com>",
}

func buildPostmarkPayload(e logicalEmail) map[string]interface{} {
	return map[string]interface{}{
		"FromFull": map[string]interface{}{
			"Email": e.fromEmail,
			"Name":  e.fromName,
		},
		"To":       e.toEmail,
		"Subject":  e.subject,
		"TextBody": e.bodyText,
		"Headers": []interface{}{
			map[string]interface{}{"Name": "Message-ID", "Value": e.messageID},
			map[string]interface{}{"Name": "In-Reply-To", "Value": e.inReplyTo},
			map[string]interface{}{"Name": "References", "Value": e.references},
		},
	}
}

func buildMailgunPayload(e logicalEmail) map[string]interface{} {
	return map[string]interface{}{
		"sender":      e.fromEmail,
		"from":        e.fromName + " <" + e.fromEmail + ">",
		"recipient":   e.toEmail,
		"subject":     e.subject,
		"body-plain":  e.bodyText,
		"Message-Id":  e.messageID,
		"In-Reply-To": e.inReplyTo,
		"References":  e.references,
	}
}

func buildSESPayload(t *testing.T, e logicalEmail) map[string]interface{} {
	t.Helper()

	// Also include the full raw MIME as base64 so body extraction is
	// exercised. The equivalence check only looks at metadata, but
	// this keeps the test close to a real SES delivery.
	mime := "From: " + e.fromName + " <" + e.fromEmail + ">\r\n" +
		"To: " + e.toEmail + "\r\n" +
		"Subject: " + e.subject + "\r\n" +
		"Message-ID: " + e.messageID + "\r\n" +
		"In-Reply-To: " + e.inReplyTo + "\r\n" +
		"References: " + e.references + "\r\n" +
		"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
		"\r\n" +
		e.bodyText

	sesMessage := map[string]interface{}{
		"notificationType": "Received",
		"mail": map[string]interface{}{
			"source":      e.fromEmail,
			"destination": []interface{}{e.toEmail},
			"headers": []interface{}{
				map[string]interface{}{"name": "From", "value": e.fromName + " <" + e.fromEmail + ">"},
				map[string]interface{}{"name": "To", "value": e.toEmail},
				map[string]interface{}{"name": "Subject", "value": e.subject},
				map[string]interface{}{"name": "Message-ID", "value": e.messageID},
				map[string]interface{}{"name": "In-Reply-To", "value": e.inReplyTo},
				map[string]interface{}{"name": "References", "value": e.references},
			},
			"commonHeaders": map[string]interface{}{
				"from":    []interface{}{e.fromName + " <" + e.fromEmail + ">"},
				"to":      []interface{}{e.toEmail},
				"subject": e.subject,
			},
		},
		"content": base64.StdEncoding.EncodeToString([]byte(mime)),
	}
	messageJSON, err := json.Marshal(sesMessage)
	if err != nil {
		t.Fatalf("marshal SES message: %v", err)
	}
	return map[string]interface{}{
		"Type":    "Notification",
		"Message": string(messageJSON),
	}
}

// TestParserEquivalence_NormalizesToSameMessage verifies that each
// provider's parser produces the same {FromEmail, ToEmail, Subject,
// InReplyTo, References} set for a single logical email. These are
// the fields the router uses for ticket resolution — so
// equivalence here means a reply delivered via any provider routes
// to the same ticket.
func TestParserEquivalence_NormalizesToSameMessage(t *testing.T) {
	postmarkMsg, err := (PostmarkInboundParser{}).Parse(buildPostmarkPayload(sampleReply))
	if err != nil {
		t.Fatalf("postmark parse: %v", err)
	}

	mailgunMsg, err := (MailgunInboundParser{}).Parse(buildMailgunPayload(sampleReply))
	if err != nil {
		t.Fatalf("mailgun parse: %v", err)
	}

	sesMsg, err := (SESInboundParser{}).Parse(buildSESPayload(t, sampleReply))
	if err != nil {
		t.Fatalf("ses parse: %v", err)
	}

	parsers := map[string]InboundMessage{
		"postmark": postmarkMsg,
		"mailgun":  mailgunMsg,
		"ses":      sesMsg,
	}

	for name, msg := range parsers {
		if msg.FromEmail != sampleReply.fromEmail {
			t.Errorf("%s: FromEmail = %q, want %q", name, msg.FromEmail, sampleReply.fromEmail)
		}
		if msg.ToEmail != sampleReply.toEmail {
			t.Errorf("%s: ToEmail = %q, want %q", name, msg.ToEmail, sampleReply.toEmail)
		}
		if msg.Subject != sampleReply.subject {
			t.Errorf("%s: Subject = %q, want %q", name, msg.Subject, sampleReply.subject)
		}
		if msg.InReplyTo != sampleReply.inReplyTo {
			t.Errorf("%s: InReplyTo = %q, want %q", name, msg.InReplyTo, sampleReply.inReplyTo)
		}
		if msg.References != sampleReply.references {
			t.Errorf("%s: References = %q, want %q", name, msg.References, sampleReply.references)
		}
	}
}

// TestParserEquivalence_BodyExtractionMatches validates that each
// parser populates the body text for a logical email where the
// provider *does* supply it.
//
// Postmark + Mailgun forward the plain text body directly; SES only
// populates it when the receipt rule includes full MIME content
// (our test payload does via action.type=SNS/encoding=BASE64).
// Matching bodies across providers means downstream rendering
// (reply notifications, activity timeline) stays consistent.
func TestParserEquivalence_BodyExtractionMatches(t *testing.T) {
	postmarkMsg, _ := (PostmarkInboundParser{}).Parse(buildPostmarkPayload(sampleReply))
	mailgunMsg, _ := (MailgunInboundParser{}).Parse(buildMailgunPayload(sampleReply))
	sesMsg, _ := (SESInboundParser{}).Parse(buildSESPayload(t, sampleReply))

	want := sampleReply.bodyText
	for name, msg := range map[string]InboundMessage{
		"postmark": postmarkMsg,
		"mailgun":  mailgunMsg,
		"ses":      sesMsg,
	} {
		if msg.BodyText != want {
			t.Errorf("%s: BodyText = %q, want %q", name, msg.BodyText, want)
		}
	}
}
