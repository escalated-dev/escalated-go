package email

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestSESInboundParser_Name(t *testing.T) {
	if name := (SESInboundParser{}).Name(); name != "ses" {
		t.Errorf("Name() = %q, want ses", name)
	}
}

func TestSESInboundParser_SubscriptionConfirmation_ReturnsSentinelError(t *testing.T) {
	envelope := map[string]interface{}{
		"Type":         "SubscriptionConfirmation",
		"TopicArn":     "arn:aws:sns:us-east-1:123:escalated-inbound",
		"SubscribeURL": "https://sns.us-east-1.amazonaws.com/?Action=ConfirmSubscription&Token=x",
		"Token":        "abc",
	}

	_, err := (SESInboundParser{}).Parse(envelope)
	if err == nil {
		t.Fatalf("expected error for SubscriptionConfirmation")
	}
	if !errors.Is(err, ErrSESSubscriptionConfirmation) {
		t.Errorf("err = %v, want wrapping ErrSESSubscriptionConfirmation", err)
	}

	var conf *SESSubscriptionConfirmation
	if !errors.As(err, &conf) {
		t.Fatalf("err does not contain *SESSubscriptionConfirmation: %v", err)
	}
	if conf.SubscribeURL == "" || conf.TopicARN == "" {
		t.Errorf("confirmation missing fields: %+v", conf)
	}
}

func TestSESInboundParser_Notification_ExtractsThreadingMetadata(t *testing.T) {
	sesMessage := map[string]interface{}{
		"notificationType": "Received",
		"mail": map[string]interface{}{
			"source":      "alice@example.com",
			"destination": []interface{}{"support@example.com"},
			"headers": []interface{}{
				map[string]interface{}{"name": "From", "value": "Alice <alice@example.com>"},
				map[string]interface{}{"name": "To", "value": "support@example.com"},
				map[string]interface{}{"name": "Subject", "value": "[ESC-42] Re: Help"},
				map[string]interface{}{"name": "Message-ID", "value": "<external-xyz@mail.alice.com>"},
				map[string]interface{}{"name": "In-Reply-To", "value": "<ticket-42@support.example.com>"},
				map[string]interface{}{"name": "References", "value": "<ticket-42@support.example.com> <prev@mail.com>"},
			},
			"commonHeaders": map[string]interface{}{
				"from":    []interface{}{"Alice <alice@example.com>"},
				"to":      []interface{}{"support@example.com"},
				"subject": "[ESC-42] Re: Help",
			},
		},
	}
	messageJSON, _ := json.Marshal(sesMessage)

	envelope := map[string]interface{}{
		"Type":    "Notification",
		"Message": string(messageJSON),
	}

	msg, err := (SESInboundParser{}).Parse(envelope)
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}

	if msg.FromEmail != "alice@example.com" {
		t.Errorf("FromEmail = %q", msg.FromEmail)
	}
	if msg.FromName != "Alice" {
		t.Errorf("FromName = %q", msg.FromName)
	}
	if msg.ToEmail != "support@example.com" {
		t.Errorf("ToEmail = %q", msg.ToEmail)
	}
	if msg.Subject != "[ESC-42] Re: Help" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	if msg.InReplyTo != "<ticket-42@support.example.com>" {
		t.Errorf("InReplyTo = %q", msg.InReplyTo)
	}
	if msg.References == "" {
		t.Errorf("References should be populated")
	}
	if msg.MessageID != "<external-xyz@mail.alice.com>" {
		t.Errorf("MessageID = %q", msg.MessageID)
	}
	if msg.Headers["From"] == "" {
		t.Errorf("Headers map should carry header array contents")
	}
}

func TestSESInboundParser_Notification_DecodesPlainTextBody(t *testing.T) {
	rawMime := "From: alice@example.com\r\n" +
		"To: support@example.com\r\n" +
		"Subject: Hi\r\n" +
		"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
		"\r\n" +
		"This is the plain text body."
	contentB64 := base64.StdEncoding.EncodeToString([]byte(rawMime))

	sesMessage := map[string]interface{}{
		"notificationType": "Received",
		"mail": map[string]interface{}{
			"headers": []interface{}{},
			"commonHeaders": map[string]interface{}{
				"from":    []interface{}{"alice@example.com"},
				"to":      []interface{}{"support@example.com"},
				"subject": "Hi",
			},
		},
		"content": contentB64,
	}
	messageJSON, _ := json.Marshal(sesMessage)

	envelope := map[string]interface{}{
		"Type":    "Notification",
		"Message": string(messageJSON),
	}

	msg, err := (SESInboundParser{}).Parse(envelope)
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}

	if msg.BodyText != "This is the plain text body." {
		t.Errorf("BodyText = %q", msg.BodyText)
	}
}

