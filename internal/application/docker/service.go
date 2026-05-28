package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domaindocker "github.com/soha/soha/internal/domain/docker"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/operationentry"
	"sigs.k8s.io/yaml"
)

const (
	OperationKindHostProvision  = "host_provision"
	OperationKindContainerStart = "container_start"
	OperationKindProjectDeploy  = "project_deploy"
	OperationKindProjectAction  = "project_action"
	OperationKindServiceAction  = "service_action"
	OperationKindPortReserve    = "port_reserve"
	OperationKindHostSync       = "host_sync"
	OperationStatusQueued       = "queued"
	OperationStatusRunning      = "running"
	OperationStatusCompleted    = "completed"
	OperationStatusFailed       = "failed"
	OperationStatusCanceled     = "canceled"
	OperationStatusTimeout      = "callback_timeout"
	defaultOperationMaxRetries  = 1
	defaultOperationTimeout     = 1800
)

type Repository = domaindocker.Repository

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type HostProvisionInput struct {
	ConnectionID      string
	Name              string
	CPU               int
	MemoryMiB         int
	DiskGiB           int
	BootImageID       string
	ImageID           string
	FlavorID          string
	Network           string
	StartAfterCreate  bool
	TemplateID        string
	ProviderParams    map[string]any
	ProviderExtraJSON map[string]any
}

type HostProvisionTask struct {
	ID           string
	Provider     string
	ConnectionID string
	Status       string
}

type HostProvisioner interface {
	ProvisionDockerHost(context.Context, domainidentity.Principal, HostProvisionInput) (HostProvisionTask, error)
}

type Option func(*Service)

func WithHostProvisioner(provisioner HostProvisioner) Option {
	return func(s *Service) {
		s.hostProvisioner = provisioner
	}
}

type Service struct {
	repo            Repository
	permissions     *appaccess.PermissionResolver
	operations      OperationRecorder
	hostProvisioner HostProvisioner
}

func New(repo Repository, permissions *appaccess.PermissionResolver, operations OperationRecorder, opts ...Option) *Service {
	service := &Service{repo: repo, permissions: permissions, operations: operations}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func (s *Service) Overview(ctx context.Context, principal domainidentity.Principal) (domaindocker.Overview, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOverviewView); err != nil {
		return domaindocker.Overview{}, err
	}
	hosts, err := s.repo.ListHosts(ctx, domaindocker.HostFilter{Limit: 500})
	if err != nil {
		return domaindocker.Overview{}, err
	}
	projects, err := s.repo.ListProjects(ctx, domaindocker.ProjectFilter{Limit: 500})
	if err != nil {
		return domaindocker.Overview{}, err
	}
	services, err := s.repo.ListServices(ctx, domaindocker.ServiceFilter{Limit: 500})
	if err != nil {
		return domaindocker.Overview{}, err
	}
	ports, err := s.repo.ListPortMappings(ctx, domaindocker.PortMappingFilter{Limit: 500})
	if err != nil {
		return domaindocker.Overview{}, err
	}
	recent, err := s.repo.ListOperations(ctx, domaindocker.OperationFilter{Limit: 10})
	if err != nil {
		return domaindocker.Overview{}, err
	}
	now := time.Now().UTC()
	expiring := make([]domaindocker.Project, 0)
	for _, project := range projects {
		if project.ExpiresAt != nil && project.ExpiresAt.After(now) && project.ExpiresAt.Before(now.Add(72*time.Hour)) {
			expiring = append(expiring, project)
		}
	}
	if len(expiring) > 8 {
		expiring = expiring[:8]
	}
	overview := domaindocker.Overview{
		HostSummary:      summarizeHosts(hosts),
		ProjectSummary:   summarizeProjects(projects),
		ServiceSummary:   summarizeServices(services),
		PortSummary:      summarizePorts(ports),
		RecentOperations: recent,
		ExpiringProjects: expiring,
	}
	pending, _ := s.repo.CountOperations(ctx, domaindocker.OperationFilter{Pending: true})
	failed, _ := s.repo.CountOperations(ctx, domaindocker.OperationFilter{Abnormal: true})
	overview.Stats = domaindocker.OverviewStats{
		HostCount:           overview.HostSummary.Total,
		OnlineHostCount:     overview.HostSummary.Online,
		ProjectCount:        overview.ProjectSummary.Total,
		RunningProjectCount: overview.ProjectSummary.Running,
		ServiceCount:        overview.ServiceSummary.Total,
		RunningServiceCount: overview.ServiceSummary.Running,
		PortMappingCount:    overview.PortSummary.Total,
		PendingTaskCount:    pending,
		FailedTaskCount:     failed,
	}
	return overview, nil
}

func (s *Service) ListHosts(ctx context.Context, principal domainidentity.Principal, filter domaindocker.HostFilter) (domaindocker.Page[domaindocker.Host], error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsView); err != nil {
		return domaindocker.Page[domaindocker.Host]{}, err
	}
	items, err := s.repo.ListHosts(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Host]{}, err
	}
	total, err := s.repo.CountHosts(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Host]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) GetHost(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Host, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsView); err != nil {
		return domaindocker.Host{}, err
	}
	return s.repo.GetHost(ctx, id)
}

func (s *Service) CreateHost(ctx context.Context, principal domainidentity.Principal, input domaindocker.HostInput) (domaindocker.Host, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsManage); err != nil {
		return domaindocker.Host{}, err
	}
	if err := validateHostInput(input); err != nil {
		return domaindocker.Host{}, err
	}
	item, err := s.repo.CreateHost(ctx, input)
	if err != nil {
		return domaindocker.Host{}, err
	}
	s.recordOperation(ctx, principal, "docker.host.create", item.ID, item.Name, "success", "created docker host", nil)
	return item, nil
}

func (s *Service) UpdateHost(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.HostInput) (domaindocker.Host, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsManage); err != nil {
		return domaindocker.Host{}, err
	}
	if err := validateHostInput(input); err != nil {
		return domaindocker.Host{}, err
	}
	item, err := s.repo.UpdateHost(ctx, id, input)
	if err != nil {
		return domaindocker.Host{}, err
	}
	s.recordOperation(ctx, principal, "docker.host.update", item.ID, item.Name, "success", "updated docker host", nil)
	return item, nil
}

func (s *Service) DeleteHost(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsManage); err != nil {
		return err
	}
	current, _ := s.repo.GetHost(ctx, id)
	if err := s.repo.DeleteHost(ctx, id); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "docker.host.delete", id, firstNonEmpty(current.Name, id), "success", "deleted docker host", nil)
	return nil
}

