package email

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
)

// ErrSESSubscriptionConfirmation is returned by SESInboundParser when
// the webhook receives an SNS subscription-confirmation envelope. The
// host app must fetch the SubscribeURL (surfaced via
// SESSubscriptionConfirmation) out-of-band to activate the
// subscription. Callers can distinguish this from a parse failure via
// errors.Is.
var ErrSESSubscriptionConfirmation = errors.New("SES subscription confirmation received")

// SESSubscriptionConfirmation carries the SubscribeURL that host apps
// must GET to activate the SNS subscription. Returned wrapped in
// ErrSESSubscriptionConfirmation via errors.As.
type SESSubscriptionConfirmation struct {
	TopicARN     string
	SubscribeURL string
	Token        string
}

func (s *SESSubscriptionConfirmation) Error() string {
	return fmt.Sprintf("SES subscription confirmation for topic %s; GET %s to confirm", s.TopicARN, s.SubscribeURL)
}

func (s *SESSubscriptionConfirmation) Unwrap() error { return ErrSESSubscriptionConfirmation }

// SESInboundParser parses AWS SES inbound mail delivered via SNS HTTP
// subscription. SES receipt rules publish a JSON envelope containing
// either:
//
//   - Type: "SubscriptionConfirmation" — one-time on subscription
//     setup. Parse returns ErrSESSubscriptionConfirmation wrapped in
//     an *SESSubscriptionConfirmation carrying the SubscribeURL the
//     host must GET to activate the subscription.
//   - Type: "Notification" — the actual inbound delivery. The inner
//     Message is a JSON-encoded SES notification with mail.commonHeaders
//     and optionally a base64-encoded raw MIME content field.
//
// Parse extracts the threading metadata (from / to / subject / the
// Message-ID triple) from commonHeaders and best-effort-decodes the
// plain text body from the raw MIME content when the SES receipt
// rule supplies it (action type "SNS" with "BASE64" encoding). For
// richer MIME handling (HTML body, inline attachments) host apps
// can embed a MIME parser on top of the InboundMessage.Headers
// metadata this parser surfaces.
type SESInboundParser struct{}

// Name returns the adapter label — matches the ?adapter= query param
// or X-Escalated-Adapter header value.
func (SESInboundParser) Name() string { return "ses" }

// Parse normalizes the SNS envelope into an InboundMessage. Accepts
// the raw JSON body (or an already-decoded map).
func (SESInboundParser) Parse(rawPayload interface{}) (InboundMessage, error) {
	envelope, err := coerceToMap(rawPayload)
	if err != nil {
		return InboundMessage{}, err
	}

	snsType := stringAt(envelope, "Type")
	switch snsType {
	case "SubscriptionConfirmation":
		return InboundMessage{}, &SESSubscriptionConfirmation{
			TopicARN:     stringAt(envelope, "TopicArn"),
			SubscribeURL: stringAt(envelope, "SubscribeURL"),
			Token:        stringAt(envelope, "Token"),
		}
	case "Notification":
		// fall through
	default:
		return InboundMessage{}, fmt.Errorf("unsupported SNS envelope type: %q", snsType)
	}

	// SNS wraps the SES payload in a JSON string at Message.
	messageJSON := stringAt(envelope, "Message")
	if messageJSON == "" {
		return InboundMessage{}, errors.New("SES notification has no Message body")
	}

	var notification map[string]interface{}
	if err := json.Unmarshal([]byte(messageJSON), &notification); err != nil {
		return InboundMessage{}, fmt.Errorf("SES notification Message is not valid JSON: %w", err)
	}

	mailObj, _ := notification["mail"].(map[string]interface{})
	common, _ := mailObj["commonHeaders"].(map[string]interface{})

	fromEmail, fromName := parseFirstAddressList(common["from"])
	toEmail, _ := parseFirstAddressList(common["to"])

	subject := stringAt(common, "subject")
	messageID := stringAt(common, "messageId")
	inReplyTo := stringAt(common, "inReplyTo")
	references := stringAt(common, "references")

	headers := extractSESHeaders(mailObj)
	// Fallbacks from the headers array when commonHeaders is missing
	// the threading fields (SES only populates commonHeaders for
	// well-known headers — In-Reply-To / References aren't always
	// surfaced there).
	if messageID == "" {
		messageID = headers["Message-ID"]
	}
	if inReplyTo == "" {
		inReplyTo = headers["In-Reply-To"]
	}
	if references == "" {
		references = headers["References"]
	}

	// Best-effort body extraction from the base64 content field. The
	// host must have configured the SES receipt rule with action
	// type=SNS and encoding=BASE64 for this to be populated; if it's
	// missing we return an empty body, which the router tolerates.
	bodyText, bodyHTML := extractSESBody(notification)

	return InboundMessage{
		FromEmail:  fromEmail,
		FromName:   fromName,
		ToEmail:    toEmail,
		Subject:    subject,
		BodyText:   bodyText,
		BodyHTML:   bodyHTML,
		MessageID:  messageID,
		InReplyTo:  inReplyTo,
		References: references,
		Headers:    headers,
	}, nil
}

