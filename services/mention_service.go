package services

import (
	"regexp"
	"strings"
)

var mentionRegex = regexp.MustCompile(`@(\w+(?:\.\w+)*)`)

// MentionResult represents a created mention
type MentionResult struct {
	ReplyID int `json:"reply_id"`
	UserID  int `json:"user_id"`
}

// AgentSearchResult represents an agent autocomplete result
type AgentSearchResult struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

// ExtractMentions extracts unique @usernames from text
func ExtractMentions(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := mentionRegex.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var result []string
	for _, match := range matches {
		if len(match) > 1 && !seen[match[1]] {
			seen[match[1]] = true
			result = append(result, match[1])
		}
	}
	return result
}

// ExtractUsernameFromEmail extracts the username portion from an email
func ExtractUsernameFromEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return email
}
