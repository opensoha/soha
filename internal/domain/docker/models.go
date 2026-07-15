package docker

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type Host struct {
	ID                         string         `json:"id"`
	Name                       string         `json:"name"`
	Status                     string         `json:"status"`
	Endpoint                   string         `json:"endpoint,omitempty"`
	AgentID                    string         `json:"agentId,omitempty"`
	AgentVersion               string         `json:"agentVersion,omitempty"`
	DockerVersion              string         `json:"dockerVersion,omitempty"`
	ComposeVersion             string         `json:"composeVersion,omitempty"`
	Architecture               string         `json:"architecture,omitempty"`
	Environment                string         `json:"environment,omitempty"`
	Owner                      string         `json:"owner,omitempty"`
	Team                       string         `json:"team,omitempty"`
	VirtualizationConnectionID string         `json:"virtualizationConnectionId,omitempty"`
	VMID                       string         `json:"vmId,omitempty"`
	VMName                     string         `json:"vmName,omitempty"`
	IPAddress                  string         `json:"ipAddress,omitempty"`
	CPUCoreCount               int            `json:"cpuCoreCount"`
	MemoryBytes                int64          `json:"memoryBytes"`
	DiskBytes                  int64          `json:"diskBytes"`
	AvailablePortStart         int            `json:"availablePortStart"`
	AvailablePortEnd           int            `json:"availablePortEnd"`
	Labels                     map[string]any `json:"labels,omitempty"`
	Config                     map[string]any `json:"config,omitempty"`
	LastHeartbeatAt            *time.Time     `json:"lastHeartbeatAt,omitempty"`
	CreatedAt                  time.Time      `json:"createdAt"`
	UpdatedAt                  time.Time      `json:"updatedAt"`
}

type HostInput struct {
	ID                         string         `json:"id,omitempty"`
	Name                       string         `json:"name"`
	Status                     string         `json:"status,omitempty"`
	Endpoint                   string         `json:"endpoint,omitempty"`
	AgentID                    string         `json:"agentId,omitempty"`
	AgentVersion               string         `json:"agentVersion,omitempty"`
	DockerVersion              string         `json:"dockerVersion,omitempty"`
	ComposeVersion             string         `json:"composeVersion,omitempty"`
	Architecture               string         `json:"architecture,omitempty"`
	Environment                string         `json:"environment,omitempty"`
	Owner                      string         `json:"owner,omitempty"`
	Team                       string         `json:"team,omitempty"`
	VirtualizationConnectionID string         `json:"virtualizationConnectionId,omitempty"`
	VMID                       string         `json:"vmId,omitempty"`
	VMName                     string         `json:"vmName,omitempty"`
	IPAddress                  string         `json:"ipAddress,omitempty"`
	CPUCoreCount               int            `json:"cpuCoreCount,omitempty"`
	MemoryBytes                int64          `json:"memoryBytes,omitempty"`
	DiskBytes                  int64          `json:"diskBytes,omitempty"`
	AvailablePortStart         int            `json:"availablePortStart,omitempty"`
	AvailablePortEnd           int            `json:"availablePortEnd,omitempty"`
	Labels                     map[string]any `json:"labels,omitempty"`
	Config                     map[string]any `json:"config,omitempty"`
}

type HostFilter struct {
	Status       string
	Search       string
	Environment  string
	Architecture string
	Page         int
	PageSize     int
	Limit        int
}

