package store

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// chatRuleColumns is the column list every chat routing rule query reads, in
// the order scanChatRule expects.
const chatRuleColumns = `id, name, strategy, department_id, agent_ids, priority, max_concurrent_chats, is_active, created_at`

// chatRuleOrder puts a department's own rules before the global
// (no-department) ones, then orders by priority.
const chatRuleOrder = `ORDER BY CASE WHEN department_id IS NULL THEN 1 ELSE 0 END, priority DESC, id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanChatRule(row rowScanner) (*models.ChatRoutingRule, error) {
	r := &models.ChatRoutingRule{}
	var agents sql.NullString
	if err := row.Scan(&r.ID, &r.Name, &r.Strategy, &r.DepartmentID, &agents, &r.Priority, &r.MaxConcurrentChats, &r.IsActive, &r.CreatedAt); err != nil {
		return nil, err
	}
	r.AgentIDs = decodeChatRuleAgentIDs(agents.String)
	return r, nil
}

// encodeChatRuleAgentIDs stores a rule's agents as a JSON array of strings, so
// integer and UUID/string host user ids round-trip the same way.
func encodeChatRuleAgentIDs(ids []models.UserID) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			out = append(out, string(id))
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// decodeChatRuleAgentIDs reads the agent_ids column. It accepts the JSON array
// written now and the PostgreSQL array literal ({1,2} or {"a","b"}) the
// PostgreSQL store wrote before, so existing rules keep their agents.
func decodeChatRuleAgentIDs(raw string) []models.UserID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var ids []models.UserID
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			return nil
		}
		return compactUserIDs(ids)
	}
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "{"), "}")

	var ids []models.UserID
	var cur strings.Builder
	quoted, escaped := false, false
	for _, c := range raw {
		switch {
		case escaped:
			cur.WriteRune(c)
			escaped = false
		case c == '\\' && quoted:
			escaped = true
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			ids = append(ids, models.UserID(strings.TrimSpace(cur.String())))
			cur.Reset()
		default:
			cur.WriteRune(c)
		}
	}
	ids = append(ids, models.UserID(strings.TrimSpace(cur.String())))
	return compactUserIDs(ids)
}

func compactUserIDs(ids []models.UserID) []models.UserID {
	var out []models.UserID
	for _, id := range ids {
		if id != "" && !strings.EqualFold(string(id), "null") {
			out = append(out, id)
		}
	}
	return out
}
