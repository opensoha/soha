package virtualization

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Connection struct {
	ID                   string         `json:"id"`
	Provider             string         `json:"provider"`
	Name                 string         `json:"name"`
	Endpoint             string         `json:"endpoint,omitempty"`
	KubernetesClusterID  string         `json:"kubernetesClusterId,omitempty"`
	DefaultNamespace     string         `json:"defaultNamespace,omitempty"`
	Enabled              bool           `json:"enabled"`
	VerifyTLS            bool           `json:"verifyTls"`
	EncryptedCredential  map[string]any `json:"-"`
	CredentialConfigured bool           `json:"credentialConfigured"`
	Config               map[string]any `json:"config,omitempty"`
	Health               map[string]any `json:"health,omitempty"`
	LastSyncedAt         *time.Time     `json:"lastSyncedAt,omitempty"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type ConnectionInput struct {
	ID                  string         `json:"id,omitempty"`
	Provider            string         `json:"provider"`
	Name                string         `json:"name"`
	Endpoint            string         `json:"endpoint,omitempty"`
	KubernetesClusterID string         `json:"kubernetesClusterId,omitempty"`
	DefaultNamespace    string         `json:"defaultNamespace,omitempty"`
	Enabled             bool           `json:"enabled"`
	VerifyTLS           bool           `json:"verifyTls"`
	EncryptedCredential map[string]any `json:"-"`
	Config              map[string]any `json:"config,omitempty"`
	Health              map[string]any `json:"health,omitempty"`
}

type ConnectionFilter struct {
	Provider            string
	KubernetesClusterID string
	Enabled             *bool
	Search              string
	Page                int
	PageSize            int
	Limit               int
}

type ConnectionDeleteDependencies struct {
	Connection       Connection                         `json:"connection"`
	VMCount          int                                `json:"vmCount"`
	ImageCount       int                                `json:"imageCount"`
	FlavorCount      int                                `json:"flavorCount"`
	TaskCount        int                                `json:"taskCount"`
	PendingTaskCount int                                `json:"pendingTaskCount"`
	DockerHostCount  int                                `json:"dockerHostCount"`
	VMSamples        []ConnectionDeleteDependencySample `json:"vmSamples,omitempty"`
	ImageSamples     []ConnectionDeleteDependencySample `json:"imageSamples,omitempty"`
	FlavorSamples    []ConnectionDeleteDependencySample `json:"flavorSamples,omitempty"`
	TaskSamples      []ConnectionDeleteDependencySample `json:"taskSamples,omitempty"`
	ForceRequired    bool                               `json:"forceRequired"`
	Blocking         bool                               `json:"blocking"`
	BlockingReasons  []string                           `json:"blockingReasons,omitempty"`
}

type ConnectionDeleteDependencySample struct {
	ID         string `json:"id"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	ExternalID string `json:"externalId,omitempty"`
	Status     string `json:"status,omitempty"`
	NodeName   string `json:"nodeName,omitempty"`
	TaskKind   string `json:"taskKind,omitempty"`
	VMID       string `json:"vmId,omitempty"`
}

type VM struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	ConnectionID string         `json:"connectionId"`
	ExternalID   string         `json:"externalId"`
	Name         string         `json:"name"`
	Namespace    string         `json:"namespace,omitempty"`
	Status       string         `json:"status"`
	PowerState   string         `json:"powerState,omitempty"`
	NodeName     string         `json:"nodeName,omitempty"`
	ImageID      string         `json:"imageId,omitempty"`
	FlavorID     string         `json:"flavorId,omitempty"`
	IPAddresses  []string       `json:"ipAddresses,omitempty"`
	Labels       map[string]any `json:"labels,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
	LastSeenAt   *time.Time     `json:"lastSeenAt,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type VMFilter struct {
	Provider     string
	ConnectionID string
	Namespace    string
	Status       string
	Search       string
	Page         int
	PageSize     int
	Limit        int
}

