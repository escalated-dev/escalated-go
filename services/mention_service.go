package services

import (
	"regexp"
	"strings"

	"github.com/escalated-dev/escalated-go/models"
)

// bracedMention is `@{Full Name}`, what the composer's mention picker inserts.
var bracedMention = regexp.MustCompile(`@\{([^}]+)\}`)

// simpleMention is `@name`. Go's regexp has no lookahead, so names inside
// braces are dropped by removing the braced mentions first.
var simpleMention = regexp.MustCompile(`@([a-zA-Z0-9_.-]+)`)

// MentionSuggestionsLimit is how many agents the mention picker lists.
const MentionSuggestionsLimit = 10

// MentionResult represents a created mention
type MentionResult struct {
	ReplyID int `json:"reply_id"`
	UserID  int `json:"user_id"`
}

// AgentSearchResult is one row of the mention picker
// (escalated.admin.agents.search): {id, name, email}.
type AgentSearchResult struct {
	ID    models.UserID `json:"id"`
	Name  string        `json:"name"`
	Email string        `json:"email"`
}

// MentionAgent is a staff member a mention can name.
type MentionAgent struct {
	ID    models.UserID
	Name  string
	Email string
}

// ExtractMentions returns the names a body mentions, as escalated-laravel's
// MentionService: `@{Full Name}` and `@name`, trimmed and unique, braced ones
// first.
func ExtractMentions(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	for _, match := range bracedMention.FindAllStringSubmatch(text, -1) {
		add(match[1])
	}
	for _, match := range simpleMention.FindAllStringSubmatch(bracedMention.ReplaceAllString(text, " "), -1) {
		add(match[1])
	}
	return result
}

// MentionedAgents returns the agents a body mentions, by name or email,
// ignoring case.
func MentionedAgents(text string, agents []MentionAgent) []MentionAgent {
	names := map[string]bool{}
	for _, name := range ExtractMentions(text) {
		names[strings.ToLower(name)] = true
	}
	var found []MentionAgent
	if len(names) == 0 {
		return found
	}
	for _, agent := range agents {
		if (agent.Name != "" && names[strings.ToLower(agent.Name)]) || (agent.Email != "" && names[strings.ToLower(agent.Email)]) {
			found = append(found, agent)
		}
	}
	return found
}

// MentionSuggestions is the mention picker: up to limit agents whose name or
// email contains the query, ignoring case.
func MentionSuggestions(query string, agents []MentionAgent, limit int) []AgentSearchResult {
	out := []AgentSearchResult{}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return out
	}
	for _, agent := range agents {
		if strings.Contains(strings.ToLower(agent.Name), needle) || strings.Contains(strings.ToLower(agent.Email), needle) {
			out = append(out, AgentSearchResult(agent))
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// ExtractUsernameFromEmail extracts the username portion from an email
func ExtractUsernameFromEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return email
}
