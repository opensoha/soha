package knowledge

type BaseStatus string

const (
	BaseStatusActive   BaseStatus = "active"
	BaseStatusDisabled BaseStatus = "disabled"
)

type SourceStatus string

const (
	SourceStatusPending SourceStatus = "pending"
	SourceStatusReady   SourceStatus = "ready"
	SourceStatusSyncing SourceStatus = "syncing"
	SourceStatusFailed  SourceStatus = "failed"
)

type DocumentStatus string

const (
	DocumentStatusPending DocumentStatus = "pending"
	DocumentStatusIndexed DocumentStatus = "indexed"
	DocumentStatusFailed  DocumentStatus = "failed"
	DocumentStatusDeleted DocumentStatus = "deleted"
)

type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

type IndexStatus string

const (
	IndexStatusBuilding   IndexStatus = "building"
	IndexStatusActive     IndexStatus = "active"
	IndexStatusFailed     IndexStatus = "failed"
	IndexStatusSuperseded IndexStatus = "superseded"
)
