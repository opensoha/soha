package virtualization

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainoperation "github.com/kubecrux/kubecrux/internal/domain/operation"
	domainvirtualization "github.com/kubecrux/kubecrux/internal/domain/virtualization"
	infravirtualization "github.com/kubecrux/kubecrux/internal/infrastructure/virtualization"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/operationentry"
	"github.com/kubecrux/kubecrux/internal/platform/runtimeobs"
)

const (
	ProviderKubeVirt = "kubevirt"
	ProviderPVE      = "pve"

	TaskKindVMCreate  = "vm_create"
	TaskKindVMAction  = "vm_action"
	TaskKindAssetSync = "asset_sync"

	TaskStatusQueued    = "queued"
	TaskStatusRunning   = "running"
	TaskStatusSucceeded = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCanceled  = "canceled"
	TaskStatusTimeout   = "callback_timeout"

	defaultTaskMaxRetries     = 1
	defaultTaskTimeoutSeconds = 1800
)

type Repository = domainvirtualization.Repository

type Adapter = infravirtualization.Adapter

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo               Repository
	adapters           map[string]Adapter
	permissions        *appaccess.PermissionResolver
	operations         OperationRecorder
	credentialKey      string
	workerInterval     time.Duration
	startupSyncEnabled bool
	workerOnce         sync.Once
	workerCancel       context.CancelFunc
	workerDone         chan struct{}
	workerID           string
	workerPrincipal    domainidentity.Principal
	metrics            *runtimeobs.Registry
	secretProvider     CredentialProvider
}

type Options struct {
	CredentialEncryptionKey string
	StartupSyncEnabled      bool
	WorkerInterval          time.Duration
	CredentialProvider      CredentialProvider
}

type CredentialProvider interface {
	ResolveVirtualizationCredential(context.Context, domainvirtualization.Connection) (map[string]any, error)
}

type ConnectionInput struct {
	ID                  string         `json:"id,omitempty"`
	Provider            string         `json:"provider"`
	Name                string         `json:"name"`
	Endpoint            string         `json:"endpoint,omitempty"`
	KubernetesClusterID string         `json:"kubernetesClusterId,omitempty"`
	DefaultNamespace    string         `json:"defaultNamespace,omitempty"`
	Enabled             *bool          `json:"enabled,omitempty"`
	VerifyTLS           *bool          `json:"verifyTls,omitempty"`
	Credential          map[string]any `json:"credential,omitempty"`
	Config              map[string]any `json:"config,omitempty"`
	Region              string         `json:"region,omitempty"`
	Description         string         `json:"description,omitempty"`
}

type CreateVMInput struct {
	ConnectionID      string         `json:"connectionId"`
	Name              string         `json:"name"`
	Namespace         string         `json:"namespace,omitempty"`
	Node              string         `json:"node,omitempty"`
	CPU               int            `json:"cpu,omitempty"`
	MemoryMiB         int            `json:"memoryMiB,omitempty"`
	BootImageID       string         `json:"bootImageId,omitempty"`
	ImageID           string         `json:"imageId,omitempty"`
	FlavorID          string         `json:"flavorId,omitempty"`
	DiskGiB           int            `json:"diskGiB,omitempty"`
	Network           string         `json:"network,omitempty"`
	CloudInit         string         `json:"cloudInit,omitempty"`
	StartAfterCreate  bool           `json:"startAfterCreate,omitempty"`
	TemplateID        string         `json:"templateId,omitempty"`
	ProviderParams    map[string]any `json:"providerParams,omitempty"`
	ProviderExtraJSON map[string]any
}

type VMActionInput struct {
	Action string         `json:"action"`
	Config map[string]any `json:"config,omitempty"`
}

type ImageInput struct {
	ID           string         `json:"id,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	ConnectionID string         `json:"connectionId"`
	ExternalID   string         `json:"externalId,omitempty"`
	Name         string         `json:"name"`
	Status       string         `json:"status,omitempty"`
	Description  string         `json:"description,omitempty"`
	OSType       string         `json:"osType,omitempty"`
	Architecture string         `json:"architecture,omitempty"`
	SizeBytes    int64          `json:"sizeBytes,omitempty"`
	SizeGiB      int64          `json:"sizeGiB,omitempty"`
	SourceKind   string         `json:"sourceKind,omitempty"`
	Namespace    string         `json:"namespace,omitempty"`
	StorageClass string         `json:"storageClass,omitempty"`
	URL          string         `json:"url,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
}