func (s *Service) QuickCreateHost(ctx context.Context, principal domainidentity.Principal, input domaindocker.QuickCreateHostInput) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsManage); err != nil {
		return domaindocker.Operation{}, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return domaindocker.Operation{}, fmt.Errorf("%w: docker host name is required", apperrors.ErrInvalidArgument)
	}
	var vmTask *HostProvisionTask
	if s.hostProvisioner != nil && strings.TrimSpace(input.VirtualizationConnectionID) != "" {
		task, err := s.hostProvisioner.ProvisionDockerHost(ctx, principal, HostProvisionInput{
			ConnectionID:     strings.TrimSpace(input.VirtualizationConnectionID),
			Name:             strings.TrimSpace(input.Name),
			CPU:              input.CPUCoreCount,
			MemoryMiB:        bytesToMiB(input.MemoryBytes),
			DiskGiB:          bytesToGiB(input.DiskBytes),
			BootImageID:      strings.TrimSpace(input.ImageID),
			ImageID:          strings.TrimSpace(input.ImageID),
			FlavorID:         strings.TrimSpace(input.FlavorID),
			Network:          strings.TrimSpace(input.Network),
			StartAfterCreate: true,
			TemplateID:       strings.TrimSpace(input.VMTemplateID),
			ProviderParams:   mapValueAny(input.Config["providerParams"]),
			ProviderExtraJSON: mapValueAny(firstNonNil(
				input.Config["providerExtra"],
				input.Config["providerExtraJSON"],
			)),
		})
		if err != nil {
			return domaindocker.Operation{}, err
		}
		vmTask = &task
	}
	hostConfig := mergeMap(input.Config, map[string]any{})
	if vmTask != nil {
		hostConfig = mergeMap(hostConfig, map[string]any{
			"virtualizationTaskId":     vmTask.ID,
			"virtualizationTaskStatus": vmTask.Status,
			"virtualizationProvider":   vmTask.Provider,
		})
	}
	host, err := s.repo.CreateHost(ctx, domaindocker.HostInput{
		Name:                       input.Name,
		Status:                     "provisioning",
		Environment:                input.Environment,
		Owner:                      firstNonEmpty(input.Owner, principal.UserName, principal.UserID),
		Team:                       input.Team,
		VirtualizationConnectionID: input.VirtualizationConnectionID,
		CPUCoreCount:               input.CPUCoreCount,
		MemoryBytes:                input.MemoryBytes,
		DiskBytes:                  input.DiskBytes,
		AvailablePortStart:         input.AvailablePortStart,
		AvailablePortEnd:           input.AvailablePortEnd,
		Labels:                     input.Labels,
		Config:                     hostConfig,
	})
	if err != nil {
		return domaindocker.Operation{}, err
	}
	payload := map[string]any{
		"hostId":                     host.ID,
		"hostName":                   host.Name,
		"virtualizationConnectionId": strings.TrimSpace(input.VirtualizationConnectionID),
		"vmTemplateId":               strings.TrimSpace(input.VMTemplateID),
		"flavorId":                   strings.TrimSpace(input.FlavorID),
		"imageId":                    strings.TrimSpace(input.ImageID),
		"network":                    strings.TrimSpace(input.Network),
		"ttlSeconds":                 input.TTLSeconds,
	}
	if vmTask != nil {
		payload["virtualizationTaskId"] = vmTask.ID
		payload["virtualizationTaskStatus"] = vmTask.Status
		payload["virtualizationProvider"] = vmTask.Provider
	}
	task, err := s.enqueueOperation(ctx, principal, OperationKindHostProvision, host.ID, "", "", payload)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	if vmTask != nil {
		now := time.Now().UTC()
		task.Status = OperationStatusRunning
		task.StartedAt = &now
		task.LastHeartbeatAt = &now
		task.Result = mergeMap(task.Result, map[string]any{
			"virtualizationTaskId":     vmTask.ID,
			"virtualizationTaskStatus": vmTask.Status,
			"message":                  "virtualization VM creation task enqueued",
		})
		if updated, updateErr := s.repo.UpdateOperation(ctx, task); updateErr == nil {
			task = updated
		}
		_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{
			ID:          uuid.NewString(),
			OperationID: task.ID,
			LogLevel:    "info",
			Message:     "virtualization VM creation task enqueued",
			Payload:     map[string]any{"virtualizationTaskId": vmTask.ID, "provider": vmTask.Provider},
		})
	}
	s.recordOperation(ctx, principal, "docker.host.provision.enqueue", host.ID, host.Name, "success", "enqueued docker host provisioning", map[string]any{"operationId": task.ID})
	return task, nil
}

func (s *Service) ListProjects(ctx context.Context, principal domainidentity.Principal, filter domaindocker.ProjectFilter) (domaindocker.Page[domaindocker.Project], error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsView); err != nil {
		return domaindocker.Page[domaindocker.Project]{}, err
	}
	items, err := s.repo.ListProjects(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Project]{}, err
	}
	total, err := s.repo.CountProjects(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Project]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) GetProject(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Project, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsView); err != nil {
		return domaindocker.Project{}, err
	}
	return s.repo.GetProject(ctx, id)
}

func (s *Service) CreateProject(ctx context.Context, principal domainidentity.Principal, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsManage); err != nil {
		return domaindocker.Project{}, err
	}
	if err := validateProjectInput(input); err != nil {
		return domaindocker.Project{}, err
	}
	if _, err := s.repo.GetHost(ctx, input.HostID); err != nil {
		return domaindocker.Project{}, fmt.Errorf("%w: docker host %s", apperrors.ErrNotFound, strings.TrimSpace(input.HostID))
	}
	item, err := s.repo.CreateProject(ctx, input)
	if err != nil {
		return domaindocker.Project{}, err
	}
	if err := s.upsertServicesFromCompose(ctx, item); err != nil {
		return domaindocker.Project{}, err
	}
	s.recordOperation(ctx, principal, "docker.project.create", item.ID, item.Name, "success", "created docker compose project", nil)
	return item, nil
}

func (s *Service) UpdateProject(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsManage); err != nil {
		return domaindocker.Project{}, err
	}
	if err := validateProjectInput(input); err != nil {
		return domaindocker.Project{}, err
	}
	item, err := s.repo.UpdateProject(ctx, id, input)
	if err != nil {
		return domaindocker.Project{}, err
	}
	if err := s.upsertServicesFromCompose(ctx, item); err != nil {
		return domaindocker.Project{}, err
	}
	s.recordOperation(ctx, principal, "docker.project.update", item.ID, item.Name, "success", "updated docker compose project", nil)
	return item, nil
}

func (s *Service) DeleteProject(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsManage); err != nil {
		return err
	}
	current, _ := s.repo.GetProject(ctx, id)
	if err := s.repo.DeleteProject(ctx, id); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "docker.project.delete", id, firstNonEmpty(current.Name, id), "success", "deleted docker compose project", nil)
	return nil
}

