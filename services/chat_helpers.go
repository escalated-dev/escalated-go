package services

import "github.com/escalated-dev/escalated-go/models"

// ActiveChatFilter returns a ChatSessionFilters that matches waiting and active sessions.
func ActiveChatFilter() models.ChatSessionFilters {
	return models.ChatSessionFilters{Active: true}
}

// ChatChannel returns the SSE channel name for a chat session.
func ChatChannel(sessionID int64) string {
	return TicketChannel(sessionID)
}
