package store

// unreadClause keeps the tickets unread for one agent (ADR
// 2026-11-05-agent-panel-data): marked unread (a read row with no
// last_read_at), or with customer activity after the agent last read them.
// first and second are the placeholders for the agent's id in the two
// subqueries.
func unreadClause(tickets, reads, first, second string) string {
	return "(EXISTS (SELECT 1 FROM " + reads + " ur WHERE ur.ticket_id = " + tickets + ".id" +
		" AND ur.user_id = " + first + " AND ur.last_read_at IS NULL)" +
		" OR (" + tickets + ".last_customer_activity_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM " + reads +
		" ur WHERE ur.ticket_id = " + tickets + ".id AND ur.user_id = " + second +
		" AND ur.last_read_at >= " + tickets + ".last_customer_activity_at)))"
}