type Image struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	ConnectionID string         `json:"connectionId"`
	ExternalID   string         `json:"externalId"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	OSType       string         `json:"osType,omitempty"`
	Architecture string         `json:"architecture,omitempty"`
	SizeBytes    int64          `json:"sizeBytes,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
	LastSeenAt   *time.Time     `json:"lastSeenAt,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type ImageFilter struct {
	Provider     string
	ConnectionID string
	Status       string
	Search       string
	Page         int
	PageSize     int
	Limit        int
}

type Flavor struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	ConnectionID string         `json:"connectionId,omitempty"`
	ExternalID   string         `json:"externalId"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	CPUCores     int            `json:"cpuCores"`
	MemoryMB     int            `json:"memoryMb"`
	DiskGB       int            `json:"diskGb"`
	Config       map[string]any `json:"config,omitempty"`
	Raw          map[string]any `json:"raw,omitempty"`
	LastSeenAt   *time.Time     `json:"lastSeenAt,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type FlavorFilter struct {
	Provider     string
	ConnectionID string
	Status       string
	Search       string
	Page         int
	PageSize     int
	Limit        int
}

type Task struct {
	ID                string          `json:"id"`
	Provider          string          `json:"provider"`
	ConnectionID      string          `json:"connectionId,omitempty"`
	VMID              string          `json:"vmId,omitempty"`
	TaskKind          string          `json:"taskKind"`
	Status            string          `json:"status"`
	RequestedBy       string          `json:"requestedBy,omitempty"`
	ClaimedByWorkerID string          `json:"claimedByWorkerId,omitempty"`
	AttemptCount      int             `json:"attemptCount"`
	MaxRetries        int             `json:"maxRetries"`
	TimeoutSeconds    int             `json:"timeoutSeconds"`
	Payload           map[string]any  `json:"payload,omitempty"`
	Result            map[string]any  `json:"result,omitempty"`
	OperationState    *OperationState `json:"operationState,omitempty" gorm:"-"`
	StartedAt         *time.Time      `json:"startedAt,omitempty"`
	LastHeartbeatAt   *time.Time      `json:"lastHeartbeatAt,omitempty"`
	FinishedAt        *time.Time      `json:"finishedAt,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

type OperationState struct {
	Phase                 string    `json:"phase"`
	Status                string    `json:"status"`
	Terminal              bool      `json:"terminal"`
	Cancelable            bool      `json:"cancelable"`
	Retryable             bool      `json:"retryable"`
	HeartbeatRequired     bool      `json:"heartbeatRequired"`
	TimeoutSeconds        int       `json:"timeoutSeconds"`
	HeartbeatStale        bool      `json:"heartbeatStale"`
	LastHeartbeatAt       time.Time `json:"lastHeartbeatAt,omitempty"`
	NextHeartbeatDeadline time.Time `json:"nextHeartbeatDeadline,omitempty"`
	FailureReason         string    `json:"failureReason,omitempty"`
	FailureMessage        string    `json:"failureMessage,omitempty"`
	FinalStateRecordedAt  time.Time `json:"finalStateRecordedAt,omitempty"`
	ClaimedByWorkerID     string    `json:"claimedByWorkerId,omitempty"`
	RecommendedNextAction string    `json:"recommendedNextAction,omitempty"`
}

const defaultOperationStateTimeoutSeconds = 1800

func WithOperationState(task Task, now time.Time) Task {
	task.OperationState = BuildOperationState(task, now)
	return task
}

func WithOperationStates(tasks []Task, now time.Time) []Task {
	out := make([]Task, len(tasks))
	for index, task := range tasks {
		out[index] = WithOperationState(task, now)
	}
	return out
}

func BuildOperationState(task Task, now time.Time) *OperationState {
	status := strings.TrimSpace(task.Status)
	timeoutSeconds := task.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultOperationStateTimeoutSeconds
	}
	terminal := operationStatusTerminal(status)
	heartbeatRequired := status == "running"
	heartbeatReference := operationHeartbeatReference(task)
	nextDeadline := time.Time{}
	heartbeatStale := false
	if heartbeatRequired {
		nextDeadline = heartbeatReference.Add(time.Duration(timeoutSeconds) * time.Second)
		heartbeatStale = now.After(nextDeadline)
	}
	state := &OperationState{
		Phase:                 operationPhase(status),
		Status:                status,
		Terminal:              terminal,
		Cancelable:            status == "queued" || status == "running",
		Retryable:             status == "failed" || status == "callback_timeout" || status == "canceled",
		HeartbeatRequired:     heartbeatRequired,
		TimeoutSeconds:        timeoutSeconds,
		HeartbeatStale:        heartbeatStale,
		ClaimedByWorkerID:     strings.TrimSpace(task.ClaimedByWorkerID),
		RecommendedNextAction: operationRecommendedNextAction(status, heartbeatStale),
	}
	if task.LastHeartbeatAt != nil && !task.LastHeartbeatAt.IsZero() {
		state.LastHeartbeatAt = task.LastHeartbeatAt.UTC()
	}
	if !nextDeadline.IsZero() {
		state.NextHeartbeatDeadline = nextDeadline.UTC()
	}
	if task.FinishedAt != nil && !task.FinishedAt.IsZero() {
		state.FinalStateRecordedAt = task.FinishedAt.UTC()
	}
	if terminal && status != "completed" {
		state.FailureReason = firstNonEmptyTaskResultString(task.Result, "failureReason", "reason", "errorCode")
		if state.FailureReason == "" {
			state.FailureReason = status
		}
		state.FailureMessage = firstNonEmptyTaskResultString(task.Result, "error", "message", "cancelReason")
	}
	return state
}

