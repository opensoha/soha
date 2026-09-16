package execution

import (
	"context"
	"crypto/sha256"
	"fmt"
)

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

type DeliveryJobStopRuntime interface {
	StopDeliveryJob(context.Context, string, ExecutionJobRequest) (ExecutionJobInspection, error)
}

type ExecutionJobRequest struct {
	TaskID          string
	Name            string
	Retain          bool
	TaskKind        string
	Namespace       string
	Commands        []string
	Runtime         map[string]any
	Workspace       map[string]any
	DefaultImage    string
	DefaultGitImage string
	TTLSeconds      int
	TimeoutSeconds  int `json:",omitempty"`
}

type ExecutionJobRef struct {
	ClusterID string
	Namespace string
	Name      string
	TaskID    string
}

type ExecutionJobState string

const (
	ExecutionJobRunning   ExecutionJobState = "running"
	ExecutionJobSucceeded ExecutionJobState = "succeeded"
	ExecutionJobFailed    ExecutionJobState = "failed"
	ExecutionJobCanceled  ExecutionJobState = "canceled"
)

type ExecutionJobLog struct {
	Message       string
	PodName       string
	ContainerName string
}

type ExecutionJobInspection struct {
	State         ExecutionJobState
	Logs          []ExecutionJobLog
	ImageDigest   string
	FailureReason string
}

func DeliveryJobName(taskID string) string {
	// Delivery attempts use distinct Task IDs and never retry a historical task.
	digest := sha256.Sum256([]byte(taskID))
	return fmt.Sprintf("soha-exec-%x", digest[:20])
}
