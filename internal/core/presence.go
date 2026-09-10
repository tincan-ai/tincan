package core

import "time"

const PresenceInterval = 30 * time.Second
const PresenceTTL = 90 * time.Second

type AgentPresence struct {
	Status     string     `json:"presence"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	ExpiresAt  *time.Time `json:"presence_expires_at"`
}
