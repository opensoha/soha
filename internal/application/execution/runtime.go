package execution

import "context"

type ClusterCatalog interface {
	ClusterIDs() []string
}

type JobRuntime interface {
	CreateExecutionJob(context.Context, string, ExecutionJobRequest) (ExecutionJobRef, error)
	InspectExecutionJob(context.Context, ExecutionJobRef) (ExecutionJobInspection, error)
	DeleteExecutionJob(context.Context, ExecutionJobRef) error
}

type ClusterRuntime interface {
	ClusterCatalog
	JobRuntime
}

type ExecutionJobRequest struct {
	TaskID          string
	TaskKind        string
	Namespace       string
	Commands        []string
	Runtime         map[string]any
	Workspace       map[string]any
	DefaultImage    string
	DefaultGitImage string
	TTLSeconds      int
}

type ExecutionJobRef struct {
	ClusterID string
	Namespace string
	Name      string
}

type ExecutionJobState string

const (
	ExecutionJobRunning   ExecutionJobState = "running"
	ExecutionJobSucceeded ExecutionJobState = "succeeded"
	ExecutionJobFailed    ExecutionJobState = "failed"
)

type ExecutionJobLog struct {
	Message       string
	PodName       string
	ContainerName string
}

type ExecutionJobInspection struct {
	State ExecutionJobState
	Logs  []ExecutionJobLog
}