// parseFirstAddressList accepts a JSON array of RFC 5322 address
// strings (e.g. `["Alice <alice@example.com>"]`) and returns the
// first entry's email + display name. Returns ("", "") for missing
// or malformed inputs.
func parseFirstAddressList(raw interface{}) (string, string) {
	arr, ok := raw.([]interface{})
	if !ok || len(arr) == 0 {
		return "", ""
	}
	first, _ := arr[0].(string)
	if first == "" {
		return "", ""
	}

	addr, err := mail.ParseAddress(first)
	if err != nil {
		// Not a parseable RFC 5322 address — return the raw string as
		// the email so downstream callers have something to work with.
		return strings.TrimSpace(first), ""
	}
	return addr.Address, addr.Name
}

// extractSESHeaders pulls the `mail.headers` array (each entry:
// `{"name":"X","value":"Y"}`) into a flat map keyed by header name.
func extractSESHeaders(mailObj map[string]interface{}) map[string]string {
	out := make(map[string]string)
	arr, ok := mailObj["headers"].([]interface{})
	if !ok {
		return out
	}
	for _, entry := range arr {
		obj, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		name := stringAt(obj, "name")
		value := stringAt(obj, "value")
		if name != "" {
			out[name] = value
		}
	}
	return out
}

// extractSESBody best-effort decodes the base64 `content` field and
// extracts text/plain + text/html parts. SES only populates this
// when the receipt rule action is configured with full content
// delivery (type=SNS, encoding=BASE64). Returns ("", "") when the
// field is absent, malformed, or multipart parsing fails.
func extractSESBody(notification map[string]interface{}) (string, string) {
	contentB64 := stringAt(notification, "content")
	if contentB64 == "" {
		return "", ""
	}
	raw, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return "", ""
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return "", ""
	}

	contentType := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Treat unparseable Content-Type as plain text.
		body, _ := io.ReadAll(msg.Body)
		return string(body), ""
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		body, _ := io.ReadAll(msg.Body)
		if isQuotedPrintable(msg.Header.Get("Content-Transfer-Encoding")) {
			body = decodeQuotedPrintable(body)
		}
		if strings.EqualFold(mediaType, "text/html") {
			return "", string(body)
		}
		return string(body), ""
	}

	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return "", ""
	}

	var text, html string
	reader := multipart.NewReader(msg.Body, boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		partBody, _ := io.ReadAll(part)
		if isQuotedPrintable(part.Header.Get("Content-Transfer-Encoding")) {
			partBody = decodeQuotedPrintable(partBody)
		}

		switch {
		case strings.EqualFold(partType, "text/plain") && text == "":
			text = string(partBody)
		case strings.EqualFold(partType, "text/html") && html == "":
			html = string(partBody)
		}
	}
	return text, html
}

func isQuotedPrintable(header string) bool {
	return strings.EqualFold(strings.TrimSpace(header), "quoted-printable")
}

func decodeQuotedPrintable(body []byte) []byte {
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(string(body))))
	if err != nil {
		return body
	}
	return decoded
}
