package workflow

import (
	"time"

	domainbuild "github.com/opensoha/soha/internal/domain/build"
)

type ExecutionHistoryFilter struct {
	ApplicationID, ServiceID, ApplicationEnvironmentID string
	WorkflowID, BuildSourceID, Status, Search, Cursor  string
	Limit                                              int
	IncludeWorkflows, IncludeBuilds                    bool
}

type ExecutionHistoryPosition struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

type ExecutionHistoryEntry struct {
	ExecutionHistoryPosition
	Batch       *DeliveryBatch      `json:"batch,omitempty"`
	Application *Run                `json:"application,omitempty"`
	Build       *domainbuild.Record `json:"build,omitempty"`
}

type ExecutionHistoryPage struct {
	Items      []ExecutionHistoryEntry `json:"items"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}
