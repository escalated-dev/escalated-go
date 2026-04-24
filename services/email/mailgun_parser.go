package email

import (
	"encoding/json"
	"strings"
)

// MailgunInboundParser parses Mailgun's inbound webhook payload into
// an InboundMessage. Mailgun POSTs multipart/form-data with snake-
// case field names (sender / recipient / body-plain / body-html /
// Message-Id / In-Reply-To / References / attachments).
//
// The handler already delivers the form-decoded payload as
// map[string]string or map[string]interface{}; this parser normalizes
// either shape. JSON []byte input is also accepted for testing.
type MailgunInboundParser struct{}

// Name returns the adapter label for mailgun webhooks.
func (MailgunInboundParser) Name() string { return "mailgun" }

// Parse normalizes the provider payload into an InboundMessage.
// Malformed JSON for the `attachments` field degrades gracefully
// (empty attachment list, no error).
func (MailgunInboundParser) Parse(rawPayload interface{}) (InboundMessage, error) {
	m := flattenToStringMap(rawPayload)

	fromEmail := m["sender"]
	if fromEmail == "" {
		fromEmail = m["from"]
	}
	fromName := extractFromName(m["from"])

	toEmail := m["recipient"]
	if toEmail == "" {
		toEmail = m["To"]
	}

	headers := make(map[string]string)
	if v := m["Message-Id"]; v != "" {
		headers["Message-ID"] = v
	}
	if v := m["In-Reply-To"]; v != "" {
		headers["In-Reply-To"] = v
	}
	if v := m["References"]; v != "" {
		headers["References"] = v
	}

	return InboundMessage{
		FromEmail:   fromEmail,
		FromName:    fromName,
		ToEmail:     toEmail,
		Subject:     m["subject"],
		BodyText:    m["body-plain"],
		BodyHTML:    m["body-html"],
		MessageID:   m["Message-Id"],
		InReplyTo:   m["In-Reply-To"],
		References:  m["References"],
		Headers:     headers,
		Attachments: parseMailgunAttachments(m["attachments"]),
	}, nil
}

// flattenToStringMap accepts map[string]string, map[string]interface{},
// []byte (JSON), or string (JSON) and returns a string-keyed/string-
// valued map. Non-scalar values are rendered with fmt.Sprintf.
func flattenToStringMap(raw interface{}) map[string]string {
	switch v := raw.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		out := make(map[string]string, len(v))
		for k, val := range v {
			if val == nil {
				continue
			}
			if s, ok := val.(string); ok {
				out[k] = s
				continue
			}
			// Serialize non-string values back to a JSON string so
			// nested structures round-trip through the attachments
			// decoder.
			if b, err := json.Marshal(val); err == nil {
				out[k] = string(b)
			}
		}
		return out
	case []byte:
		var m map[string]interface{}
		if err := json.Unmarshal(v, &m); err == nil {
			return flattenToStringMap(m)
		}
	case string:
		return flattenToStringMap([]byte(v))
	}
	return map[string]string{}
}

// extractFromName pulls the display-name portion out of a
// "Full Name <email>" style header. Returns "" when the input is
// a bare email address. Surrounding quotes are stripped.
func extractFromName(raw string) string {
	if raw == "" {
		return ""
	}
	angle := strings.Index(raw, "<")
	if angle <= 0 {
		return ""
	}
	name := strings.TrimSpace(raw[:angle])
	// Strip surrounding quotes if present.
	if len(name) >= 2 && strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) {
		name = name[1 : len(name)-1]
	}
	return name
}

// parseMailgunAttachments decodes Mailgun's JSON-encoded attachments
// string. Mailgun hosts content behind a URL for large attachments;
// we carry the URL through in DownloadURL.
func parseMailgunAttachments(jsonStr string) []InboundAttachment {
	if jsonStr == "" {
		return nil
	}
	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &entries); err != nil {
		return nil
	}
	var list []InboundAttachment
	for _, entry := range entries {
		name, _ := entry["name"].(string)
		if name == "" {
			name = "attachment"
		}
		contentType, _ := entry["content-type"].(string)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		var size int64
		if n, ok := entry["size"].(float64); ok {
			size = int64(n)
		}
		url, _ := entry["url"].(string)

		list = append(list, InboundAttachment{
			Name:        name,
			ContentType: contentType,
			SizeBytes:   size,
			DownloadURL: url,
		})
	}
	return list
}