type Project struct {
	ID             string         `json:"id"`
	HostID         string         `json:"hostId"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    string         `json:"description,omitempty"`
	Environment    string         `json:"environment,omitempty"`
	Owner          string         `json:"owner,omitempty"`
	Team           string         `json:"team,omitempty"`
	SourceKind     string         `json:"sourceKind,omitempty"`
	SourceRef      string         `json:"sourceRef,omitempty"`
	ComposeContent string         `json:"composeContent,omitempty"`
	EnvContent     string         `json:"envContent,omitempty"`
	Status         string         `json:"status"`
	DesiredState   string         `json:"desiredState,omitempty"`
	TemplateID     string         `json:"templateId,omitempty"`
	TTLSeconds     int            `json:"ttlSeconds,omitempty"`
	ExpiresAt      *time.Time     `json:"expiresAt,omitempty"`
	LastDeployedAt *time.Time     `json:"lastDeployedAt,omitempty"`
	Labels         map[string]any `json:"labels,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type ProjectInput struct {
	ID             string         `json:"id,omitempty"`
	HostID         string         `json:"hostId"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug,omitempty"`
	Description    string         `json:"description,omitempty"`
	Environment    string         `json:"environment,omitempty"`
	Owner          string         `json:"owner,omitempty"`
	Team           string         `json:"team,omitempty"`
	SourceKind     string         `json:"sourceKind,omitempty"`
	SourceRef      string         `json:"sourceRef,omitempty"`
	ComposeContent string         `json:"composeContent,omitempty"`
	EnvContent     string         `json:"envContent,omitempty"`
	Status         string         `json:"status,omitempty"`
	DesiredState   string         `json:"desiredState,omitempty"`
	TemplateID     string         `json:"templateId,omitempty"`
	TTLSeconds     int            `json:"ttlSeconds,omitempty"`
	Labels         map[string]any `json:"labels,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
}

type ProjectFilter struct {
	HostID      string
	Status      string
	SourceKind  string
	Search      string
	Environment string
	Page        int
	PageSize    int
	Limit       int
}

type ProjectRuntimeLogs struct {
	ProjectID   string `json:"projectId"`
	ServiceName string `json:"serviceName,omitempty"`
	TailLines   int    `json:"tailLines"`
	Content     string `json:"content"`
	Source      string `json:"source"`
}

type ProjectVolume struct {
	Name            string `json:"name,omitempty"`
	Type            string `json:"type,omitempty"`
	Source          string `json:"source,omitempty"`
	Target          string `json:"target"`
	ReadOnly        bool   `json:"readOnly"`
	SubPath         string `json:"subPath,omitempty"`
	BrowseSupported bool   `json:"browseSupported"`
}

type ProjectVolumeFileListInput struct {
	ServiceName string `json:"serviceName,omitempty"`
	Target      string `json:"target"`
	Path        string `json:"path,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type ProjectVolumeFileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
}

type ProjectVolumeFileList struct {
	ProjectID   string                   `json:"projectId"`
	ServiceName string                   `json:"serviceName"`
	Target      string                   `json:"target"`
	Path        string                   `json:"path"`
	Items       []ProjectVolumeFileEntry `json:"items"`
}

type ProjectVolumeFileReadInput struct {
	ServiceName string `json:"serviceName,omitempty"`
	Target      string `json:"target"`
	Path        string `json:"path"`
	LimitBytes  int64  `json:"limitBytes,omitempty"`
}

type ProjectVolumeFileContent struct {
	ProjectID   string `json:"projectId"`
	ServiceName string `json:"serviceName"`
	Target      string `json:"target"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	SizeBytes   int64  `json:"sizeBytes"`
	Truncated   bool   `json:"truncated"`
}

type Service struct {
	ID             string         `json:"id"`
	ProjectID      string         `json:"projectId"`
	HostID         string         `json:"hostId"`
	Name           string         `json:"name"`
	Image          string         `json:"image,omitempty"`
	Status         string         `json:"status"`
	ContainerID    string         `json:"containerId,omitempty"`
	RestartCount   int            `json:"restartCount"`
	CPUPercent     float64        `json:"cpuPercent"`
	MemoryBytes    int64          `json:"memoryBytes"`
	NetworkRxBytes int64          `json:"networkRxBytes"`
	NetworkTxBytes int64          `json:"networkTxBytes"`
	Config         map[string]any `json:"config,omitempty"`
	LastSeenAt     *time.Time     `json:"lastSeenAt,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type ServiceInput struct {
	ID             string         `json:"id,omitempty"`
	ProjectID      string         `json:"projectId"`
	HostID         string         `json:"hostId"`
	Name           string         `json:"name"`
	Image          string         `json:"image,omitempty"`
	Status         string         `json:"status,omitempty"`
	ContainerID    string         `json:"containerId,omitempty"`
	RestartCount   int            `json:"restartCount,omitempty"`
	CPUPercent     float64        `json:"cpuPercent,omitempty"`
	MemoryBytes    int64          `json:"memoryBytes,omitempty"`
	NetworkRxBytes int64          `json:"networkRxBytes,omitempty"`
	NetworkTxBytes int64          `json:"networkTxBytes,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
}

type ServiceFilter struct {
	HostID    string
	ProjectID string
	Status    string
	Search    string
	Page      int
	PageSize  int
	Limit     int
}

type PortMapping struct {
	ID               string         `json:"id"`
	HostID           string         `json:"hostId"`
	ProjectID        string         `json:"projectId,omitempty"`
	ServiceID        string         `json:"serviceId,omitempty"`
	Name             string         `json:"name"`
	HostIP           string         `json:"hostIp,omitempty"`
	HostPort         int            `json:"hostPort"`
	ContainerPort    int            `json:"containerPort"`
	Protocol         string         `json:"protocol"`
	ExposureScope    string         `json:"exposureScope"`
	Status           string         `json:"status"`
	DomainName       string         `json:"domainName,omitempty"`
	DomainScheme     string         `json:"domainScheme,omitempty"`
	DomainTLSEnabled bool           `json:"domainTlsEnabled"`
	AccessURL        string         `json:"accessUrl,omitempty"`
	Owner            string         `json:"owner,omitempty"`
	ExpiresAt        *time.Time     `json:"expiresAt,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

type PortMappingInput struct {
	ID               string         `json:"id,omitempty"`
	HostID           string         `json:"hostId"`
	ProjectID        string         `json:"projectId,omitempty"`
	ServiceID        string         `json:"serviceId,omitempty"`
	Name             string         `json:"name"`
	HostIP           string         `json:"hostIp,omitempty"`
	HostPort         int            `json:"hostPort"`
	ContainerPort    int            `json:"containerPort"`
	Protocol         string         `json:"protocol,omitempty"`
	ExposureScope    string         `json:"exposureScope,omitempty"`
	Status           string         `json:"status,omitempty"`
	DomainName       string         `json:"domainName,omitempty"`
	DomainScheme     string         `json:"domainScheme,omitempty"`
	DomainTLSEnabled bool           `json:"domainTlsEnabled,omitempty"`
	AccessURL        string         `json:"accessUrl,omitempty"`
	Owner            string         `json:"owner,omitempty"`
	ExpiresAt        *time.Time     `json:"expiresAt,omitempty"`
	Config           map[string]any `json:"config,omitempty"`
}

type PortMappingFilter struct {
	HostID     string
	ProjectID  string
	ServiceID  string
	Status     string
	Search     string
	Page       int
	PageSize   int
	Limit      int
	HostPort   int
	HostIP     string
	Protocol   string
	DomainName string
	ExcludeID  string
}

type Template struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	TemplateKind   string         `json:"templateKind"`
	ComposeContent string         `json:"composeContent,omitempty"`
	EnvContent     string         `json:"envContent,omitempty"`
	Variables      map[string]any `json:"variables,omitempty"`
	Enabled        bool           `json:"enabled"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type TemplateInput struct {
	ID             string         `json:"id,omitempty"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	TemplateKind   string         `json:"templateKind,omitempty"`
	ComposeContent string         `json:"composeContent,omitempty"`
	EnvContent     string         `json:"envContent,omitempty"`
	Variables      map[string]any `json:"variables,omitempty"`
	Enabled        bool           `json:"enabled"`
}

