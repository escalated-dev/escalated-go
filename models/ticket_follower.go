package models

import "database/sql"

// TicketFollower is a join row: a host user following a ticket. Followers are a
// notification target alongside the assignee and requester. Recorded via the
// add_follower workflow action.
type TicketFollower struct {
	TicketID int64  `json:"ticket_id"`
	UserID   UserID `json:"user_id"`
}

// FollowerRecipients returns the follower user ids minus the actor (a user is
// never notified of their own action), de-duplicated and order-preserving.
//
// Go has no built-in notification fan-out, so these ids are exposed for the
// host app to deliver to. See issue #72.
func FollowerRecipients(userIDs []UserID, excludeUserID UserID) []UserID {
	result := make([]UserID, 0, len(userIDs))
	seen := make(map[UserID]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id == excludeUserID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// AddFollower idempotently records a user as following a ticket.
func AddFollower(db *sql.DB, ticketID int64, userID UserID) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO escalated_ticket_followers (ticket_id, user_id) VALUES (?, ?)`,
		ticketID, userID,
	)
	return err
}

// FollowerUserIDs returns the host-user ids following a ticket, minus the
// actor and de-duplicated.
func FollowerUserIDs(db *sql.DB, ticketID int64, excludeUserID UserID) ([]UserID, error) {
	rows, err := db.Query(
		`SELECT user_id FROM escalated_ticket_followers WHERE ticket_id = ?`,
		ticketID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []UserID
	for rows.Next() {
		var id UserID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return FollowerRecipients(ids, excludeUserID), nil
}
