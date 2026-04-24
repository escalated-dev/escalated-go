package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/escalated-dev/escalated-go/services/email"
)

// InboundEmailHandler is the single ingress for inbound-email
// webhooks. Dispatches to the matching parser (selected via
// ?adapter=... query or X-Escalated-Adapter header), then hands the
// parsed message to email.InboundEmailService which orchestrates
// resolve-or-create + reply-add + noise skipping.
//
// Guarded by a constant-time shared-secret check on the
// X-Escalated-Inbound-Secret header — hosts configure this via the
// same secret that signs Reply-To addresses (symmetric).
type InboundEmailHandler struct {
	service *email.InboundEmailService
	parsers map[string]email.InboundEmailParser
	secret  string
}

// NewInboundEmailHandler constructs a handler wired to an inbound
// service + the provided parsers. Parsers are registered by their
// Name().
//
// Host apps build the service once at startup (passing their
// TicketService shim + TicketLookup + mail domain + secret) so the
// service can share a writer across requests.
func NewInboundEmailHandler(service *email.InboundEmailService, secret string, parsers ...email.InboundEmailParser) *InboundEmailHandler {
	byName := make(map[string]email.InboundEmailParser, len(parsers))
	for _, p := range parsers {
		byName[p.Name()] = p
	}
	return &InboundEmailHandler{
		service: service,
		parsers: byName,
		secret:  secret,
	}
}

// Inbound is the HTTP handler for POST /escalated/webhook/email/inbound.
// Returns:
//   - 200 OK { status, outcome, ticket_id, reply_id, pending_attachment_downloads }
//     on successful processing.
//   - 401 Unauthorized on secret mismatch.
//   - 400 Bad Request on unknown adapter or invalid payload.
//   - 500 Internal Server Error on store errors from the service.
func (h *InboundEmailHandler) Inbound(w http.ResponseWriter, r *http.Request) {
	if !h.verifySecret(r) {
		writeInboundJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "missing or invalid inbound secret",
		})
		return
	}

	adapter := r.URL.Query().Get("adapter")
	if adapter == "" {
		adapter = r.Header.Get("X-Escalated-Adapter")
	}
	if adapter == "" {
		writeInboundJSON(w, http.StatusBadRequest, map[string]string{"error": "missing adapter"})
		return
	}

	parser, ok := h.parsers[adapter]
	if !ok {
		writeInboundJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unknown adapter: " + adapter,
		})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeInboundJSON(w, http.StatusBadRequest, map[string]string{"error": "unreadable body"})
		return
	}
	defer r.Body.Close()

	message, err := parser.Parse(body)
	if err != nil {
		log.Printf("[InboundEmailHandler] parse failed for %s: %v", adapter, err)
		writeInboundJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	result, err := h.service.Process(r.Context(), message)
	if err != nil {
		log.Printf("[InboundEmailHandler] processing error: %v", err)
		writeInboundJSON(w, http.StatusInternalServerError, map[string]string{"error": "processing failed"})
		return
	}

	writeInboundJSON(w, http.StatusOK, map[string]interface{}{
		"status":                       statusForOutcome(result.Outcome),
		"outcome":                      string(result.Outcome),
		"ticket_id":                    result.TicketID,
		"reply_id":                     result.ReplyID,
		"pending_attachment_downloads": pendingToJSON(result.PendingAttachmentDownloads),
	})
}

func statusForOutcome(o email.Outcome) string {
	switch o {
	case email.OutcomeRepliedToExisting:
		return "matched"
	case email.OutcomeCreatedNew:
		return "created"
	case email.OutcomeSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

func pendingToJSON(in []email.PendingAttachment) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(in))
	for _, a := range in {
		out = append(out, map[string]interface{}{
			"name":         a.Name,
			"content_type": a.ContentType,
			"size_bytes":   a.SizeBytes,
			"download_url": a.DownloadURL,
		})
	}
	return out
}

func (h *InboundEmailHandler) verifySecret(r *http.Request) bool {
	if h.secret == "" {
		return false
	}
	provided := r.Header.Get("X-Escalated-Inbound-Secret")
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(h.secret), []byte(provided)) == 1
}

func writeInboundJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
