package copilot

import (
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"time"
)

type PersonalWorkspaceRuntime struct {
	AssistantID        string                         `json:"assistantId"`
	EnvironmentVersion int64                          `json:"environmentVersion"`
	Latest             *PersonalRuntime               `json:"latest,omitempty"`
	BrowserControl     *PersonalRuntimeBrowserControl `json:"browserControl,omitempty"`
}
type PersonalRuntimeBrowserControl struct {
	ControlID  string    `json:"controlId"`
	Generation int64     `json:"generation"`
	Owner      string    `json:"owner"`
	TaskID     string    `json:"taskId,omitempty"`
	InstanceID string    `json:"instanceId,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type PersonalRuntime struct {
	ID                 string                   `json:"id"`
	Kind               string                   `json:"kind"`
	Status             string                   `json:"status"`
	AssistantID        string                   `json:"assistantId"`
	EnvironmentVersion int64                    `json:"environmentVersion"`
	Output             string                   `json:"output,omitempty"`
	ExitCode           *int                     `json:"exitCode,omitempty"`
	File               *guest.RuntimeFileResult `json:"file,omitempty"`
	Browser            *guest.BrowserFrame      `json:"browser,omitempty"`
}
