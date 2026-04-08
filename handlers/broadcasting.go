package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/escalated-dev/escalated-go/services"
)

// BroadcastHandler serves the real-time event streaming endpoints.
type BroadcastHandler struct {
	broadcaster *services.Broadcaster
	userID      func(r *http.Request) int64
}

// NewBroadcastHandler creates a new BroadcastHandler.
func NewBroadcastHandler(b *services.Broadcaster, userIDFunc func(r *http.Request) int64) *BroadcastHandler {
	return &BroadcastHandler{
		broadcaster: b,
		userID:      userIDFunc,
	}
}

// SSE handles GET /api/events - Server-Sent Events stream.
func (h *BroadcastHandler) SSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	uid := h.userID(r)
	if uid <= 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	// Parse requested channels from query parameter
	channelsParam := r.URL.Query().Get("channels")
	var channels []string
	if channelsParam != "" {
		channels = strings.Split(channelsParam, ",")
	}
	// Always subscribe to the user's private channel
	channels = append(channels, services.UserChannel(uid))

	subscriberID := fmt.Sprintf("user-%d", uid)
	sub, err := h.broadcaster.Subscribe(subscriberID, channels)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	defer h.broadcaster.Unsubscribe(subscriberID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"subscriber_id\":%q}\n\n", subscriberID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.Done():
			return
		case evt := <-sub.Events:
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.EventType, data)
			flusher.Flush()
		}
	}
}

// SubscribeChannel handles POST /api/events/subscribe - add a channel to current subscription.
func (h *BroadcastHandler) SubscribeChannel(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)
	if uid <= 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var in struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}

	subscriberID := fmt.Sprintf("user-%d", uid)
	if err := h.broadcaster.AddChannel(subscriberID, in.Channel); err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

// UnsubscribeChannel handles POST /api/events/unsubscribe - remove a channel.
func (h *BroadcastHandler) UnsubscribeChannel(w http.ResponseWriter, r *http.Request) {
	uid := h.userID(r)
	if uid <= 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var in struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}

	subscriberID := fmt.Sprintf("user-%d", uid)
	h.broadcaster.RemoveChannel(subscriberID, in.Channel)

	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}
