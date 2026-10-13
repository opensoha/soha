package copilot

import (
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
)

// Native run IDs include a prefix; the private Agent protocol accepts UUIDs.
func PersonalTaskID(runID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("personal-task\x00"+runID)).String()
}

// TaskID comes from the server's Workflow/Native identity, not tool input.
type PersonalCommandInput struct {
	TaskID         string
	IdempotencyKey string
	Argv           []string
	TaskDeadline   time.Time
}

type PersonalCommand struct {
	ID           string     `json:"id"`
	AssistantID  string     `json:"assistantId"`
	TaskID       string     `json:"taskId"`
	State        string     `json:"status"`
	Version      int64      `json:"version"`
	Output       string     `json:"output,omitempty"`
	OutputDigest string     `json:"outputDigest,omitempty"`
	ObservedAt   *time.Time `json:"observedAt,omitempty"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// The owning service records the dispatch before HTTP. A repeated call can
// recover this record but cannot run argv again, including unknown outcomes.
type PersonalCommandRecord struct {
	PersonalCommand
	CreatedBy      string
	RequestHash    string
	Lease          contractenv.Lease
	EnvOperationID string
	OperationUntil time.Time
}