type TemplateFilter struct {
	Enabled  *bool
	Kind     string
	Search   string
	Page     int
	PageSize int
	Limit    int
}

type Operation struct {
	ID                string          `json:"id"`
	HostID            string          `json:"hostId,omitempty"`
	ProjectID         string          `json:"projectId,omitempty"`
	ServiceID         string          `json:"serviceId,omitempty"`
	OperationKind     string          `json:"operationKind"`
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

func WithOperationState(operation Operation, now time.Time) Operation {
	operation.OperationState = BuildOperationState(operation, now)
	return operation
}

func WithOperationStates(operations []Operation, now time.Time) []Operation {
	out := make([]Operation, len(operations))
	for index, operation := range operations {
		out[index] = WithOperationState(operation, now)
	}
	return out
}

func BuildOperationState(operation Operation, now time.Time) *OperationState {
	status := strings.TrimSpace(operation.Status)
	timeoutSeconds := operation.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultOperationStateTimeoutSeconds
	}
	terminal := operationStatusTerminal(status)
	heartbeatRequired := status == "running"
	heartbeatReference := operationHeartbeatReference(operation)
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
		ClaimedByWorkerID:     strings.TrimSpace(operation.ClaimedByWorkerID),
		RecommendedNextAction: operationRecommendedNextAction(status, heartbeatStale),
	}
	if operation.LastHeartbeatAt != nil && !operation.LastHeartbeatAt.IsZero() {
		state.LastHeartbeatAt = operation.LastHeartbeatAt.UTC()
	}
	if !nextDeadline.IsZero() {
		state.NextHeartbeatDeadline = nextDeadline.UTC()
	}
	if operation.FinishedAt != nil && !operation.FinishedAt.IsZero() {
		state.FinalStateRecordedAt = operation.FinishedAt.UTC()
	}
	if terminal && status != "completed" {
		state.FailureReason = firstNonEmptyOperationResultString(operation.Result, "failureReason", "reason", "errorCode")
		if state.FailureReason == "" {
			state.FailureReason = status
		}
		state.FailureMessage = firstNonEmptyOperationResultString(operation.Result, "error", "message", "cancelReason")
	}
	return state
}