func (s *Service) DeployProject(ctx context.Context, principal domainidentity.Principal, id string, action string) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsDeploy); err != nil {
		return domaindocker.Operation{}, err
	}
	project, err := s.repo.GetProject(ctx, id)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	normalizedAction := strings.TrimSpace(action)
	if normalizedAction == "" {
		normalizedAction = "deploy"
	}
	if !slices.Contains([]string{"deploy", "redeploy", "start", "stop", "restart", "down", "pull", "build", "destroy"}, normalizedAction) {
		return domaindocker.Operation{}, fmt.Errorf("%w: unsupported compose action %s", apperrors.ErrInvalidArgument, normalizedAction)
	}
	task, err := s.enqueueOperation(ctx, principal, OperationKindProjectDeploy, project.HostID, project.ID, "", map[string]any{
		"action":         normalizedAction,
		"composeContent": project.ComposeContent,
		"envContent":     project.EnvContent,
		"projectSlug":    project.Slug,
	})
	if err != nil {
		return domaindocker.Operation{}, err
	}
	s.recordOperation(ctx, principal, "docker.project.deploy.enqueue", project.ID, project.Name, "success", "enqueued docker compose action", map[string]any{"operationId": task.ID, "action": normalizedAction})
	return task, nil
}

func (s *Service) StartContainer(ctx context.Context, principal domainidentity.Principal, input domaindocker.ContainerStartInput) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsManage); err != nil {
		return domaindocker.Operation{}, err
	}
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsDeploy); err != nil {
		return domaindocker.Operation{}, err
	}
	if err := s.authorize(ctx, principal, appaccess.PermDockerPortsManage); err != nil {
		return domaindocker.Operation{}, err
	}
	if err := validateContainerStartInput(input); err != nil {
		return domaindocker.Operation{}, err
	}
	host, err := s.repo.GetHost(ctx, input.HostID)
	if err != nil {
		return domaindocker.Operation{}, fmt.Errorf("%w: docker host %s", apperrors.ErrNotFound, strings.TrimSpace(input.HostID))
	}
	serviceName := NormalizeSlug(input.Name)
	input.Protocol = normalizedProtocol(input.Protocol)
	input.ExposureScope = firstNonEmpty(strings.TrimSpace(input.ExposureScope), "internal")
	input.DomainName = strings.ToLower(strings.TrimSpace(input.DomainName))
	if input.DomainName != "" {
		input.DomainScheme = normalizedDomainScheme(input.DomainScheme, input.DomainTLSEnabled)
	} else {
		input.DomainScheme = ""
		input.DomainTLSEnabled = false
	}
	input.RestartPolicy = normalizedRestartPolicy(input.RestartPolicy)
	input.Owner = firstNonEmpty(input.Owner, principal.UserName, principal.UserID)
	if input.HostIP == "" {
		input.HostIP = "0.0.0.0"
	}
	portMappingID := uuid.NewString()

	composeContent, err := singleContainerComposeContent(serviceName, input)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	portInput := domaindocker.PortMappingInput{
		HostID:           input.HostID,
		Name:             input.Name,
		HostIP:           input.HostIP,
		HostPort:         input.HostPort,
		ContainerPort:    input.ContainerPort,
		Protocol:         input.Protocol,
		ID:               portMappingID,
		ExposureScope:    input.ExposureScope,
		Status:           "active",
		DomainName:       input.DomainName,
		DomainScheme:     input.DomainScheme,
		DomainTLSEnabled: input.DomainTLSEnabled,
		AccessURL:        singleContainerAccessURL(input, host),
		Owner:            input.Owner,
		Config: mergeMap(input.Config, map[string]any{
			"sourceKind":    "single_container",
			"containerName": serviceName,
			"image":         input.Image,
		}),
	}
	if err := s.validatePortMapping(ctx, portInput, ""); err != nil {
		return domaindocker.Operation{}, err
	}
	projectInput := domaindocker.ProjectInput{
		HostID:         input.HostID,
		Name:           input.Name,
		Slug:           serviceName,
		Description:    "Single container: " + strings.TrimSpace(input.Image),
		Environment:    input.Environment,
		Owner:          input.Owner,
		Team:           input.Team,
		SourceKind:     "single_container",
		ComposeContent: composeContent,
		EnvContent:     input.EnvContent,
		Status:         "defined",
		DesiredState:   "running",
		TTLSeconds:     input.TTLSeconds,
		Labels:         input.Labels,
		Config: mergeMap(input.Config, map[string]any{
			"sourceKind":       "single_container",
			"serviceName":      serviceName,
			"image":            input.Image,
			"containerPort":    input.ContainerPort,
			"hostPort":         input.HostPort,
			"protocol":         input.Protocol,
			"domainName":       input.DomainName,
			"domainScheme":     input.DomainScheme,
			"domainTlsEnabled": input.DomainTLSEnabled,
		}),
	}
	serviceInput := domaindocker.ServiceInput{
		HostID: input.HostID,
		Name:   serviceName,
		Image:  input.Image,
		Status: "defined",
		Config: map[string]any{
			"sourceKind":    "single_container",
			"containerPort": input.ContainerPort,
			"hostPort":      input.HostPort,
			"protocol":      input.Protocol,
			"domainName":    input.DomainName,
		},
	}
	requestedBy := firstNonEmpty(principal.UserID, principal.UserName)
	operationPayload := map[string]any{
		"action":         "deploy",
		"sourceKind":     "single_container",
		"projectSlug":    projectInput.Slug,
		"serviceName":    serviceInput.Name,
		"composeContent": projectInput.ComposeContent,
		"envContent":     projectInput.EnvContent,
		"image":          input.Image,
		"portMappingId":  portMappingID,
		"accessUrl":      portInput.AccessURL,
		"domainName":     portInput.DomainName,
	}
	created, err := s.repo.CreateContainerStart(ctx, domaindocker.ContainerStartCreateInput{
		Project:     projectInput,
		Service:     serviceInput,
		PortMapping: portInput,
		Operation: domaindocker.OperationInput{
			HostID:         input.HostID,
			OperationKind:  OperationKindContainerStart,
			Status:         OperationStatusQueued,
			RequestedBy:    requestedBy,
			MaxRetries:     defaultOperationMaxRetries,
			TimeoutSeconds: defaultOperationTimeout,
			Payload:        operationPayload,
		},
	})
	if err != nil {
		return domaindocker.Operation{}, err
	}
	_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{
		ID:          uuid.NewString(),
		OperationID: created.Operation.ID,
		LogLevel:    "info",
		Message:     "operation queued by control plane",
		Payload:     map[string]any{"kind": OperationKindContainerStart},
		CreatedAt:   time.Now().UTC(),
	})
	s.recordOperation(ctx, principal, "docker.container.start.enqueue", created.Project.ID, created.Project.Name, "success", "enqueued single container start", map[string]any{"operationId": created.Operation.ID, "serviceId": created.Service.ID, "portMappingId": created.PortMapping.ID, "domainName": created.PortMapping.DomainName})
	return created.Operation, nil
}

