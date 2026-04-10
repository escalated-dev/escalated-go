package models

import "time"

// TwoFactor stores two-factor authentication configuration for a user.
type TwoFactor struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	Method        string     `json:"method"`
	Secret        *string    `json:"secret,omitempty"`
	RecoveryCodes []string   `json:"recovery_codes,omitempty"`
	IsEnabled     bool       `json:"is_enabled"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// UseRecoveryCode attempts to use a recovery code. Returns true if successful.
func (tf *TwoFactor) UseRecoveryCode(code string) bool {
	for i, c := range tf.RecoveryCodes {
		if c == code {
			tf.RecoveryCodes = append(tf.RecoveryCodes[:i], tf.RecoveryCodes[i+1:]...)
			return true
		}
	}
	return false
}