func operationHeartbeatReference(operation Operation) time.Time {
	if operation.LastHeartbeatAt != nil && !operation.LastHeartbeatAt.IsZero() {
		return operation.LastHeartbeatAt.UTC()
	}
	if operation.StartedAt != nil && !operation.StartedAt.IsZero() {
		return operation.StartedAt.UTC()
	}
	return operation.CreatedAt.UTC()
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

func firstNonEmptyOperationResultString(result map[string]any, keys ...string) string {
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

type OperationInput struct {
	HostID         string         `json:"hostId,omitempty"`
	ProjectID      string         `json:"projectId,omitempty"`
	ServiceID      string         `json:"serviceId,omitempty"`
	OperationKind  string         `json:"operationKind"`
	Status         string         `json:"status,omitempty"`
	RequestedBy    string         `json:"requestedBy,omitempty"`
	MaxRetries     int            `json:"maxRetries,omitempty"`
	TimeoutSeconds int            `json:"timeoutSeconds,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
}

type OperationFilter struct {
	HostID        string
	ProjectID     string
	ServiceID     string
	Status        string
	Statuses      []string
	OperationKind string
	Abnormal      bool
	Pending       bool
	Search        string
	Page          int
	PageSize      int
	Limit         int
}

type OperationClaimInput struct {
	WorkerID       string   `json:"workerId"`
	AgentID        string   `json:"agentId,omitempty"`
	HostIDs        []string `json:"hostIds,omitempty"`
	OperationKinds []string `json:"operationKinds,omitempty"`
}

type OperationCallbackInput struct {
	OperationID string         `json:"operationId"`
	WorkerID    string         `json:"workerId"`
	Status      string         `json:"status"`
	Payload     map[string]any `json:"payload,omitempty"`
	Logs        []string       `json:"logs,omitempty"`
}

type OperationLog struct {
	ID          string         `json:"id"`
	OperationID string         `json:"operationId"`
	LogLevel    string         `json:"logLevel"`
	Message     string         `json:"message"`
	Payload     map[string]any `json:"payload,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type ContainerStartCreateInput struct {
	Project      ProjectInput
	Service      ServiceInput
	PortMapping  PortMappingInput
	PortMappings []PortMappingInput
	Operation    OperationInput
}

type ContainerStartCreateResult struct {
	Project      Project
	Service      Service
	PortMapping  PortMapping
	PortMappings []PortMapping
	Operation    Operation
}

type QuickCreateHostInput struct {
	Name                       string         `json:"name"`
	Environment                string         `json:"environment,omitempty"`
	Owner                      string         `json:"owner,omitempty"`
	Team                       string         `json:"team,omitempty"`
	VirtualizationConnectionID string         `json:"virtualizationConnectionId,omitempty"`
	VMTemplateID               string         `json:"vmTemplateId,omitempty"`
	FlavorID                   string         `json:"flavorId,omitempty"`
	ImageID                    string         `json:"imageId,omitempty"`
	Architecture               string         `json:"architecture,omitempty"`
	CloudInit                  string         `json:"cloudInit,omitempty"`
	CPUCoreCount               int            `json:"cpuCoreCount,omitempty"`
	MemoryBytes                int64          `json:"memoryBytes,omitempty"`
	DiskBytes                  int64          `json:"diskBytes,omitempty"`
	Network                    string         `json:"network,omitempty"`
	AvailablePortStart         int            `json:"availablePortStart,omitempty"`
	AvailablePortEnd           int            `json:"availablePortEnd,omitempty"`
	TTLSeconds                 int            `json:"ttlSeconds,omitempty"`
	Labels                     map[string]any `json:"labels,omitempty"`
	Config                     map[string]any `json:"config,omitempty"`
}

type ContainerPortInput struct {
	Name             string `json:"name,omitempty"`
	HostIP           string `json:"hostIp,omitempty"`
	HostPort         int    `json:"hostPort"`
	ContainerPort    int    `json:"containerPort"`
	Protocol         string `json:"protocol,omitempty"`
	ExposureScope    string `json:"exposureScope,omitempty"`
	DomainName       string `json:"domainName,omitempty"`
	DomainScheme     string `json:"domainScheme,omitempty"`
	DomainTLSEnabled bool   `json:"domainTlsEnabled,omitempty"`
}

type ContainerVolumeInput struct {
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readOnly,omitempty"`
	SubPath  string `json:"subPath,omitempty"`
}

type ContainerEnvironmentVariableInput struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type ContainerResourceInput struct {
	CPUS                   float64 `json:"cpus,omitempty"`
	MemoryBytes            int64   `json:"memoryBytes,omitempty"`
	MemoryReservationBytes int64   `json:"memoryReservationBytes,omitempty"`
}

type ContainerStartInput struct {
	HostID               string                              `json:"hostId"`
	Name                 string                              `json:"name"`
	Image                string                              `json:"image"`
	Architecture         string                              `json:"architecture,omitempty"`
	ImagePullPolicy      string                              `json:"imagePullPolicy,omitempty"`
	ContainerPort        int                                 `json:"containerPort"`
	HostIP               string                              `json:"hostIp,omitempty"`
	HostPort             int                                 `json:"hostPort"`
	Protocol             string                              `json:"protocol,omitempty"`
	ExposureScope        string                              `json:"exposureScope,omitempty"`
	DomainName           string                              `json:"domainName,omitempty"`
	DomainScheme         string                              `json:"domainScheme,omitempty"`
	DomainTLSEnabled     bool                                `json:"domainTlsEnabled,omitempty"`
	Command              string                              `json:"command,omitempty"`
	Entrypoint           string                              `json:"entrypoint,omitempty"`
	EnvContent           string                              `json:"envContent,omitempty"`
	EnvironmentVariables []ContainerEnvironmentVariableInput `json:"environmentVariables,omitempty"`
	RestartPolicy        string                              `json:"restartPolicy,omitempty"`
	Network              string                              `json:"network,omitempty"`
	Ports                []ContainerPortInput                `json:"ports,omitempty"`
	Volumes              []ContainerVolumeInput              `json:"volumes,omitempty"`
	Resources            ContainerResourceInput              `json:"resources,omitempty"`
	Environment          string                              `json:"environment,omitempty"`
	Owner                string                              `json:"owner,omitempty"`
	Team                 string                              `json:"team,omitempty"`
	TTLSeconds           int                                 `json:"ttlSeconds,omitempty"`
	Labels               map[string]any                      `json:"labels,omitempty"`
	Config               map[string]any                      `json:"config,omitempty"`
}

type Repository interface {
	ListHosts(context.Context, HostFilter) ([]Host, error)
	CountHosts(context.Context, HostFilter) (int, error)
	GetHost(context.Context, string) (Host, error)
	CreateHost(context.Context, HostInput) (Host, error)
	UpdateHost(context.Context, string, HostInput) (Host, error)
	DeleteHost(context.Context, string) error

	ListProjects(context.Context, ProjectFilter) ([]Project, error)
	CountProjects(context.Context, ProjectFilter) (int, error)
	GetProject(context.Context, string) (Project, error)
	CreateProject(context.Context, ProjectInput) (Project, error)
	UpdateProject(context.Context, string, ProjectInput) (Project, error)
	DeleteProject(context.Context, string) error

	ListServices(context.Context, ServiceFilter) ([]Service, error)
	CountServices(context.Context, ServiceFilter) (int, error)
	GetService(context.Context, string) (Service, error)
	UpsertService(context.Context, ServiceInput) (Service, error)
	DeleteService(context.Context, string) error

	ListPortMappings(context.Context, PortMappingFilter) ([]PortMapping, error)
	CountPortMappings(context.Context, PortMappingFilter) (int, error)
	GetPortMapping(context.Context, string) (PortMapping, error)
	CreatePortMapping(context.Context, PortMappingInput) (PortMapping, error)
	UpdatePortMapping(context.Context, string, PortMappingInput) (PortMapping, error)
	DeletePortMapping(context.Context, string) error

	CreateContainerStart(context.Context, ContainerStartCreateInput) (ContainerStartCreateResult, error)

	ListTemplates(context.Context, TemplateFilter) ([]Template, error)
	CountTemplates(context.Context, TemplateFilter) (int, error)
	GetTemplate(context.Context, string) (Template, error)
	CreateTemplate(context.Context, TemplateInput) (Template, error)
	UpdateTemplate(context.Context, string, TemplateInput) (Template, error)
	DeleteTemplate(context.Context, string) error

	CreateOperation(context.Context, OperationInput) (Operation, error)
	UpdateOperation(context.Context, Operation) (Operation, error)
	ClaimOperation(context.Context, string, string, []string, []string, time.Time) (Operation, error)
	GetOperation(context.Context, string) (Operation, error)
	ListOperations(context.Context, OperationFilter) ([]Operation, error)
	CountOperations(context.Context, OperationFilter) (int, error)
	CreateOperationLog(context.Context, OperationLog) error
	ListOperationLogs(context.Context, string, int) ([]OperationLog, error)
	UpdateProjectRuntime(context.Context, string, string, string, *time.Time) (Project, error)
	TouchHostRuntime(context.Context, string, HostInput) (Host, error)
}