func (s *Service) ListServices(ctx context.Context, principal domainidentity.Principal, filter domaindocker.ServiceFilter) (domaindocker.Page[domaindocker.Service], error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesView); err != nil {
		return domaindocker.Page[domaindocker.Service]{}, err
	}
	items, err := s.repo.ListServices(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Service]{}, err
	}
	total, err := s.repo.CountServices(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Service]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) ServiceAction(ctx context.Context, principal domainidentity.Principal, id string, action string) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerServicesManage); err != nil {
		return domaindocker.Operation{}, err
	}
	service, err := s.repo.GetService(ctx, id)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	normalizedAction := strings.TrimSpace(action)
	if !slices.Contains([]string{"restart", "start", "stop", "logs"}, normalizedAction) {
		return domaindocker.Operation{}, fmt.Errorf("%w: unsupported service action %s", apperrors.ErrInvalidArgument, normalizedAction)
	}
	payload := map[string]any{"action": normalizedAction, "serviceName": service.Name}
	if project, projectErr := s.repo.GetProject(ctx, service.ProjectID); projectErr == nil {
		payload["composeContent"] = project.ComposeContent
		payload["envContent"] = project.EnvContent
		payload["projectSlug"] = project.Slug
	}
	task, err := s.enqueueOperation(ctx, principal, OperationKindServiceAction, service.HostID, service.ProjectID, service.ID, payload)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	s.recordOperation(ctx, principal, "docker.service.action.enqueue", service.ID, service.Name, "success", "enqueued docker service action", map[string]any{"operationId": task.ID, "action": normalizedAction})
	return task, nil
}

func (s *Service) ListPortMappings(ctx context.Context, principal domainidentity.Principal, filter domaindocker.PortMappingFilter) (domaindocker.Page[domaindocker.PortMapping], error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerPortsView); err != nil {
		return domaindocker.Page[domaindocker.PortMapping]{}, err
	}
	items, err := s.repo.ListPortMappings(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.PortMapping]{}, err
	}
	total, err := s.repo.CountPortMappings(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.PortMapping]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) CreatePortMapping(ctx context.Context, principal domainidentity.Principal, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerPortsManage); err != nil {
		return domaindocker.PortMapping{}, err
	}
	input = normalizePortMappingInput(input)
	if err := s.validatePortMapping(ctx, input, ""); err != nil {
		return domaindocker.PortMapping{}, err
	}
	item, err := s.repo.CreatePortMapping(ctx, input)
	if err != nil {
		return domaindocker.PortMapping{}, err
	}
	_, _ = s.enqueueOperation(ctx, principal, OperationKindPortReserve, item.HostID, item.ProjectID, item.ServiceID, map[string]any{
		"portMappingId": item.ID,
		"hostPort":      item.HostPort,
		"containerPort": item.ContainerPort,
		"protocol":      item.Protocol,
	})
	s.recordOperation(ctx, principal, "docker.port.create", item.ID, item.Name, "success", "reserved docker port mapping", map[string]any{"hostPort": item.HostPort, "protocol": item.Protocol})
	return item, nil
}

func (s *Service) UpdatePortMapping(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerPortsManage); err != nil {
		return domaindocker.PortMapping{}, err
	}
	input = normalizePortMappingInput(input)
	if err := s.validatePortMapping(ctx, input, id); err != nil {
		return domaindocker.PortMapping{}, err
	}
	item, err := s.repo.UpdatePortMapping(ctx, id, input)
	if err != nil {
		return domaindocker.PortMapping{}, err
	}
	s.recordOperation(ctx, principal, "docker.port.update", item.ID, item.Name, "success", "updated docker port mapping", map[string]any{"hostPort": item.HostPort, "protocol": item.Protocol})
	return item, nil
}

func (s *Service) DeletePortMapping(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerPortsManage); err != nil {
		return err
	}
	current, _ := s.repo.GetPortMapping(ctx, id)
	if err := s.repo.DeletePortMapping(ctx, id); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "docker.port.delete", id, firstNonEmpty(current.Name, id), "success", "released docker port mapping", map[string]any{"hostPort": current.HostPort, "protocol": current.Protocol})
	return nil
}

func (s *Service) ListTemplates(ctx context.Context, principal domainidentity.Principal, filter domaindocker.TemplateFilter) (domaindocker.Page[domaindocker.Template], error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerTemplatesView); err != nil {
		return domaindocker.Page[domaindocker.Template]{}, err
	}
	items, err := s.repo.ListTemplates(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Template]{}, err
	}
	total, err := s.repo.CountTemplates(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Template]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) CreateTemplate(ctx context.Context, principal domainidentity.Principal, input domaindocker.TemplateInput) (domaindocker.Template, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerTemplatesManage); err != nil {
		return domaindocker.Template{}, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return domaindocker.Template{}, fmt.Errorf("%w: template name is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.CreateTemplate(ctx, input)
	if err != nil {
		return domaindocker.Template{}, err
	}
	s.recordOperation(ctx, principal, "docker.template.create", item.ID, item.Name, "success", "created docker template", nil)
	return item, nil
}

func (s *Service) UpdateTemplate(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.TemplateInput) (domaindocker.Template, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerTemplatesManage); err != nil {
		return domaindocker.Template{}, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return domaindocker.Template{}, fmt.Errorf("%w: template name is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateTemplate(ctx, id, input)
	if err != nil {
		return domaindocker.Template{}, err
	}
	s.recordOperation(ctx, principal, "docker.template.update", item.ID, item.Name, "success", "updated docker template", nil)
	return item, nil
}

func (s *Service) DeleteTemplate(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDockerTemplatesManage); err != nil {
		return err
	}
	current, _ := s.repo.GetTemplate(ctx, id)
	if err := s.repo.DeleteTemplate(ctx, id); err != nil {
		return err
	}
	s.recordOperation(ctx, principal, "docker.template.delete", id, firstNonEmpty(current.Name, id), "success", "deleted docker template", nil)
	return nil
}

func (s *Service) ListOperations(ctx context.Context, principal domainidentity.Principal, filter domaindocker.OperationFilter) (domaindocker.Page[domaindocker.Operation], error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsView); err != nil {
		return domaindocker.Page[domaindocker.Operation]{}, err
	}
	items, err := s.repo.ListOperations(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Operation]{}, err
	}
	total, err := s.repo.CountOperations(ctx, filter)
	if err != nil {
		return domaindocker.Page[domaindocker.Operation]{}, err
	}
	return pageOf(items, total, filter.Page, filter.PageSize), nil
}

func (s *Service) GetOperation(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsView); err != nil {
		return domaindocker.Operation{}, err
	}
	return s.repo.GetOperation(ctx, id)
}