func operationHeartbeatReference(task Task) time.Time {
	if task.LastHeartbeatAt != nil && !task.LastHeartbeatAt.IsZero() {
		return task.LastHeartbeatAt.UTC()
	}
	if task.StartedAt != nil && !task.StartedAt.IsZero() {
		return task.StartedAt.UTC()
	}
	return task.CreatedAt.UTC()
}

func operationStatusTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "canceled", "callback_timeout":
		return true
	default:
		return false
	}
}

func operationPhase(status string) string {
	switch strings.TrimSpace(status) {
	case "queued":
		return "pending"
	case "running":
		return "running"
	case "completed":
		return "succeeded"
	case "failed", "callback_timeout":
		return "failed"
	case "canceled":
		return "canceled"
	default:
		return "unknown"
	}
}

func operationRecommendedNextAction(status string, heartbeatStale bool) string {
	switch strings.TrimSpace(status) {
	case "queued":
		return "wait_for_worker_claim"
	case "running":
		if heartbeatStale {
			return "inspect_worker_or_cancel"
		}
		return "wait_for_heartbeat"
	case "failed", "callback_timeout":
		return "inspect_failure_or_retry"
	case "canceled":
		return "retry_or_close"
	case "completed":
		return "inspect_result"
	default:
		return "inspect_status"
	}
}

func firstNonEmptyTaskResultString(result map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := result[key]
		if !ok {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

type TaskFilter struct {
	Provider     string
	ConnectionID string
	VMID         string
	Status       string
	Statuses     []string
	Abnormal     bool
	Pending      bool
	TaskKind     string
	Search       string
	Page         int
	PageSize     int
	Limit        int
}

type TaskLog struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"taskId"`
	LogLevel  string         `json:"logLevel"`
	Message   string         `json:"message"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Repository interface {
	CreateConnection(context.Context, ConnectionInput) (Connection, error)
	UpdateConnection(context.Context, string, ConnectionInput) (Connection, error)
	DeleteConnection(context.Context, string) error
	GetConnection(context.Context, string) (Connection, error)
	ListConnections(context.Context, ConnectionFilter) ([]Connection, error)
	CountConnections(context.Context, ConnectionFilter) (int, error)
	CountDockerHostsByConnection(context.Context, string) (int, error)
	MarkDockerHostsUnavailableByConnection(context.Context, string) error
	MarkDockerHostsUnavailableByVM(context.Context, string) error
	UpsertVM(context.Context, VM) (VM, error)
	GetVM(context.Context, string) (VM, error)
	ListVMs(context.Context, VMFilter) ([]VM, error)
	CountVMs(context.Context, VMFilter) (int, error)
	UpsertImage(context.Context, Image) (Image, error)
	GetImage(context.Context, string) (Image, error)
	ListImages(context.Context, ImageFilter) ([]Image, error)
	CountImages(context.Context, ImageFilter) (int, error)
	UpsertFlavor(context.Context, Flavor) (Flavor, error)
	GetFlavor(context.Context, string) (Flavor, error)
	ListFlavors(context.Context, FlavorFilter) ([]Flavor, error)
	CountFlavors(context.Context, FlavorFilter) (int, error)
	CreateTask(context.Context, Task) (Task, error)
	UpdateTask(context.Context, Task) (Task, error)
	ClaimTask(context.Context, string, time.Time) (Task, error)
	GetTask(context.Context, string) (Task, error)
	ListTasks(context.Context, TaskFilter) ([]Task, error)
	CountTasks(context.Context, TaskFilter) (int, error)
	ListTimedOutTasks(context.Context, time.Time, int) ([]Task, error)
	HeartbeatTask(context.Context, string, string, time.Time) error
	UpdateConnectionHealth(context.Context, string, map[string]any, *time.Time) (Connection, error)
	MarkVMsStale(context.Context, string, string, time.Time) error
	MarkImagesStale(context.Context, string, string, time.Time) error
	MarkFlavorsStale(context.Context, string, string, time.Time) error
	CreateTaskLog(context.Context, TaskLog) error
	ListTaskLogs(context.Context, string, int) ([]TaskLog, error)
}
