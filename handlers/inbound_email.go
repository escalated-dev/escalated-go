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
// ?adapter=... query or X-Escalated-Adapter header), then resolves
// the parsed message to a ticket via email.InboundRouter.
//
// Guarded by a constant-time shared-secret check on the
// X-Escalated-Inbound-Secret header — hosts configure this via the
// same secret that signs Reply-To addresses (symmetric).
type InboundEmailHandler struct {
	router  *email.InboundRouter
	parsers map[string]email.InboundEmailParser
	secret  string
}

// NewInboundEmailHandler constructs a handler wired to a router +
// the provided parsers. Parsers are registered by their Name().
func NewInboundEmailHandler(s email.TicketLookup, domain, secret string, parsers ...email.InboundEmailParser) *InboundEmailHandler {
	router := email.NewInboundRouter(s, domain, secret)
	byName := make(map[string]email.InboundEmailParser, len(parsers))
	for _, p := range parsers {
		byName[p.Name()] = p
	}
	return &InboundEmailHandler{
		router:  router,
		parsers: byName,
		secret:  secret,
	}
}

// Inbound is the HTTP handler for POST /escalated/webhook/email/inbound.
// Returns:
//   - 200 OK { status, ticketId? } on successful routing.
//   - 401 Unauthorized on secret mismatch.
//   - 400 Bad Request on unknown adapter or invalid payload.
//   - 500 Internal Server Error on store errors from the router.
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

	ticket, err := h.router.ResolveTicket(r.Context(), message)
	if err != nil {
		log.Printf("[InboundEmailHandler] router error: %v", err)
		writeInboundJSON(w, http.StatusInternalServerError, map[string]string{"error": "routing failed"})
		return
	}

	status := "unmatched"
	var ticketID int64
	if ticket != nil {
		status = "matched"
		ticketID = ticket.ID
	}
	writeInboundJSON(w, http.StatusOK, map[string]interface{}{
		"status":    status,
		"ticket_id": ticketID,
	})
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
