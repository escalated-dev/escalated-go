package models

import "time"

// API token abilities. A token's abilities narrow what its owner may do over
// the REST API; they never widen it (escalated-laravel ApiToken).
const (
	APITokenAbilityAgent = "agent"
	APITokenAbilityAdmin = "admin"
	APITokenAbilityAll   = "*"
)

// APITokenAbilities are the abilities a token may be given.
var APITokenAbilities = []string{APITokenAbilityAgent, APITokenAbilityAdmin, APITokenAbilityAll}

// APIToken is a bearer token for the REST API, owned by a host user.
//
// Only the SHA-256 digest of the token is stored (TokenHash), and it is never
// serialized: the plain text is shown once, when the token is created.
type APIToken struct {
	ID         int64      `json:"id"`
	UserID     UserID     `json:"user_id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"-"`
	Abilities  []string   `json:"abilities"`
	LastUsedAt *time.Time `json:"last_used_at"`
	LastUsedIP *string    `json:"last_used_ip"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// HasAbility reports whether the token carries ability, or "*".
func (t *APIToken) HasAbility(ability string) bool {
	for _, a := range t.Abilities {
		if a == APITokenAbilityAll || a == ability {
			return true
		}
	}
	return false
}

// IsExpired reports whether the token has an expiry at or before now.
func (t *APIToken) IsExpired(now time.Time) bool {
	return t.ExpiresAt != nil && !t.ExpiresAt.After(now)
}
