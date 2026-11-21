package apphosting

import (
	"slices"
	"time"
)

type RuntimeConfig struct {
	// Environment values are authenticated ciphertext, indexed by variable name.
	Environment               map[string]string `json:"-"`
	DataWarningBytes          int64
	VisitorGrant              VisitorGrant
	VisitorRequest            *VisitorAttributeRequest
	SensitivePublisherIDs     []string
	SensitivePublisherOwnerID string
}

type VisitorGrant struct {
	Fields  []string
	OwnerID string
}

type VisitorAttributeRequest struct {
	ID          string
	OwnerID     string
	Reason      string
	Fields      []string
	RequestedAt time.Time
}

func (c RuntimeConfig) AllowsSensitivePublisher(app App, actorID string) bool {
	return app.IsOwner(actorID) || (c.SensitivePublisherOwnerID == app.OwnerUserID &&
		app.IsOwnerOrMaintainer(actorID) && slices.Contains(c.SensitivePublisherIDs, actorID))
}

func (c RuntimeConfig) ContainsEnvironmentSnapshot(snapshot map[string]string) bool {
	for name, ciphertext := range snapshot {
		if current, ok := c.Environment[name]; !ok || current != ciphertext {
			return false
		}
	}
	return true
}