func (s *Service) ListOperationLogs(ctx context.Context, principal domainidentity.Principal, id string, limit int) ([]domaindocker.OperationLog, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsView); err != nil {
		return nil, err
	}
	return s.repo.ListOperationLogs(ctx, id, limit)
}

func (s *Service) CancelOperation(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsManage); err != nil {
		return domaindocker.Operation{}, err
	}
	item, err := s.repo.GetOperation(ctx, id)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	if !slices.Contains([]string{OperationStatusQueued, OperationStatusRunning}, item.Status) {
		return domaindocker.Operation{}, fmt.Errorf("%w: operation is not cancelable", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	item.Status = OperationStatusCanceled
	item.FinishedAt = &now
	item.Result = mergeMap(item.Result, map[string]any{"canceledBy": principal.UserID})
	updated, err := s.repo.UpdateOperation(ctx, item)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{ID: uuid.NewString(), OperationID: updated.ID, LogLevel: "warn", Message: "operation canceled by control plane", Payload: map[string]any{"userId": principal.UserID}})
	s.recordOperation(ctx, principal, "docker.operation.cancel", updated.ID, updated.OperationKind, "success", "canceled docker operation", map[string]any{"operationId": updated.ID})
	return updated, nil
}

func (s *Service) RetryOperation(ctx context.Context, principal domainidentity.Principal, id string) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsManage); err != nil {
		return domaindocker.Operation{}, err
	}
	item, err := s.repo.GetOperation(ctx, id)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	if !slices.Contains([]string{OperationStatusFailed, OperationStatusTimeout, OperationStatusCanceled}, item.Status) {
		return domaindocker.Operation{}, fmt.Errorf("%w: operation is not retryable", apperrors.ErrInvalidArgument)
	}
	item.Status = OperationStatusQueued
	item.AttemptCount++
	item.ClaimedByWorkerID = ""
	item.StartedAt = nil
	item.LastHeartbeatAt = nil
	item.FinishedAt = nil
	item.Result = mergeMap(item.Result, map[string]any{"retriedBy": principal.UserID})
	updated, err := s.repo.UpdateOperation(ctx, item)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{ID: uuid.NewString(), OperationID: updated.ID, LogLevel: "info", Message: "operation retry queued", Payload: map[string]any{"userId": principal.UserID}})
	s.recordOperation(ctx, principal, "docker.operation.retry", updated.ID, updated.OperationKind, "success", "queued docker operation retry", map[string]any{"operationId": updated.ID})
	return updated, nil
}

func (s *Service) ClaimOperation(ctx context.Context, input domaindocker.OperationClaimInput) (domaindocker.Operation, error) {
	workerID := firstNonEmpty(input.WorkerID, input.AgentID)
	if workerID == "" {
		return domaindocker.Operation{}, fmt.Errorf("%w: docker worker id is required", apperrors.ErrInvalidArgument)
	}
	kinds := input.OperationKinds
	if len(kinds) == 0 {
		kinds = []string{OperationKindContainerStart, OperationKindProjectDeploy, OperationKindServiceAction, OperationKindPortReserve, OperationKindHostSync}
	}
	item, err := s.repo.ClaimOperation(ctx, workerID, input.AgentID, input.HostIDs, kinds, time.Now().UTC())
	if err != nil {
		return domaindocker.Operation{}, err
	}
	_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{
		ID:          uuid.NewString(),
		OperationID: item.ID,
		LogLevel:    "info",
		Message:     "operation claimed by docker worker",
		Payload:     map[string]any{"workerId": workerID, "agentId": input.AgentID},
	})
	if item.HostID != "" {
		_, _ = s.repo.TouchHostRuntime(ctx, item.HostID, domaindocker.HostInput{
			Status:  "online",
			AgentID: input.AgentID,
			Config:  map[string]any{"lastClaimedOperationId": item.ID, "lastClaimedAt": time.Now().UTC().Format(time.RFC3339)},
		})
	}
	return item, nil
}

func (s *Service) GetOperationForRunner(ctx context.Context, id string) (domaindocker.Operation, error) {
	return s.repo.GetOperation(ctx, id)
}

func (s *Service) RecordOperationCallback(ctx context.Context, input domaindocker.OperationCallbackInput) (domaindocker.Operation, error) {
	item, err := s.repo.GetOperation(ctx, input.OperationID)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	workerID := strings.TrimSpace(input.WorkerID)
	if workerID == "" {
		return domaindocker.Operation{}, fmt.Errorf("%w: docker worker id is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(item.ClaimedByWorkerID) != "" && strings.TrimSpace(item.ClaimedByWorkerID) != workerID {
		return domaindocker.Operation{}, fmt.Errorf("%w: docker operation is claimed by another worker", apperrors.ErrAccessDenied)
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = OperationStatusRunning
	}
	if !validCallbackStatus(status) {
		return domaindocker.Operation{}, fmt.Errorf("%w: unsupported docker callback status %s", apperrors.ErrInvalidArgument, status)
	}
	if operationTerminal(item.Status) {
		return item, nil
	}
	now := time.Now().UTC()
	item.ClaimedByWorkerID = workerID
	item.LastHeartbeatAt = &now
	item.Result = mergeMap(item.Result, sanitizeMetadata(input.Payload))
	if status == OperationStatusRunning {
		item.Status = OperationStatusRunning
	} else {
		item.Status = status
		item.FinishedAt = &now
	}
	updated, err := s.repo.UpdateOperation(ctx, item)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	s.appendCallbackLogs(ctx, updated.ID, status, input)
	if updated.HostID != "" {
		s.touchHostFromCallback(ctx, updated.HostID, workerID, status, input.Payload)
	}
	s.applyCallbackRuntimeState(ctx, updated, status, input.Payload)
	return updated, nil
}

func (s *Service) enqueueOperation(ctx context.Context, principal domainidentity.Principal, kind, hostID, projectID, serviceID string, payload map[string]any) (domaindocker.Operation, error) {
	task, err := s.repo.CreateOperation(ctx, domaindocker.OperationInput{
		HostID:         hostID,
		ProjectID:      projectID,
		ServiceID:      serviceID,
		OperationKind:  kind,
		Status:         OperationStatusQueued,
		RequestedBy:    firstNonEmpty(principal.UserID, principal.UserName),
		MaxRetries:     defaultOperationMaxRetries,
		TimeoutSeconds: defaultOperationTimeout,
		Payload:        payload,
	})
	if err != nil {
		return domaindocker.Operation{}, err
	}
	_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{
		ID:          uuid.NewString(),
		OperationID: task.ID,
		LogLevel:    "info",
		Message:     "operation queued by control plane",
		Payload:     map[string]any{"kind": kind},
		CreatedAt:   time.Now().UTC(),
	})
	return task, nil
}

