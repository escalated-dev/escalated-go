package models

import (
	"math"
	"time"
)

// AgentCapacity tracks an agent's concurrent-ticket load against a ceiling
// for a given channel. Used by services.CapacityService for load-aware
// assignment. Mirrors the Laravel AgentCapacity model (unique per
// user_id + channel).
type AgentCapacity struct {
	ID            int64     `json:"id"`
	UserID        UserID    `json:"user_id"`
	Channel       string    `json:"channel"`
	MaxConcurrent int       `json:"max_concurrent"`
	CurrentCount  int       `json:"current_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// HasCapacity reports whether the agent can take another ticket on this
// channel.
func (c AgentCapacity) HasCapacity() bool {
	return c.CurrentCount < c.MaxConcurrent
}

// LoadPercentage is the current load as a percentage of the ceiling,
// rounded to one decimal place. An uncapped agent (max <= 0) reports 100.
func (c AgentCapacity) LoadPercentage() float64 {
	if c.MaxConcurrent <= 0 {
		return 100.0
	}
	pct := float64(c.CurrentCount) / float64(c.MaxConcurrent) * 100
	return math.Round(pct*10) / 10
}