func TestSESInboundParser_Notification_DecodesMultipartBody(t *testing.T) {
	boundary := "boundary-abc"
	rawMime := "From: alice@example.com\r\n" +
		"To: support@example.com\r\n" +
		"Subject: Hi\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
		"\r\n" +
		"Plain body\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=\"utf-8\"\r\n" +
		"\r\n" +
		"<p>HTML body</p>\r\n" +
		"--" + boundary + "--\r\n"
	contentB64 := base64.StdEncoding.EncodeToString([]byte(rawMime))

	envelope := map[string]interface{}{
		"Type": "Notification",
		"Message": mustJSON(map[string]interface{}{
			"mail": map[string]interface{}{
				"commonHeaders": map[string]interface{}{
					"from":    []interface{}{"alice@example.com"},
					"to":      []interface{}{"support@example.com"},
					"subject": "Hi",
				},
			},
			"content": contentB64,
		}),
	}

	msg, err := (SESInboundParser{}).Parse(envelope)
	if err != nil {
		t.Fatalf("Parse err = %v", err)
	}

	if msg.BodyText == "" || msg.BodyText[:10] != "Plain body" {
		t.Errorf("BodyText = %q, want starting with 'Plain body'", msg.BodyText)
	}
	if msg.BodyHTML == "" || msg.BodyHTML[:3] != "<p>" {
		t.Errorf("BodyHTML = %q, want starting with '<p>'", msg.BodyHTML)
	}
}

func TestSESInboundParser_Notification_MissingContentLeavesBodyEmpty(t *testing.T) {
	envelope := map[string]interface{}{
		"Type": "Notification",
		"Message": mustJSON(map[string]interface{}{
			"mail": map[string]interface{}{
				"commonHeaders": map[string]interface{}{
					"from":    []interface{}{"alice@example.com"},
					"to":      []interface{}{"support@example.com"},
					"subject": "Hi",
				},
			},
		}),
	}

	msg, err := (SESInboundParser{}).Parse(envelope)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if msg.BodyText != "" || msg.BodyHTML != "" {
		t.Errorf("expected empty body, got text=%q html=%q", msg.BodyText, msg.BodyHTML)
	}
	if msg.FromEmail != "alice@example.com" {
		t.Errorf("FromEmail should still be populated: %q", msg.FromEmail)
	}
}

func TestSESInboundParser_UnknownType_Errors(t *testing.T) {
	_, err := (SESInboundParser{}).Parse(map[string]interface{}{
		"Type": "UnbindConfirmation",
	})
	if err == nil {
		t.Fatalf("expected error for unknown envelope type")
	}
}

func TestSESInboundParser_MissingMessageField_Errors(t *testing.T) {
	_, err := (SESInboundParser{}).Parse(map[string]interface{}{
		"Type": "Notification",
	})
	if err == nil {
		t.Fatalf("expected error for missing Message body")
	}
}

func TestSESInboundParser_MalformedMessageJSON_Errors(t *testing.T) {
	_, err := (SESInboundParser{}).Parse(map[string]interface{}{
		"Type":    "Notification",
		"Message": "not json at all",
	})
	if err == nil {
		t.Fatalf("expected error for malformed Message JSON")
	}
}

func TestSESInboundParser_AcceptsRawJSONBytes(t *testing.T) {
	bodyJSON := `{"Type":"Notification","Message":"{\"mail\":{\"commonHeaders\":{\"from\":[\"alice@example.com\"],\"to\":[\"support@example.com\"],\"subject\":\"Hi\"}}}"}`

	msg, err := (SESInboundParser{}).Parse([]byte(bodyJSON))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if msg.FromEmail != "alice@example.com" {
		t.Errorf("FromEmail = %q", msg.FromEmail)
	}
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
