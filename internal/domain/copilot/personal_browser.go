package copilot

import "time"

type PersonalBrowserControl struct {
	AssistantID, OwnerID, ID, Owner, TaskID, InstanceID string
	Generation                                          int64
	ExpiresAt                                           time.Time
}
