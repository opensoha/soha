package copilot

import "time"

// PersonalAssistant is user-owned identity; conversations and runtime tasks keep their own IDs.
type PersonalAssistant struct {
	ID              string    `json:"id"`
	CreatedBy       string    `json:"-"`
	Name            string    `json:"name"`
	AvatarURL       string    `json:"avatarUrl,omitempty"`
	AvatarPosterURL string    `json:"avatarPosterUrl,omitempty"`
	AvatarMotion    string    `json:"avatarMotion,omitempty"`
	MainSessionID   string    `json:"mainSessionId"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type PersonalAssistantAvatar struct {
	AvatarURL       string `json:"avatarUrl"`
	AvatarPosterURL string `json:"avatarPosterUrl,omitempty"`
	AvatarMotion    string `json:"avatarMotion,omitempty"`
}

type PersonalArtifact struct {
	// Retain source lineage for current ACL checks; it is not a public field.
	SourceMetadata map[string]any   `json:"-"`
	ID             string           `json:"id"`
	SessionID      string           `json:"sessionId,omitempty"`
	MessageID      string           `json:"messageId,omitempty"`
	TaskID         string           `json:"taskId,omitempty"`
	Artifact       AnalysisArtifact `json:"artifact"`
	CreatedAt      time.Time        `json:"createdAt"`
}
