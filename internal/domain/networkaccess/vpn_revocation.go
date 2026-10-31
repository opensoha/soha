package networkaccess

import "time"

type VPNRevocationExecution struct {
	RuntimeID            string     `json:"runtimeId"`
	ConfigurationVersion int        `json:"configurationVersion"`
	ApplyStatus          string     `json:"applyStatus"`
	ReasonCode           string     `json:"reasonCode,omitempty"`
	AppliedAt            *time.Time `json:"appliedAt,omitempty"`
}

type VPNRevocation struct {
	SessionID  string                   `json:"sessionId"`
	SubjectID  string                   `json:"subjectId"`
	DeviceID   string                   `json:"deviceId"`
	SiteID     string                   `json:"siteId"`
	Epoch      int64                    `json:"epoch"`
	Active     bool                     `json:"active"`
	ReasonCode string                   `json:"reasonCode"`
	CreatedAt  time.Time                `json:"createdAt"`
	ReleasedAt *time.Time               `json:"releasedAt,omitempty"`
	Executions []VPNRevocationExecution `json:"executions"`
}