type FlavorInput struct {
	ID           string         `json:"id,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	ConnectionID string         `json:"connectionId,omitempty"`
	ExternalID   string         `json:"externalId,omitempty"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	CPUCores     int            `json:"cpuCores,omitempty"`
	CPU          int            `json:"cpu,omitempty"`
	MemoryMB     int            `json:"memoryMb,omitempty"`
	MemoryMiB    int            `json:"memoryMiB,omitempty"`
	DiskGB       int            `json:"diskGb,omitempty"`
	DiskGiB      int            `json:"diskGiB,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
}

type Overview struct {
	Stats             OverviewStats               `json:"stats"`
	RecentOperations  []domainvirtualization.Task `json:"recentOperations"`
	LastSyncTask      *domainvirtualization.Task  `json:"lastSyncTask,omitempty"`
	ConnectionSummary OverviewConnectionSummary   `json:"connectionSummary"`
	TaskSummary       OverviewTaskSummary         `json:"taskSummary"`
	Attention         OverviewAttention           `json:"attention"`
}

type OverviewStats struct {
	Connections      ConnectionHealthStats `json:"connections"`
	VMCount          int                   `json:"vmCount"`
	RunningVMCount   int                   `json:"runningVmCount"`
	StoppedVMCount   int                   `json:"stoppedVmCount"`
	ImageCount       int                   `json:"imageCount"`
	FlavorCount      int                   `json:"flavorCount"`
	PendingTaskCount int                   `json:"pendingTaskCount"`
	FailedTaskCount  int                   `json:"failedTaskCount"`
}

type OverviewConnectionSummary struct {
	Total             int `json:"total"`
	Healthy           int `json:"healthy"`
	Degraded          int `json:"degraded"`
	Unavailable       int `json:"unavailable"`
	NeverSynced       int `json:"neverSynced"`
	CredentialMissing int `json:"credentialMissing"`
}

type OverviewTaskSummary struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Failed    int `json:"failed"`
	Timeout   int `json:"timeout"`
	Canceled  int `json:"canceled"`
	Completed int `json:"completed"`
}

type OverviewAttention struct {
	RiskyConnections []domainvirtualization.Connection `json:"riskyConnections"`
	FailedSyncTasks  []domainvirtualization.Task       `json:"failedSyncTasks"`
	FailedOperations []domainvirtualization.Task       `json:"failedOperations"`
}

type VMDetail struct {
	VM         domainvirtualization.VM          `json:"vm"`
	Connection *domainvirtualization.Connection `json:"connection,omitempty"`
	Image      *domainvirtualization.Image      `json:"image,omitempty"`
	Flavor     *domainvirtualization.Flavor     `json:"flavor,omitempty"`
	Operations []OperationWithLogs              `json:"operations"`
	Logs       []domainvirtualization.TaskLog   `json:"logs"`
}

type OperationWithLogs struct {
	Task domainvirtualization.Task      `json:"task"`
	Logs []domainvirtualization.TaskLog `json:"logs,omitempty"`
}

type ConnectionHealthStats struct {
	Total       int `json:"total"`
	Healthy     int `json:"healthy"`
	Degraded    int `json:"degraded"`
	Unavailable int `json:"unavailable"`
}

func New(repo Repository, adapters map[string]Adapter, permissions *appaccess.PermissionResolver, operations OperationRecorder, opts Options) *Service {
	normalized := make(map[string]Adapter, len(adapters))
	for provider, adapter := range adapters {
		if adapter != nil {
			normalized[normalizeProvider(provider)] = adapter
		}
	}
	interval := opts.WorkerInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Service{
		repo:               repo,
		adapters:           normalized,
		permissions:        permissions,
		operations:         operations,
		credentialKey:      strings.TrimSpace(opts.CredentialEncryptionKey),
		workerInterval:     interval,
		startupSyncEnabled: opts.StartupSyncEnabled,
		secretProvider:     opts.CredentialProvider,
		workerID:           "virtualization-worker-" + uuid.NewString(),
		workerPrincipal: domainidentity.Principal{
			UserID:   "system",
			UserName: "System",
			Roles:    []string{"admin"},
		},
	}
}

func (s *Service) SetInstrumentation(metrics *runtimeobs.Registry) {
	s.metrics = metrics
}

func (s *Service) Overview(ctx context.Context, principal domainidentity.Principal) (Overview, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationOverviewView); err != nil {
		return Overview{}, err
	}
	connections, err := s.repo.ListConnections(ctx, domainvirtualization.ConnectionFilter{Limit: 1000})
	if err != nil {
		return Overview{}, err
	}
	vms, err := s.repo.ListVMs(ctx, domainvirtualization.VMFilter{Limit: 1000})
	if err != nil {
		return Overview{}, err
	}
	images, err := s.repo.ListImages(ctx, domainvirtualization.ImageFilter{Limit: 1000})
	if err != nil {
		return Overview{}, err
	}
	flavors, err := s.repo.ListFlavors(ctx, domainvirtualization.FlavorFilter{Limit: 1000})
	if err != nil {
		return Overview{}, err
	}
	recentTasks, err := s.repo.ListTasks(ctx, domainvirtualization.TaskFilter{Limit: 20})
	if err != nil {
		return Overview{}, err
	}
	attentionTasks, err := s.repo.ListTasks(ctx, domainvirtualization.TaskFilter{Limit: 100})
	if err != nil {
		return Overview{}, err
	}
	out := Overview{RecentOperations: recentTasks}
	out.Stats.Connections.Total = len(connections)
	out.ConnectionSummary.Total = len(connections)
	for _, connection := range connections {
		healthStatus := strings.ToLower(stringValue(connection.Health, "status"))
		switch healthStatus {
		case "healthy":
			out.Stats.Connections.Healthy++
			out.ConnectionSummary.Healthy++
		case "degraded":
			out.Stats.Connections.Degraded++
			out.ConnectionSummary.Degraded++
		default:
			out.Stats.Connections.Unavailable++
			out.ConnectionSummary.Unavailable++
		}
		if connection.LastSyncedAt == nil {
			out.ConnectionSummary.NeverSynced++
		}
		if connection.Enabled && !connection.CredentialConfigured {
			out.ConnectionSummary.CredentialMissing++
		}
	}
	out.Stats.VMCount = len(vms)
	for _, vm := range vms {
		switch strings.ToLower(firstNonEmpty(vm.PowerState, vm.Status)) {
		case "running", "active", "started":
			out.Stats.RunningVMCount++
		case "stopped", "halted", "shutdown":
			out.Stats.StoppedVMCount++
		}
	}
	out.Stats.ImageCount = len(images)
	out.Stats.FlavorCount = countActiveFlavors(flavors)
	for i := range attentionTasks {
		switch attentionTasks[i].Status {
		case TaskStatusQueued:
			out.Stats.PendingTaskCount++
			out.TaskSummary.Queued++
		case TaskStatusRunning:
			out.Stats.PendingTaskCount++
			out.TaskSummary.Running++
		case TaskStatusFailed:
			out.Stats.FailedTaskCount++
			out.TaskSummary.Failed++
		case TaskStatusTimeout:
			out.TaskSummary.Timeout++
		case TaskStatusCanceled:
			out.TaskSummary.Canceled++
		case TaskStatusSucceeded:
			out.TaskSummary.Completed++
		}
		if attentionTasks[i].TaskKind == TaskKindAssetSync && out.LastSyncTask == nil {
			item := attentionTasks[i]
			out.LastSyncTask = &item
		}
	}
	out.Attention.RiskyConnections = topRiskyConnections(connections, 5)
	out.Attention.FailedSyncTasks = topFailedTasks(attentionTasks, 5, func(task domainvirtualization.Task) bool {
		return task.TaskKind == TaskKindAssetSync && isAbnormalTaskStatus(task.Status)
	})
	out.Attention.FailedOperations = topFailedTasks(attentionTasks, 5, func(task domainvirtualization.Task) bool {
		return isAbnormalTaskStatus(task.Status)
	})
	return out, nil
}

func (s *Service) ListConnections(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersView); err != nil {
		return nil, err
	}
	items, err := s.repo.ListConnections(ctx, filter)
	if err != nil {
		return nil, err
	}
	return sanitizeConnections(items), nil
}

func (s *Service) ListConnectionsPage(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.ConnectionFilter) (domainvirtualization.Page[domainvirtualization.Connection], error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersView); err != nil {
		return domainvirtualization.Page[domainvirtualization.Connection]{}, err
	}
	filter.Page, filter.PageSize = normalizedPageRequest(filter.Page, filter.PageSize, filter.Limit)
	items, err := s.repo.ListConnections(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Connection]{}, err
	}
	total, err := s.repo.CountConnections(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Connection]{}, err
	}
	return pageOf(sanitizeConnections(items), total, filter.Page, filter.PageSize), nil
}

func (s *Service) CreateConnection(ctx context.Context, principal domainidentity.Principal, input ConnectionInput) (domainvirtualization.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersManage); err != nil {
		return domainvirtualization.Connection{}, err
	}
	repoInput, err := s.connectionInput(input, nil)
	if err != nil {
		return domainvirtualization.Connection{}, err
	}
	item, err := s.repo.CreateConnection(ctx, repoInput)
	if err != nil {
		return domainvirtualization.Connection{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.connection.create", item.ID, item.Name, "success", "created virtualization connection", nil)
	return sanitizeConnection(item), nil
}

func (s *Service) UpdateConnection(ctx context.Context, principal domainidentity.Principal, id string, input ConnectionInput) (domainvirtualization.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersManage); err != nil {
		return domainvirtualization.Connection{}, err
	}
	current, err := s.repo.GetConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return domainvirtualization.Connection{}, mapNotFound(err)
	}
	repoInput, err := s.connectionInput(input, &current)
	if err != nil {
		return domainvirtualization.Connection{}, err
	}
	item, err := s.repo.UpdateConnection(ctx, id, repoInput)
	if err != nil {
		return domainvirtualization.Connection{}, mapNotFound(err)
	}
	s.recordOperation(ctx, principal, "virtualization.connection.update", item.ID, item.Name, "success", "updated virtualization connection", nil)
	return sanitizeConnection(item), nil
}

func (s *Service) DeleteConnection(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersManage); err != nil {
		return err
	}
	current, _ := s.repo.GetConnection(ctx, strings.TrimSpace(id))
	if err := s.repo.DeleteConnection(ctx, id); err != nil {
		return mapNotFound(err)
	}
	s.recordOperation(ctx, principal, "virtualization.connection.delete", id, firstNonEmpty(current.Name, id), "success", "deleted virtualization connection", nil)
	return nil
}

func (s *Service) TestConnection(ctx context.Context, principal domainidentity.Principal, id string) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return domainvirtualization.Task{}, mapNotFound(err)
	}
	adapterConnection, err := s.adapterConnection(connection)
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	adapter, err := s.adapterFor(connection.Provider)
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	result, err := adapter.TestConnection(ctx, adapterConnection)
	status := TaskStatusSucceeded
	message := result.Message
	if err != nil {
		status = TaskStatusFailed
		message = err.Error()
	}
	now := time.Now().UTC()
	health := map[string]any{
		"status":    firstNonEmpty(result.Status, status),
		"healthy":   result.Healthy,
		"message":   message,
		"checkedAt": now.Format(time.RFC3339),
	}
	if err != nil {
		health["status"] = "unavailable"
	}
	_, _ = s.repo.UpdateConnectionHealth(ctx, connection.ID, health, nil)
	task, createErr := s.repo.CreateTask(ctx, domainvirtualization.Task{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		TaskKind:     "connection_test",
		Status:       status,
		RequestedBy:  principal.UserID,
		Payload:      map[string]any{"connectionId": connection.ID},
		Result: map[string]any{
			"healthy": result.Healthy,
			"status":  firstNonEmpty(result.Status, status),
			"message": message,
		},
		StartedAt:  &now,
		FinishedAt: &now,
	})
	if createErr != nil {
		return domainvirtualization.Task{}, createErr
	}
	level := "info"
	if status == TaskStatusFailed {
		level = "error"
	}
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: level, Message: firstNonEmpty(message, "connection test completed")})
	s.recordOperation(ctx, principal, "virtualization.connection.test", connection.ID, connection.Name, status, "tested virtualization connection", map[string]any{"taskId": task.ID})
	if err != nil {
		return task, nil
	}
	return task, nil
}

func (s *Service) SyncConnection(ctx context.Context, principal domainidentity.Principal, id string) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationSyncManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return domainvirtualization.Task{}, mapNotFound(err)
	}
	task, err := s.enqueueSyncTask(ctx, principal, connection, map[string]any{"source": "manual"})
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.sync.enqueue", connection.ID, connection.Name, "success", "enqueued virtualization asset sync", map[string]any{"taskId": task.ID})
	return task, nil
}

func (s *Service) SyncAll(ctx context.Context, principal domainidentity.Principal) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationSyncManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	tasks, err := s.enqueueStartupSync(ctx, principal, map[string]any{"source": "manual_global"})
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	if len(tasks) == 0 {
		return domainvirtualization.Task{}, fmt.Errorf("%w: no enabled virtualization connections to sync", apperrors.ErrInvalidArgument)
	}
	s.recordOperation(ctx, principal, "virtualization.sync.global", "", "all connections", "success", "enqueued global virtualization asset sync", map[string]any{"taskCount": len(tasks)})
	return tasks[0], nil
}

func (s *Service) ListVMs(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.VMFilter) ([]domainvirtualization.VM, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationVMsView); err != nil {
		return nil, err
	}
	return s.repo.ListVMs(ctx, filter)
}

func (s *Service) ListVMsPage(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.VMFilter) (domainvirtualization.Page[domainvirtualization.VM], error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationVMsView); err != nil {
		return domainvirtualization.Page[domainvirtualization.VM]{}, err
	}
	filter.Page, filter.PageSize = normalizedPageRequest(filter.Page, filter.PageSize, filter.Limit)
	items, err := s.repo.ListVMs(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.VM]{}, err
	}
	total, err := s.repo.CountVMs(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.VM]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) GetVM(ctx context.Context, principal domainidentity.Principal, id string) (domainvirtualization.VM, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationVMsView); err != nil {
		return domainvirtualization.VM{}, err
	}
	item, err := s.repo.GetVM(ctx, strings.TrimSpace(id))
	return item, mapNotFound(err)
}

func (s *Service) GetVMDetail(ctx context.Context, principal domainidentity.Principal, id string) (VMDetail, error) {
	vm, err := s.GetVM(ctx, principal, id)
	if err != nil {
		return VMDetail{}, err
	}
	detail := VMDetail{VM: vm}
	if connection, err := s.repo.GetConnection(ctx, vm.ConnectionID); err == nil {
		sanitized := sanitizeConnection(connection)
		detail.Connection = &sanitized
	}
	if vm.ImageID != "" {
		if image, err := s.repo.GetImage(ctx, vm.ImageID); err == nil {
			detail.Image = &image
		}
	}
	if vm.FlavorID != "" {
		if flavor, err := s.repo.GetFlavor(ctx, vm.FlavorID); err == nil {
			detail.Flavor = &flavor
		}
	}
	tasks, err := s.repo.ListTasks(ctx, domainvirtualization.TaskFilter{VMID: vm.ID, Limit: 20})
	if err != nil {
		return VMDetail{}, err
	}
	detail.Operations = make([]OperationWithLogs, 0, len(tasks))
	for index, task := range tasks {
		logs, _ := s.repo.ListTaskLogs(ctx, task.ID, 100)
		detail.Operations = append(detail.Operations, OperationWithLogs{Task: task, Logs: logs})
		if index == 0 {
			detail.Logs = logs
		}
	}
	return detail, nil
}

func (s *Service) CreateVM(ctx context.Context, principal domainidentity.Principal, input CreateVMInput) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationVMsManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(input.ConnectionID))
	if err != nil {
		return domainvirtualization.Task{}, mapNotFound(err)
	}
	if strings.TrimSpace(input.Name) == "" {
		return domainvirtualization.Task{}, fmt.Errorf("%w: vm name is required", apperrors.ErrInvalidArgument)
	}
	flavor := domainvirtualization.Flavor{}
	if strings.TrimSpace(input.FlavorID) != "" {
		flavor, err = s.repo.GetFlavor(ctx, strings.TrimSpace(input.FlavorID))
		if err != nil {
			return domainvirtualization.Task{}, mapNotFound(err)
		}
		if flavor.ConnectionID != "" && flavor.ConnectionID != connection.ID {
			return domainvirtualization.Task{}, fmt.Errorf("%w: flavor does not belong to connection", apperrors.ErrInvalidArgument)
		}
		if input.CPU == 0 {
			input.CPU = flavor.CPUCores
		}
		if input.MemoryMiB == 0 {
			input.MemoryMiB = flavor.MemoryMB
		}
		if input.DiskGiB == 0 {
			input.DiskGiB = flavor.DiskGB
		}
	}
	imageID := firstNonEmpty(input.ImageID, input.BootImageID)
	image := domainvirtualization.Image{}
	if strings.TrimSpace(imageID) != "" {
		image, err = s.repo.GetImage(ctx, strings.TrimSpace(imageID))
		if err != nil {
			return domainvirtualization.Task{}, mapNotFound(err)
		}
		if image.ConnectionID != connection.ID {
			return domainvirtualization.Task{}, fmt.Errorf("%w: image does not belong to connection", apperrors.ErrInvalidArgument)
		}
		input.BootImageID = image.ID
	}
	task, err := s.repo.CreateTask(ctx, domainvirtualization.Task{
		Provider:       connection.Provider,
		ConnectionID:   connection.ID,
		TaskKind:       TaskKindVMCreate,
		Status:         TaskStatusQueued,
		RequestedBy:    principal.UserID,
		MaxRetries:     defaultTaskMaxRetries,
		TimeoutSeconds: defaultTaskTimeoutSeconds,
		Payload: map[string]any{
			"name":             input.Name,
			"namespace":        input.Namespace,
			"node":             input.Node,
			"flavorId":         input.FlavorID,
			"cpu":              input.CPU,
			"memoryMiB":        input.MemoryMiB,
			"bootImageId":      input.BootImageID,
			"imageId":          imageID,
			"diskGiB":          input.DiskGiB,
			"network":          input.Network,
			"cloudInit":        input.CloudInit,
			"startAfterCreate": input.StartAfterCreate,
			"templateId":       input.TemplateID,
			"providerParams":   input.ProviderParams,
			"providerExtra":    input.ProviderExtraJSON,
		},
	})
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.vm.create.enqueue", connection.ID, input.Name, "success", "enqueued virtual machine creation", map[string]any{"taskId": task.ID})
	return task, nil
}

func (s *Service) VMAction(ctx context.Context, principal domainidentity.Principal, id string, input VMActionInput) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationVMsManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	vm, err := s.repo.GetVM(ctx, strings.TrimSpace(id))
	if err != nil {
		return domainvirtualization.Task{}, mapNotFound(err)
	}
	action, err := normalizeAction(input.Action)
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	task, err := s.repo.CreateTask(ctx, domainvirtualization.Task{
		Provider:       vm.Provider,
		ConnectionID:   vm.ConnectionID,
		VMID:           vm.ID,
		TaskKind:       TaskKindVMAction,
		Status:         TaskStatusQueued,
		RequestedBy:    principal.UserID,
		MaxRetries:     defaultTaskMaxRetries,
		TimeoutSeconds: defaultTaskTimeoutSeconds,
		Payload: map[string]any{
			"action": string(action),
			"vmId":   vm.ID,
		},
	})
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.vm.action.enqueue", vm.ID, vm.Name, "success", "enqueued virtual machine action", map[string]any{"taskId": task.ID, "action": string(action)})
	return task, nil
}

func (s *Service) ListImages(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.ImageFilter) ([]domainvirtualization.Image, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationImagesView); err != nil {
		return nil, err
	}
	return s.repo.ListImages(ctx, filter)
}

func (s *Service) ListImagesPage(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.ImageFilter) (domainvirtualization.Page[domainvirtualization.Image], error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationImagesView); err != nil {
		return domainvirtualization.Page[domainvirtualization.Image]{}, err
	}
	filter.Page, filter.PageSize = normalizedPageRequest(filter.Page, filter.PageSize, filter.Limit)
	items, err := s.repo.ListImages(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Image]{}, err
	}
	total, err := s.repo.CountImages(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Image]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) CreateImage(ctx context.Context, principal domainidentity.Principal, input ImageInput) (domainvirtualization.Image, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationImagesManage); err != nil {
		return domainvirtualization.Image{}, err
	}
	item, err := s.imageFromInput(ctx, input, "")
	if err != nil {
		return domainvirtualization.Image{}, err
	}
	stored, err := s.repo.UpsertImage(ctx, item)
	if err != nil {
		return domainvirtualization.Image{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.image.create", stored.ID, stored.Name, "success", "created virtualization image or template", nil)
	return stored, nil
}

func (s *Service) UpdateImage(ctx context.Context, principal domainidentity.Principal, id string, input ImageInput) (domainvirtualization.Image, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationImagesManage); err != nil {
		return domainvirtualization.Image{}, err
	}
	item, err := s.imageFromInput(ctx, input, strings.TrimSpace(id))
	if err != nil {
		return domainvirtualization.Image{}, err
	}
	stored, err := s.repo.UpsertImage(ctx, item)
	if err != nil {
		return domainvirtualization.Image{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.image.update", stored.ID, stored.Name, "success", "updated virtualization image or template", nil)
	return stored, nil
}

func (s *Service) DeleteImage(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationImagesManage); err != nil {
		return err
	}
	current, err := s.repo.GetImage(ctx, strings.TrimSpace(id))
	if err != nil {
		return mapNotFound(err)
	}
	current.Status = "deleted"
	if _, err := s.repo.UpsertImage(ctx, current); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "virtualization.image.delete", current.ID, current.Name, "success", "deleted virtualization image or template", nil)
	return nil
}

func (s *Service) ListFlavors(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.FlavorFilter) ([]domainvirtualization.Flavor, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationFlavorsView); err != nil {
		return nil, err
	}
	return s.repo.ListFlavors(ctx, filter)
}

func (s *Service) ListFlavorsPage(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.FlavorFilter) (domainvirtualization.Page[domainvirtualization.Flavor], error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationFlavorsView); err != nil {
		return domainvirtualization.Page[domainvirtualization.Flavor]{}, err
	}
	filter.Page, filter.PageSize = normalizedPageRequest(filter.Page, filter.PageSize, filter.Limit)
	items, err := s.repo.ListFlavors(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Flavor]{}, err
	}
	total, err := s.repo.CountFlavors(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Flavor]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) CreateFlavor(ctx context.Context, principal domainidentity.Principal, input FlavorInput) (domainvirtualization.Flavor, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationFlavorsManage); err != nil {
		return domainvirtualization.Flavor{}, err
	}
	item, err := s.repo.UpsertFlavor(ctx, flavorFromInput(input, ""))
	if err != nil {
		return domainvirtualization.Flavor{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.flavor.create", item.ID, item.Name, "success", "created virtualization flavor", nil)
	return item, nil
}

func (s *Service) UpdateFlavor(ctx context.Context, principal domainidentity.Principal, id string, input FlavorInput) (domainvirtualization.Flavor, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationFlavorsManage); err != nil {
		return domainvirtualization.Flavor{}, err
	}
	item, err := s.repo.UpsertFlavor(ctx, flavorFromInput(input, strings.TrimSpace(id)))
	if err != nil {
		return domainvirtualization.Flavor{}, err
	}
	s.recordOperation(ctx, principal, "virtualization.flavor.update", item.ID, item.Name, "success", "updated virtualization flavor", nil)
	return item, nil
}

func (s *Service) DeleteFlavor(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationFlavorsManage); err != nil {
		return err
	}
	current, err := s.repo.GetFlavor(ctx, strings.TrimSpace(id))
	if err != nil {
		return mapNotFound(err)
	}
	item := current
	item.Status = "deleted"
	if _, err := s.repo.UpsertFlavor(ctx, item); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "virtualization.flavor.delete", id, firstNonEmpty(current.Name, id), "success", "deleted virtualization flavor", nil)
	return nil
}

func (s *Service) ListOperations(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView); err != nil {
		return nil, err
	}
	return s.repo.ListTasks(ctx, filter)
}

func (s *Service) ListOperationsPage(ctx context.Context, principal domainidentity.Principal, filter domainvirtualization.TaskFilter) (domainvirtualization.Page[domainvirtualization.Task], error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView); err != nil {
		return domainvirtualization.Page[domainvirtualization.Task]{}, err
	}
	filter.Page, filter.PageSize = normalizedPageRequest(filter.Page, filter.PageSize, filter.Limit)
	items, err := s.repo.ListTasks(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Task]{}, err
	}
	total, err := s.repo.CountTasks(ctx, filter)
	if err != nil {
		return domainvirtualization.Page[domainvirtualization.Task]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) GetOperation(ctx context.Context, principal domainidentity.Principal, taskID string) (domainvirtualization.Task, error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView); err != nil {
		return domainvirtualization.Task{}, err
	}
	item, err := s.repo.GetTask(ctx, strings.TrimSpace(taskID))
	return item, mapNotFound(err)
}

func (s *Service) ListOperationLogs(ctx context.Context, principal domainidentity.Principal, taskID string, limit int) ([]domainvirtualization.TaskLog, error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView); err != nil {
		return nil, err
	}
	return s.repo.ListTaskLogs(ctx, strings.TrimSpace(taskID), limit)
}

func (s *Service) CancelOperation(ctx context.Context, principal domainidentity.Principal, taskID string) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationOperationsManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	task, err := s.repo.GetTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return domainvirtualization.Task{}, mapNotFound(err)
	}
	if !isCancelableTaskStatus(task.Status) {
		return domainvirtualization.Task{}, fmt.Errorf("%w: virtualization operation %s cannot be canceled from status %s", apperrors.ErrInvalidArgument, task.ID, task.Status)
	}
	now := time.Now().UTC()
	task.Status = TaskStatusCanceled
	task.FinishedAt = &now
	task.Result = mergeMaps(task.Result, map[string]any{
		"message":    "operation canceled",
		"canceledBy": principal.UserID,
		"canceledAt": now.Format(time.RFC3339),
	})
	updated, err := s.repo.UpdateTask(ctx, task)
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: updated.ID, LogLevel: "warn", Message: "operation canceled", Payload: map[string]any{"actor": principal.UserID}})
	s.recordOperation(ctx, principal, "virtualization.operation.cancel", updated.ID, updated.TaskKind, "success", "canceled virtualization operation", map[string]any{"taskId": updated.ID})
	return updated, nil
}

func (s *Service) RetryOperation(ctx context.Context, principal domainidentity.Principal, taskID string) (domainvirtualization.Task, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationOperationsManage); err != nil {
		return domainvirtualization.Task{}, err
	}
	task, err := s.repo.GetTask(ctx, strings.TrimSpace(taskID))
	if err != nil {
		return domainvirtualization.Task{}, mapNotFound(err)
	}
	if !isRetryableTaskStatus(task.Status) {
		return domainvirtualization.Task{}, fmt.Errorf("%w: virtualization operation %s cannot be retried from status %s", apperrors.ErrInvalidArgument, task.ID, task.Status)
	}
	if task.MaxRetries == 0 {
		task.MaxRetries = defaultTaskMaxRetries
	}
	if task.AttemptCount > task.MaxRetries {
		return domainvirtualization.Task{}, fmt.Errorf("%w: virtualization operation %s retry limit reached", apperrors.ErrInvalidArgument, task.ID)
	}
	task.Status = TaskStatusQueued
	task.ClaimedByWorkerID = ""
	task.StartedAt = nil
	task.LastHeartbeatAt = nil
	task.FinishedAt = nil
	task.Result = mergeMaps(task.Result, map[string]any{
		"message":   "operation queued for retry",
		"retriedBy": principal.UserID,
		"retriedAt": time.Now().UTC().Format(time.RFC3339),
	})
	updated, err := s.repo.UpdateTask(ctx, task)
	if err != nil {
		return domainvirtualization.Task{}, err
	}
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: updated.ID, LogLevel: "info", Message: "operation queued for retry", Payload: map[string]any{"actor": principal.UserID}})
	s.recordOperation(ctx, principal, "virtualization.operation.retry", updated.ID, updated.TaskKind, "success", "queued virtualization operation retry", map[string]any{"taskId": updated.ID})
	return updated, nil
}

func (s *Service) GetVMMetrics(ctx context.Context, principal domainidentity.Principal, vmID string, rangeMinutes, stepSeconds int) (infravirtualization.VMMetricsResult, error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermVirtualizationVMsMetrics, appaccess.PermVirtualizationVMsView); err != nil {
		return infravirtualization.VMMetricsResult{}, err
	}

	vm, err := s.repo.GetVM(ctx, vmID)
	if err != nil {
		return infravirtualization.VMMetricsResult{}, err
	}

	connection, err := s.repo.GetConnection(ctx, vm.ConnectionID)
	if err != nil {
		return infravirtualization.VMMetricsResult{}, err
	}

	adapter, err := s.adapterFor(connection.Provider)
	if err != nil {
		return infravirtualization.VMMetricsResult{}, err
	}

	adapterConn, err := s.adapterConnection(connection)
	if err != nil {
		return infravirtualization.VMMetricsResult{}, err
	}

	adapterVM := infravirtualization.VM{
		ID:        vm.ID,
		Name:      vm.Name,
		Namespace: vm.Namespace,
		Node:      vm.NodeName,
		Status:    vm.Status,
		Metadata:  make(map[string]string),
	}
	if vm.Config != nil {
		for k, v := range vm.Config {
			if str, ok := v.(string); ok {
				adapterVM.Metadata[k] = str
			}
		}
	}

	return adapter.GetVMMetrics(ctx, adapterConn, adapterVM, rangeMinutes, stepSeconds)
}

func (s *Service) GetConsoleURL(ctx context.Context, principal domainidentity.Principal, vmID string) (infravirtualization.ConsoleURLResult, error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermVirtualizationVMsConsole, appaccess.PermVirtualizationVMsView); err != nil {
		return infravirtualization.ConsoleURLResult{}, err
	}

	vm, err := s.repo.GetVM(ctx, vmID)
	if err != nil {
		return infravirtualization.ConsoleURLResult{}, err
	}

	connection, err := s.repo.GetConnection(ctx, vm.ConnectionID)
	if err != nil {
		return infravirtualization.ConsoleURLResult{}, err
	}

	adapter, err := s.adapterFor(connection.Provider)
	if err != nil {
		return infravirtualization.ConsoleURLResult{}, err
	}

	adapterConn, err := s.adapterConnection(connection)
	if err != nil {
		return infravirtualization.ConsoleURLResult{}, err
	}

	adapterVM := infravirtualization.VM{
		ID:        vm.ID,
		Name:      vm.Name,
		Namespace: vm.Namespace,
		Node:      vm.NodeName,
		Status:    vm.Status,
		Metadata:  make(map[string]string),
	}
	if vm.Config != nil {
		for k, v := range vm.Config {
			if str, ok := v.(string); ok {
				adapterVM.Metadata[k] = str
			}
		}
	}

	return adapter.GetConsoleURL(ctx, adapterConn, adapterVM)
}

func (s *Service) Start(ctx context.Context) {
	s.workerOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(ctx)
		s.workerCancel = cancel
		s.workerDone = make(chan struct{})
		go s.runWorker(workerCtx)
		if s.startupSyncEnabled {
			_, _ = s.enqueueStartupSync(ctx, s.workerPrincipal, map[string]any{"source": "startup"})
		}
	})
}

func (s *Service) Shutdown() {
	if s.workerCancel != nil {
		s.workerCancel()
	}
	if s.workerDone != nil {
		<-s.workerDone
	}
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) authorizeAny(ctx context.Context, principal domainidentity.Principal, permissionKeys ...string) error {
	var last error
	for _, permissionKey := range permissionKeys {
		if err := s.authorize(ctx, principal, permissionKey); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("%w: missing virtualization permission", apperrors.ErrAccessDenied)
}

func (s *Service) connectionInput(input ConnectionInput, current *domainvirtualization.Connection) (domainvirtualization.ConnectionInput, error) {
	provider := normalizeProvider(input.Provider)
	if provider == "" && current != nil {
		provider = current.Provider
	}
	if provider == "" {
		provider = ProviderKubeVirt
	}
	if provider != ProviderKubeVirt && provider != ProviderPVE {
		return domainvirtualization.ConnectionInput{}, fmt.Errorf("%w: unsupported virtualization provider %q", apperrors.ErrInvalidArgument, provider)
	}
	config := cloneMap(input.Config)
	if config == nil && current != nil {
		config = cloneMap(current.Config)
	}
	if config == nil {
		config = map[string]any{}
	}
	if input.Region != "" {
		config["region"] = input.Region
	}
	if input.Description != "" {
		config["description"] = input.Description
	}
	enabled := true
	if current != nil {
		enabled = current.Enabled
	}
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	verifyTLS := true
	if current != nil {
		verifyTLS = current.VerifyTLS
	}
	if input.VerifyTLS != nil {
		verifyTLS = *input.VerifyTLS
	}
	encrypted := map[string]any{}
	if current != nil {
		encrypted = cloneMap(current.EncryptedCredential)
	}
	if len(input.Credential) > 0 {
		if provider == ProviderPVE && strings.TrimSpace(s.credentialKey) == "" {
			return domainvirtualization.ConnectionInput{}, fmt.Errorf("%w: security.credential_encryption_key is required for PVE credentials", apperrors.ErrInvalidArgument)
		}
		if provider == ProviderPVE {
			sealed, err := infravirtualization.EncryptCredentialJSON(s.credentialKey, input.Credential)
			if err != nil {
				return domainvirtualization.ConnectionInput{}, fmt.Errorf("%w: encrypt pve credential: %v", apperrors.ErrInvalidArgument, err)
			}
			encrypted = map[string]any{"ciphertext": base64.StdEncoding.EncodeToString(sealed)}
		}
	}
	health := map[string]any{"status": "unknown"}
	if current != nil && len(current.Health) > 0 {
		health = cloneMap(current.Health)
	}
	return domainvirtualization.ConnectionInput{
		ID:                  input.ID,
		Provider:            provider,
		Name:                strings.TrimSpace(input.Name),
		Endpoint:            strings.TrimSpace(input.Endpoint),
		KubernetesClusterID: strings.TrimSpace(input.KubernetesClusterID),
		DefaultNamespace:    strings.TrimSpace(input.DefaultNamespace),
		Enabled:             enabled,
		VerifyTLS:           verifyTLS,
		EncryptedCredential: encrypted,
		Config:              config,
		Health:              health,
	}, nil
}

func (s *Service) adapterFor(provider string) (Adapter, error) {
	adapter := s.adapters[normalizeProvider(provider)]
	if adapter == nil {
		return nil, fmt.Errorf("%w: virtualization adapter %q is not configured", apperrors.ErrInvalidArgument, provider)
	}
	return adapter, nil
}

func (s *Service) adapterConnection(connection domainvirtualization.Connection) (infravirtualization.Connection, error) {
	credential := map[string]any{}
	secretRef := strings.TrimSpace(stringValue(connection.Config, "credentialSecretRef"))
	if s.secretProvider != nil {
		if resolved, err := s.secretProvider.ResolveVirtualizationCredential(context.Background(), connection); err != nil {
			return infravirtualization.Connection{}, fmt.Errorf("%w: resolve virtualization credential: %v", apperrors.ErrInvalidArgument, err)
		} else if len(resolved) > 0 {
			credential = resolved
		}
	} else if secretRef != "" {
		return infravirtualization.Connection{}, fmt.Errorf("%w: credential secret provider is not configured for %s", apperrors.ErrInvalidArgument, secretRef)
	}
	if connection.Provider == ProviderPVE && len(connection.EncryptedCredential) > 0 {
		if strings.TrimSpace(s.credentialKey) == "" {
			return infravirtualization.Connection{}, fmt.Errorf("%w: security.credential_encryption_key is required for PVE credentials", apperrors.ErrInvalidArgument)
		}
		encoded := stringValue(connection.EncryptedCredential, "ciphertext")
		if encoded == "" {
			return infravirtualization.Connection{}, fmt.Errorf("%w: encrypted PVE credential payload is invalid", apperrors.ErrInvalidArgument)
		}
		sealed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return infravirtualization.Connection{}, fmt.Errorf("%w: encrypted PVE credential payload is invalid", apperrors.ErrInvalidArgument)
		}
		decrypted, err := infravirtualization.DecryptCredentialJSON(s.credentialKey, sealed)
		if err != nil {
			return infravirtualization.Connection{}, fmt.Errorf("%w: decrypt PVE credential: %v", apperrors.ErrInvalidArgument, err)
		}
		credential = mergeMaps(credential, decrypted)
	}
	options := cloneMap(connection.Config)
	if options == nil {
		options = map[string]any{}
	}
	if connection.DefaultNamespace != "" {
		options["namespace"] = connection.DefaultNamespace
	}
	return infravirtualization.Connection{
		ID:         connection.ID,
		Name:       connection.Name,
		Provider:   connection.Provider,
		Mode:       stringValue(options, "mode"),
		ClusterID:  connection.KubernetesClusterID,
		Endpoint:   connection.Endpoint,
		Credential: credential,
		Options:    options,
	}, nil
}

func sanitizeConnections(items []domainvirtualization.Connection) []domainvirtualization.Connection {
	out := make([]domainvirtualization.Connection, len(items))
	for i := range items {
		out[i] = sanitizeConnection(items[i])
	}
	return out
}

func sanitizeConnection(item domainvirtualization.Connection) domainvirtualization.Connection {
	item.CredentialConfigured = len(item.EncryptedCredential) > 0
	item.EncryptedCredential = nil
	return item
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func normalizeAction(action string) (infravirtualization.PowerAction, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start":
		return infravirtualization.PowerActionStart, nil
	case "stop", "shutdown":
		return infravirtualization.PowerActionStop, nil
	case "restart", "reboot":
		return infravirtualization.PowerActionRestart, nil
	case "delete":
		return infravirtualization.PowerActionDelete, nil
	default:
		return "", fmt.Errorf("%w: unsupported vm action %q", apperrors.ErrInvalidArgument, action)
	}
}

func mapNotFound(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "no rows") {
		return fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return err
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case float64:
		return fmt.Sprintf("%.0f", value)
	case int:
		return fmt.Sprintf("%d", value)
	case int64:
		return fmt.Sprintf("%d", value)
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func countActiveFlavors(items []domainvirtualization.Flavor) int {
	count := 0
	for _, item := range items {
		if item.Status != "deleted" {
			count++
		}
	}
	return count
}

func topRiskyConnections(items []domainvirtualization.Connection, limit int) []domainvirtualization.Connection {
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, func(left, right domainvirtualization.Connection) int {
		if score := compareInts(connectionRiskScore(left), connectionRiskScore(right)); score != 0 {
			return score
		}
		return strings.Compare(left.Name, right.Name)
	})
	out := make([]domainvirtualization.Connection, 0, min(limit, len(sorted)))
	for _, item := range sorted {
		if connectionRiskScore(item) >= 4 {
			continue
		}
		out = append(out, sanitizeConnection(item))
		if len(out) >= limit {
			break
		}
	}
	return out
}

func topFailedTasks(items []domainvirtualization.Task, limit int, include func(domainvirtualization.Task) bool) []domainvirtualization.Task {
	out := make([]domainvirtualization.Task, 0, limit)
	for _, item := range items {
		if include != nil && !include(item) {
			continue
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func isAbnormalTaskStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case TaskStatusFailed, TaskStatusTimeout:
		return true
	default:
		return false
	}
}

func connectionRiskScore(item domainvirtualization.Connection) int {
	healthStatus := strings.ToLower(stringValue(item.Health, "status"))
	switch healthStatus {
	case "unavailable":
		return 0
	case "degraded":
		return 1
	}
	if item.Enabled && !item.CredentialConfigured {
		return 2
	}
	if item.LastSyncedAt == nil {
		return 3
	}
	return 4
}

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func mergeMaps(base map[string]any, patch map[string]any) map[string]any {
	out := cloneMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range patch {
		out[key] = value
	}
	return out
}

func flavorFromInput(input FlavorInput, id string) domainvirtualization.Flavor {
	cpu := input.CPUCores
	if cpu == 0 {
		cpu = input.CPU
	}
	memory := input.MemoryMB
	if memory == 0 {
		memory = input.MemoryMiB
	}
	disk := input.DiskGB
	if disk == 0 {
		disk = input.DiskGiB
	}
	status := "active"
	if input.Enabled != nil && !*input.Enabled {
		status = "disabled"
	}
	config := cloneMap(input.Config)
	if config == nil {
		config = map[string]any{}
	}
	if input.Description != "" {
		config["description"] = input.Description
	}
	if id == "" {
		id = strings.TrimSpace(input.ID)
	}
	externalID := firstNonEmpty(input.ExternalID, id, input.Name)
	return domainvirtualization.Flavor{
		ID:           id,
		Provider:     firstNonEmpty(normalizeProvider(input.Provider), "manual"),
		ConnectionID: strings.TrimSpace(input.ConnectionID),
		ExternalID:   externalID,
		Name:         strings.TrimSpace(input.Name),
		Status:       status,
		CPUCores:     cpu,
		MemoryMB:     memory,
		DiskGB:       disk,
		Config:       config,
	}
}

func (s *Service) imageFromInput(ctx context.Context, input ImageInput, id string) (domainvirtualization.Image, error) {
	connectionID := strings.TrimSpace(input.ConnectionID)
	if connectionID == "" {
		return domainvirtualization.Image{}, fmt.Errorf("%w: connectionId is required", apperrors.ErrInvalidArgument)
	}
	connection, err := s.repo.GetConnection(ctx, connectionID)
	if err != nil {
		return domainvirtualization.Image{}, mapNotFound(err)
	}
	if id == "" {
		id = strings.TrimSpace(input.ID)
	}
	config := cloneMap(input.Config)
	if config == nil {
		config = map[string]any{}
	}
	if input.Description != "" {
		config["description"] = input.Description
	}
	if input.SourceKind != "" {
		config["sourceKind"] = strings.TrimSpace(input.SourceKind)
	}
	if input.Namespace != "" {
		config["namespace"] = strings.TrimSpace(input.Namespace)
	}
	if input.StorageClass != "" {
		config["storageClass"] = strings.TrimSpace(input.StorageClass)
	}
	if input.URL != "" {
		config["url"] = strings.TrimSpace(input.URL)
	}
	sourceRef := firstNonEmpty(stringValue(config, "sourceRef"), input.ExternalID)
	if sourceRef != "" {
		config["sourceRef"] = sourceRef
	}
	sizeBytes := input.SizeBytes
	if sizeBytes == 0 && input.SizeGiB > 0 {
		sizeBytes = input.SizeGiB * 1024 * 1024 * 1024
	}
	externalID := firstNonEmpty(input.ExternalID, sourceRef, input.Name, id)
	status := firstNonEmpty(input.Status, "active")
	return domainvirtualization.Image{
		ID:           id,
		Provider:     firstNonEmpty(normalizeProvider(input.Provider), connection.Provider),
		ConnectionID: connection.ID,
		ExternalID:   externalID,
		Name:         strings.TrimSpace(input.Name),
		Status:       status,
		OSType:       strings.TrimSpace(input.OSType),
		Architecture: strings.TrimSpace(input.Architecture),
		SizeBytes:    sizeBytes,
		Config:       config,
		Raw:          map[string]any{"managedBy": "kubecrux"},
	}, nil
}

func normalizedPageRequest(page, pageSize, limit int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = limit
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
	}
	return page, pageSize
}

func pageOf[T any](items []T, total, page, pageSize int) domainvirtualization.Page[T] {
	page, pageSize = normalizedPageRequest(page, pageSize, 0)
	return domainvirtualization.Page[T]{Items: items, Total: total, Page: page, PageSize: pageSize}
}

func (s *Service) recordOperation(ctx context.Context, principal domainidentity.Principal, operationType, targetID, targetLabel, result, summary string, metadata map[string]any) {
	if s.operations == nil {
		return
	}
	targetScope := map[string]any{"module": "virtualization"}
	if targetID != "" {
		targetScope["targetId"] = targetID
	}
	if targetLabel != "" {
		targetScope["targetLabel"] = targetLabel
	}
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operationType, targetScope, result, summary, sanitizeMetadata(metadata)))
}

func sanitizeMetadata(metadata map[string]any) map[string]any {
	out := cloneMap(metadata)
	if out == nil {
		out = map[string]any{}
	}
	for _, key := range []string{"credential", "token", "tokenSecret", "ticket", "csrfToken", "password"} {
		delete(out, key)
	}
	return out
}

func payloadString(payload map[string]any, key string) string {
	return stringValue(payload, key)
}

func payloadInt(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func taskTerminal(status string) bool {
	return slices.Contains([]string{TaskStatusSucceeded, TaskStatusFailed, TaskStatusCanceled, TaskStatusTimeout}, status)
}

func isCancelableTaskStatus(status string) bool {
	return slices.Contains([]string{TaskStatusQueued, TaskStatusRunning}, strings.TrimSpace(status))
}

func isRetryableTaskStatus(status string) bool {
	return slices.Contains([]string{TaskStatusFailed, TaskStatusCanceled, TaskStatusTimeout}, strings.TrimSpace(status))
}

func isUnsupported(err error) bool {
	return errors.Is(err, infravirtualization.ErrUnsupported)
}

func newID() string {
	return uuid.NewString()
}
