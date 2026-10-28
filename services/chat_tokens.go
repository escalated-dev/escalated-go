package services

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"time"

	"github.com/escalated-dev/escalated-go/models"
)

// Widget chat tokens are expiring grants, as in escalated-laravel: the visitor
// gets a random token once, only its SHA-256 is kept on the chat session, and
// it stops working the guest grant lifetime (GuestAccessConfig.TTL) after the
// chat starts. The chat's ticket keeps no guest token, so a chat token cannot
// open ticket routes.

// IssueChatToken gives a new chat session its visitor token and clears the
// chat ticket's guest token. It returns the token and when it expires.
func (g *GuestAccess) IssueChatToken(ctx context.Context, sessionID, ticketID int64) (string, time.Time, error) {
	token, err := models.GenerateGuestToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := g.clock().Truncate(time.Second).Add(g.grantTTL())
	if _, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("chat_sessions")+`
		SET visitor_token_hash = ?, visitor_token_expires_at = ? WHERE id = ?`), sha256Hex(token), expires, sessionID); err != nil {
		return "", time.Time{}, err
	}
	if _, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("tickets")+` SET guest_token = NULL WHERE id = ?`), ticketID); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// ChatSessionForToken returns the chat session an unexpired visitor token
// unlocks; ok is false for an unknown or expired token.
//
// A chat started before tokens were hashed holds its plain token as the chat
// ticket's guest_token and nothing on the session. The first request with it
// moves the token's hash to the session with a full lifetime and clears the
// ticket's copy, so chats in progress during an upgrade keep working.
func (g *GuestAccess) ChatSessionForToken(ctx context.Context, token string) (int64, bool, error) {
	if token == "" || len(token) > 255 {
		return 0, false, nil
	}
	now := g.clock()
	var id int64
	var expires sql.NullTime
	err := g.DB.QueryRowContext(ctx, g.q(`SELECT id, visitor_token_expires_at FROM `+g.t("chat_sessions")+`
		WHERE visitor_token_hash = ?`), sha256Hex(token)).Scan(&id, &expires)
	if err == nil {
		return id, expires.Valid && expires.Time.After(now), nil
	}
	if err != sql.ErrNoRows {
		return 0, false, err
	}

	var ticketID int64
	var stored sql.NullString
	err = g.DB.QueryRowContext(ctx, g.q(`SELECT s.id, t.id, t.guest_token FROM `+g.t("chat_sessions")+` s
		JOIN `+g.t("tickets")+` t ON t.id = s.ticket_id
		WHERE t.guest_token = ? AND s.visitor_token_hash IS NULL
		ORDER BY s.created_at DESC LIMIT 1`), token).Scan(&id, &ticketID, &stored)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	// The column's collation may compare case-insensitively.
	if !stored.Valid || subtle.ConstantTimeCompare([]byte(stored.String), []byte(token)) != 1 {
		return 0, false, nil
	}
	if _, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("chat_sessions")+`
		SET visitor_token_hash = ?, visitor_token_expires_at = ? WHERE id = ?`),
		sha256Hex(token), now.Truncate(time.Second).Add(g.grantTTL()), id); err != nil {
		return 0, false, err
	}
	if _, err := g.DB.ExecContext(ctx, g.q(`UPDATE `+g.t("tickets")+` SET guest_token = NULL WHERE id = ?`), ticketID); err != nil {
		return 0, false, err
	}
	return id, true, nil
}
