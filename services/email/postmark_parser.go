package email

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// PostmarkInboundParser parses Postmark's inbound webhook payload
// into an InboundMessage. Postmark posts a JSON body with FromFull /
// ToFull / Subject / TextBody / HtmlBody / Headers / Attachments
// fields.
//
// rawPayload may be either a map[string]interface{} (from
// json.Unmarshal) or a []byte containing the raw JSON body. Both
// are normalized internally.
type PostmarkInboundParser struct{}

// Name returns the adapter label — must match the ?adapter= query
// param or X-Escalated-Adapter header value.
func (PostmarkInboundParser) Name() string { return "postmark" }

// Parse normalizes the provider payload into a transport-agnostic
// InboundMessage. Returns an error for malformed JSON.
func (PostmarkInboundParser) Parse(rawPayload interface{}) (InboundMessage, error) {
	data, err := coerceToMap(rawPayload)
	if err != nil {
		return InboundMessage{}, err
	}

	fromFull, _ := data["FromFull"].(map[string]interface{})
	fromEmail := stringAt(fromFull, "Email")
	if fromEmail == "" {
		fromEmail = stringAt(data, "From")
	}
	fromName := stringAt(fromFull, "Name")
	if fromName == "" {
		fromName = stringAt(data, "FromName")
	}

	toEmail := stringAt(data, "OriginalRecipient")
	if toEmail == "" {
		toEmail = firstToEmail(data)
	}
	if toEmail == "" {
		toEmail = stringAt(data, "To")
	}

	headers := extractHeaders(data)

	messageID := stringAt(data, "MessageID")
	if messageID == "" {
		messageID = headers["Message-ID"]
	}

	return InboundMessage{
		FromEmail:   fromEmail,
		FromName:    fromName,
		ToEmail:     toEmail,
		Subject:     stringAt(data, "Subject"),
		BodyText:    stringAt(data, "TextBody"),
		BodyHTML:    stringAt(data, "HtmlBody"),
		MessageID:   messageID,
		InReplyTo:   headers["In-Reply-To"],
		References:  headers["References"],
		Headers:     headers,
		Attachments: extractAttachments(data),
	}, nil
}

// coerceToMap accepts raw JSON bytes or an already-decoded map.
func coerceToMap(raw interface{}) (map[string]interface{}, error) {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v, nil
	case []byte:
		var out map[string]interface{}
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, err
		}
		return out, nil
	case string:
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, err
		}
		return out, nil
	default:
		return nil, errors.New("unsupported payload type")
	}
}

func stringAt(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func firstToEmail(root map[string]interface{}) string {
	arr, ok := root["ToFull"].([]interface{})
	if !ok {
		return ""
	}
	for _, entry := range arr {
		obj, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		if email := stringAt(obj, "Email"); email != "" {
			return email
		}
	}
	return ""
}

func extractHeaders(root map[string]interface{}) map[string]string {
	out := make(map[string]string)
	arr, ok := root["Headers"].([]interface{})
	if !ok {
		return out
	}
	for _, entry := range arr {
		obj, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		name := stringAt(obj, "Name")
		value := stringAt(obj, "Value")
		if name != "" {
			out[name] = value
		}
	}
	return out
}

func extractAttachments(root map[string]interface{}) []InboundAttachment {
	arr, ok := root["Attachments"].([]interface{})
	if !ok {
		return nil
	}
	var list []InboundAttachment
	for _, entry := range arr {
		obj, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		name := stringAt(obj, "Name")
		if name == "" {
			name = "attachment"
		}
		contentType := stringAt(obj, "ContentType")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		var size int64
		if n, ok := obj["ContentLength"].(float64); ok {
			size = int64(n)
		}
		var content []byte
		if b64 := stringAt(obj, "Content"); b64 != "" {
			if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil {
				content = decoded
			}
		}
		list = append(list, InboundAttachment{
			Name:        name,
			ContentType: contentType,
			SizeBytes:   size,
			Content:     content,
		})
	}
	return list
}
