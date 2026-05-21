package virtualization

import (
	"context"
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
	ID                string         `json:"id"`
	Provider          string         `json:"provider"`
	ConnectionID      string         `json:"connectionId,omitempty"`
	VMID              string         `json:"vmId,omitempty"`
	TaskKind          string         `json:"taskKind"`
	Status            string         `json:"status"`
	RequestedBy       string         `json:"requestedBy,omitempty"`
	ClaimedByWorkerID string         `json:"claimedByWorkerId,omitempty"`
	AttemptCount      int            `json:"attemptCount"`
	MaxRetries        int            `json:"maxRetries"`
	TimeoutSeconds    int            `json:"timeoutSeconds"`
	Payload           map[string]any `json:"payload,omitempty"`
	Result            map[string]any `json:"result,omitempty"`
	StartedAt         *time.Time     `json:"startedAt,omitempty"`
	LastHeartbeatAt   *time.Time     `json:"lastHeartbeatAt,omitempty"`
	FinishedAt        *time.Time     `json:"finishedAt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type TaskFilter struct {
	Provider     string
	ConnectionID string
	VMID         string
	Status       string
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