func (s *Service) appendCallbackLogs(ctx context.Context, operationID, status string, input domaindocker.OperationCallbackInput) {
	logLevel := "info"
	if status == OperationStatusFailed || status == OperationStatusTimeout {
		logLevel = "error"
	} else if status == OperationStatusCanceled {
		logLevel = "warn"
	}
	if len(input.Logs) == 0 {
		if logsRaw, ok := input.Payload["logs"]; ok {
			input.Logs = stringSlice(logsRaw)
		}
	}
	for _, line := range input.Logs {
		message := strings.TrimSpace(line)
		if message == "" {
			continue
		}
		_ = s.repo.CreateOperationLog(ctx, domaindocker.OperationLog{
			ID:          uuid.NewString(),
			OperationID: operationID,
			LogLevel:    logLevel,
			Message:     message,
			Payload:     map[string]any{"workerId": input.WorkerID, "callbackStatus": status},
		})
	}
}

func (s *Service) touchHostFromCallback(ctx context.Context, hostID, workerID, status string, payload map[string]any) {
	hostStatus := "online"
	if status == OperationStatusFailed || status == OperationStatusTimeout {
		hostStatus = "degraded"
	}
	_, _ = s.repo.TouchHostRuntime(ctx, hostID, domaindocker.HostInput{
		Status:         hostStatus,
		AgentID:        workerID,
		AgentVersion:   stringValue(payload, "agentVersion"),
		DockerVersion:  stringValue(payload, "dockerVersion"),
		ComposeVersion: stringValue(payload, "composeVersion"),
		Endpoint:       stringValue(payload, "endpoint"),
		IPAddress:      stringValue(payload, "ipAddress"),
		Config: map[string]any{
			"lastDockerOperationStatus": status,
			"lastDockerOperationAt":     time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (s *Service) applyCallbackRuntimeState(ctx context.Context, item domaindocker.Operation, status string, payload map[string]any) {
	if status == OperationStatusRunning {
		return
	}
	if item.ProjectID != "" {
		nextStatus, desired := projectStatusForOperation(item, status)
		var deployedAt *time.Time
		if status == OperationStatusCompleted && (item.OperationKind == OperationKindProjectDeploy || item.OperationKind == OperationKindContainerStart) {
			now := time.Now().UTC()
			deployedAt = &now
		}
		if nextStatus != "" || desired != "" || deployedAt != nil {
			_, _ = s.repo.UpdateProjectRuntime(ctx, item.ProjectID, nextStatus, desired, deployedAt)
		}
	}
	if item.ServiceID != "" && status == OperationStatusCompleted {
		if current, err := s.repo.GetService(ctx, item.ServiceID); err == nil {
			nextStatus := serviceStatusForAction(stringValue(item.Payload, "action"))
			if nextStatus == "" {
				nextStatus = current.Status
			}
			current.Status = nextStatus
			current.Config = mergeMap(current.Config, map[string]any{"lastAction": stringValue(item.Payload, "action")})
			_, _ = s.repo.UpsertService(ctx, domaindocker.ServiceInput{
				ID:             current.ID,
				ProjectID:      current.ProjectID,
				HostID:         current.HostID,
				Name:           current.Name,
				Image:          current.Image,
				Status:         current.Status,
				ContainerID:    current.ContainerID,
				RestartCount:   current.RestartCount,
				CPUPercent:     current.CPUPercent,
				MemoryBytes:    current.MemoryBytes,
				NetworkRxBytes: current.NetworkRxBytes,
				NetworkTxBytes: current.NetworkTxBytes,
				Config:         current.Config,
			})
		}
	}
	if services := serviceInputsFromPayload(item, payload); len(services) > 0 {
		for _, service := range services {
			_, _ = s.repo.UpsertService(ctx, service)
		}
	}
}

func (s *Service) upsertServicesFromCompose(ctx context.Context, project domaindocker.Project) error {
	names := composeServiceNames(project.ComposeContent)
	for _, name := range names {
		_, err := s.repo.UpsertService(ctx, domaindocker.ServiceInput{
			ProjectID: project.ID,
			HostID:    project.HostID,
			Name:      name,
			Status:    "defined",
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validatePortMapping(ctx context.Context, input domaindocker.PortMappingInput, excludeID string) error {
	if strings.TrimSpace(input.HostID) == "" {
		return fmt.Errorf("%w: hostId is required", apperrors.ErrInvalidArgument)
	}
	if input.HostPort <= 0 || input.HostPort > 65535 {
		return fmt.Errorf("%w: hostPort must be between 1 and 65535", apperrors.ErrInvalidArgument)
	}
	if input.ContainerPort <= 0 || input.ContainerPort > 65535 {
		return fmt.Errorf("%w: containerPort must be between 1 and 65535", apperrors.ErrInvalidArgument)
	}
	protocol := strings.ToLower(strings.TrimSpace(input.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" && protocol != "udp" {
		return fmt.Errorf("%w: protocol must be tcp or udp", apperrors.ErrInvalidArgument)
	}
	if err := validateDomainAccess(input.DomainName, input.DomainScheme); err != nil {
		return err
	}
	existing, err := s.repo.ListPortMappings(ctx, domaindocker.PortMappingFilter{
		HostID:    input.HostID,
		HostIP:    strings.TrimSpace(input.HostIP),
		HostPort:  input.HostPort,
		Protocol:  protocol,
		ExcludeID: excludeID,
		Limit:     20,
	})
	if err != nil {
		return err
	}
	for _, item := range existing {
		if item.Status != "released" {
			return fmt.Errorf("%w: host port %d/%s is already reserved", apperrors.ErrInvalidArgument, input.HostPort, protocol)
		}
	}
	if domain := strings.TrimSpace(input.DomainName); domain != "" {
		matches, err := s.repo.ListPortMappings(ctx, domaindocker.PortMappingFilter{
			DomainName: strings.ToLower(domain),
			ExcludeID:  excludeID,
			Limit:      20,
		})
		if err != nil {
			return err
		}
		for _, item := range matches {
			if item.Status != "released" {
				return fmt.Errorf("%w: domain %s is already reserved", apperrors.ErrInvalidArgument, domain)
			}
		}
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) recordOperation(ctx context.Context, principal domainidentity.Principal, operationType, targetID, targetLabel, result, summary string, metadata map[string]any) {
	if s.operations == nil {
		return
	}
	targetScope := map[string]any{"module": "docker"}
	if targetID != "" {
		targetScope["targetId"] = targetID
	}
	if targetLabel != "" {
		targetScope["targetLabel"] = targetLabel
	}
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operationType, targetScope, result, summary, sanitizeMetadata(metadata)))
}

func validateHostInput(input domaindocker.HostInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: docker host name is required", apperrors.ErrInvalidArgument)
	}
	if input.AvailablePortStart > 0 && input.AvailablePortEnd > 0 && input.AvailablePortStart > input.AvailablePortEnd {
		return fmt.Errorf("%w: available port range is invalid", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateProjectInput(input domaindocker.ProjectInput) error {
	if strings.TrimSpace(input.HostID) == "" {
		return fmt.Errorf("%w: hostId is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: docker project name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.ComposeContent) == "" {
		return fmt.Errorf("%w: composeContent is required", apperrors.ErrInvalidArgument)
	}
	if names := composeServiceNames(input.ComposeContent); len(names) == 0 {
		return fmt.Errorf("%w: composeContent must define at least one service", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateContainerStartInput(input domaindocker.ContainerStartInput) error {
	if strings.TrimSpace(input.HostID) == "" {
		return fmt.Errorf("%w: hostId is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: container name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Image) == "" {
		return fmt.Errorf("%w: image is required", apperrors.ErrInvalidArgument)
	}
	if input.ContainerPort <= 0 || input.ContainerPort > 65535 {
		return fmt.Errorf("%w: containerPort must be between 1 and 65535", apperrors.ErrInvalidArgument)
	}
	if input.HostPort <= 0 || input.HostPort > 65535 {
		return fmt.Errorf("%w: hostPort must be between 1 and 65535", apperrors.ErrInvalidArgument)
	}
	protocol := normalizedProtocol(input.Protocol)
	if protocol != "tcp" && protocol != "udp" {
		return fmt.Errorf("%w: protocol must be tcp or udp", apperrors.ErrInvalidArgument)
	}
	if err := validateDomainAccess(input.DomainName, input.DomainScheme); err != nil {
		return err
	}
	if policy := normalizedRestartPolicy(input.RestartPolicy); policy == "" {
		return fmt.Errorf("%w: restartPolicy is invalid", apperrors.ErrInvalidArgument)
	}
	return nil
}

func normalizePortMappingInput(input domaindocker.PortMappingInput) domaindocker.PortMappingInput {
	input.Protocol = normalizedProtocol(input.Protocol)
	input.ExposureScope = firstNonEmpty(input.ExposureScope, "internal")
	input.DomainName = strings.ToLower(strings.TrimSpace(input.DomainName))
	if input.DomainName != "" {
		input.DomainScheme = normalizedDomainScheme(input.DomainScheme, input.DomainTLSEnabled)
		if input.AccessURL == "" {
			input.AccessURL = input.DomainScheme + "://" + input.DomainName
		}
	} else {
		input.DomainScheme = ""
		input.DomainTLSEnabled = false
	}
	return input
}

func singleContainerComposeContent(serviceName string, input domaindocker.ContainerStartInput) (string, error) {
	service := map[string]any{
		"image":   strings.TrimSpace(input.Image),
		"restart": normalizedRestartPolicy(input.RestartPolicy),
		"ports": []string{
			fmt.Sprintf("%s:%d:%d/%s", firstNonEmpty(input.HostIP, "0.0.0.0"), input.HostPort, input.ContainerPort, normalizedProtocol(input.Protocol)),
		},
	}
	if value := strings.TrimSpace(input.ImagePullPolicy); value != "" {
		service["pull_policy"] = value
	}
	if strings.TrimSpace(input.EnvContent) != "" {
		service["env_file"] = []string{".env"}
	}
	if value := strings.TrimSpace(input.Command); value != "" {
		service["command"] = value
	}
	if value := strings.TrimSpace(input.Entrypoint); value != "" {
		service["entrypoint"] = value
	}
	if value := strings.TrimSpace(input.Network); value != "" {
		service["networks"] = []string{value}
	}
	if labels := domainProxyLabels(serviceName, input); len(labels) > 0 {
		service["labels"] = labels
	}
	doc := map[string]any{
		"services": map[string]any{
			serviceName: service,
		},
	}
	if value := strings.TrimSpace(input.Network); value != "" {
		doc["networks"] = map[string]any{
			value: map[string]any{"external": true},
		}
	}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal single container compose: %w", err)
	}
	return string(raw), nil
}

func domainProxyLabels(serviceName string, input domaindocker.ContainerStartInput) []string {
	domain := strings.ToLower(strings.TrimSpace(input.DomainName))
	if domain == "" {
		return nil
	}
	routerName := NormalizeSlug(serviceName + "-" + domain)
	entrypoint := "web"
	if input.DomainTLSEnabled || strings.EqualFold(input.DomainScheme, "https") {
		entrypoint = "websecure"
	}
	labels := []string{
		"traefik.enable=true",
		fmt.Sprintf("traefik.http.routers.%s.rule=Host(`%s`)", routerName, domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints=%s", routerName, entrypoint),
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port=%d", routerName, input.ContainerPort),
	}
	if entrypoint == "websecure" {
		labels = append(labels, fmt.Sprintf("traefik.http.routers.%s.tls=true", routerName))
	}
	return labels
}

func singleContainerAccessURL(input domaindocker.ContainerStartInput, host domaindocker.Host) string {
	if domain := strings.TrimSpace(input.DomainName); domain != "" {
		return normalizedDomainScheme(input.DomainScheme, input.DomainTLSEnabled) + "://" + strings.ToLower(domain)
	}
	accessHost := strings.TrimSpace(input.HostIP)
	if accessHost == "" || accessHost == "0.0.0.0" {
		accessHost = firstNonEmpty(host.IPAddress, endpointHost(host.Endpoint), host.ID)
	}
	if accessHost == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", accessHost, input.HostPort)
}

func validateDomainAccess(domainName string, domainScheme string) error {
	domain := strings.ToLower(strings.TrimSpace(domainName))
	if domain == "" {
		return nil
	}
	if strings.Contains(domain, "://") || strings.ContainsAny(domain, "/\\:") || !domainPattern.MatchString(domain) {
		return fmt.Errorf("%w: domainName must be a hostname without scheme, port, or path", apperrors.ErrInvalidArgument)
	}
	if strings.HasPrefix(domain, "*.") {
		return fmt.Errorf("%w: wildcard domain is not supported for container access", apperrors.ErrInvalidArgument)
	}
	scheme := strings.ToLower(strings.TrimSpace(domainScheme))
	if scheme != "" && scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: domainScheme must be http or https", apperrors.ErrInvalidArgument)
	}
	return nil
}

func normalizedProtocol(value string) string {
	protocol := strings.ToLower(strings.TrimSpace(value))
	if protocol == "" {
		return "tcp"
	}
	return protocol
}

func normalizedDomainScheme(value string, tlsEnabled bool) string {
	scheme := strings.ToLower(strings.TrimSpace(value))
	if scheme == "" {
		if tlsEnabled {
			return "https"
		}
		return "http"
	}
	return scheme
}

func normalizedRestartPolicy(value string) string {
	policy := strings.TrimSpace(value)
	if policy == "" {
		return "unless-stopped"
	}
	if slices.Contains([]string{"no", "always", "on-failure", "unless-stopped"}, policy) {
		return policy
	}
	return ""
}

func endpointHost(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "tcp://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	if trimmed == "" {
		return ""
	}
	if host, _, found := strings.Cut(trimmed, ":"); found {
		return host
	}
	return trimmed
}

func composeServiceNames(content string) []string {
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	servicesRaw, ok := raw["services"]
	if !ok {
		return nil
	}
	services, ok := servicesRaw.(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(services))
	for name := range services {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

func summarizeHosts(items []domaindocker.Host) domaindocker.HostSummary {
	out := domaindocker.HostSummary{Total: len(items)}
	for _, item := range items {
		switch strings.ToLower(item.Status) {
		case "online", "ready", "healthy":
			out.Online++
		case "degraded":
			out.Degraded++
		case "offline", "unavailable":
			out.Offline++
		case "provisioning", "pending":
			out.Provisioning++
		}
	}
	return out
}

func summarizeProjects(items []domaindocker.Project) domaindocker.StatusSummary {
	out := domaindocker.StatusSummary{Total: len(items)}
	for _, item := range items {
		countStatus(&out, item.Status)
	}
	return out
}

func summarizeServices(items []domaindocker.Service) domaindocker.StatusSummary {
	out := domaindocker.StatusSummary{Total: len(items)}
	for _, item := range items {
		countStatus(&out, item.Status)
	}
	return out
}

func summarizePorts(items []domaindocker.PortMapping) domaindocker.PortSummary {
	out := domaindocker.PortSummary{Total: len(items)}
	now := time.Now().UTC()
	for _, item := range items {
		switch strings.ToLower(item.ExposureScope) {
		case "vpn":
			out.VPN++
		case "public":
			out.Public++
		default:
			out.Internal++
		}
		if item.ExpiresAt != nil && item.ExpiresAt.Before(now) {
			out.Expired++
		}
	}
	return out
}

func countStatus(out *domaindocker.StatusSummary, status string) {
	switch strings.ToLower(status) {
	case "running", "online", "ready", "active":
		out.Running++
	case "pending", "queued", "provisioning", "defined", "draft":
		out.Pending++
	case "failed", "error", "callback_timeout":
		out.Failed++
	case "stopped", "exited", "disabled":
		out.Stopped++
	default:
		out.Unknown++
	}
}

func pageOf[T any](items []T, total, page, pageSize int) domaindocker.Page[T] {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 500 {
		pageSize = 500
	}
	return domaindocker.Page[T]{Items: items, Total: total, Page: page, PageSize: pageSize}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mergeMap(base map[string]any, updates map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range base {
		out[key] = value
	}
	for key, value := range updates {
		out[key] = value
	}
	return out
}

func sanitizeMetadata(metadata map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range metadata {
		out[key] = value
	}
	for _, key := range []string{"credential", "token", "password", "secret", "envContent"} {
		delete(out, key)
	}
	return out
}

func validCallbackStatus(status string) bool {
	return slices.Contains([]string{OperationStatusRunning, OperationStatusCompleted, OperationStatusFailed, OperationStatusCanceled, OperationStatusTimeout}, strings.TrimSpace(status))
}

func operationTerminal(status string) bool {
	return slices.Contains([]string{OperationStatusCompleted, OperationStatusFailed, OperationStatusCanceled, OperationStatusTimeout}, strings.TrimSpace(status))
}

func projectStatusForOperation(item domaindocker.Operation, callbackStatus string) (string, string) {
	if callbackStatus == OperationStatusFailed || callbackStatus == OperationStatusTimeout {
		return "failed", ""
	}
	if callbackStatus == OperationStatusCanceled {
		return "stopped", ""
	}
	if callbackStatus != OperationStatusCompleted || (item.OperationKind != OperationKindProjectDeploy && item.OperationKind != OperationKindContainerStart) {
		return "", ""
	}
	action := stringValue(item.Payload, "action")
	switch action {
	case "deploy", "redeploy", "start", "restart":
		return "running", "running"
	case "stop":
		return "stopped", "stopped"
	case "down", "destroy":
		return "stopped", "removed"
	case "pull", "build":
		return "", ""
	default:
		return "running", "running"
	}
}

func serviceStatusForAction(action string) string {
	switch strings.TrimSpace(action) {
	case "stop":
		return "stopped"
	case "start", "restart":
		return "running"
	default:
		return ""
	}
}

func serviceInputsFromPayload(item domaindocker.Operation, payload map[string]any) []domaindocker.ServiceInput {
	raw, ok := payload["services"]
	if !ok {
		return nil
	}
	records, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]domaindocker.ServiceInput, 0, len(records))
	for _, record := range records {
		mapped, ok := record.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(mapped["name"]))
		if name == "" {
			continue
		}
		out = append(out, domaindocker.ServiceInput{
			ProjectID:      item.ProjectID,
			HostID:         item.HostID,
			Name:           name,
			Image:          strings.TrimSpace(fmt.Sprint(mapped["image"])),
			Status:         firstNonEmpty(strings.TrimSpace(fmt.Sprint(mapped["status"])), "unknown"),
			ContainerID:    strings.TrimSpace(fmt.Sprint(mapped["containerId"])),
			RestartCount:   intValue(mapped["restartCount"]),
			CPUPercent:     floatValue(mapped["cpuPercent"]),
			MemoryBytes:    int64Value(mapped["memoryBytes"]),
			NetworkRxBytes: int64Value(mapped["networkRxBytes"]),
			NetworkTxBytes: int64Value(mapped["networkTxBytes"]),
			Config:         mapValueAny(mapped["config"]),
		})
	}
	return out
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intValue(raw any) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		i, _ := value.Int64()
		return int(i)
	default:
		return 0
	}
}

func int64Value(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		i, _ := value.Int64()
		return i
	default:
		return 0
	}
}

func floatValue(raw any) float64 {
	switch value := raw.(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		f, _ := value.Float64()
		return f
	default:
		return 0
	}
}

func mapValueAny(raw any) map[string]any {
	mapped, ok := raw.(map[string]any)
	if !ok || mapped == nil {
		return map[string]any{}
	}
	return mapped
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func bytesToMiB(value int64) int {
	if value <= 0 {
		return 0
	}
	return int(value / (1024 * 1024))
}

func bytesToGiB(value int64) int {
	if value <= 0 {
		return 0
	}
	return int(value / (1024 * 1024 * 1024))
}

func stringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

var (
	slugPattern   = regexp.MustCompile(`[^a-z0-9._-]+`)
	domainPattern = regexp.MustCompile(`^(\*\.)?[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)
)

func NormalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "docker-project"
	}
	return value
}
