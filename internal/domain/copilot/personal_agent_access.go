package copilot

import "time"

type PersonalAgentAccess struct {
	UserID      string     `json:"userId"`
	Status      string     `json:"status"`
	Version     int64      `json:"version"`
	RequestedAt *time.Time `json:"requestedAt,omitempty"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
	DecidedBy   string     `json:"decidedBy,omitempty"`
}
