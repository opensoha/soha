package docker

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domaindocker "github.com/soha/soha/internal/domain/docker"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	"github.com/soha/soha/internal/platform/apperrors"
)

func TestCreateProjectUpsertsServicesFromCompose(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "dev-docker", Status: "online"}
	service := New(repo, dockerTestPermissions(), &captureDockerOperations{})

	project, err := service.CreateProject(context.Background(), dockerTestPrincipal(), domaindocker.ProjectInput{
		HostID: "host-1",
		Name:   "demo",
		ComposeContent: `services:
  api:
    image: nginx:alpine
  worker:
    image: alpine:3.20
`,
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if project.ID == "" {
		t.Fatalf("CreateProject() returned empty project id")
	}
	names := make([]string, 0, len(repo.services))
	for _, item := range repo.services {
		names = append(names, item.Name)
		if item.ProjectID != project.ID {
			t.Fatalf("service %s projectID = %s, want %s", item.Name, item.ProjectID, project.ID)
		}
		if item.Status != "defined" {
			t.Fatalf("service %s status = %s, want defined", item.Name, item.Status)
		}
	}
	slices.Sort(names)
	if got, want := names, []string{"api", "worker"}; !slices.Equal(got, want) {
		t.Fatalf("upserted services = %v, want %v", got, want)
	}
}

func TestCreateProjectRejectsComposeWithoutServices(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "dev-docker", Status: "online"}
	service := New(repo, dockerTestPermissions(), nil)

	_, err := service.CreateProject(context.Background(), dockerTestPrincipal(), domaindocker.ProjectInput{
		HostID:         "host-1",
		Name:           "invalid",
		ComposeContent: "name: invalid\n",
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateProject() error = %v, want ErrInvalidArgument", err)
	}
	if len(repo.projects) != 0 || len(repo.services) != 0 {
		t.Fatalf("invalid compose created projects=%d services=%d", len(repo.projects), len(repo.services))
	}
}

func TestRunnerClaimAndCallbackCompletesOperation(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "dev-docker", Status: "online"}
	repo.projects["project-1"] = domaindocker.Project{ID: "project-1", HostID: "host-1", Name: "demo", Slug: "demo", Status: "draft"}
	service := New(repo, dockerTestPermissions(), nil)
	operation, err := service.DeployProject(context.Background(), dockerTestPrincipal(), "project-1", "deploy")
	if err != nil {
		t.Fatalf("DeployProject() error = %v", err)
	}

	claimed, err := service.ClaimOperation(context.Background(), domaindocker.OperationClaimInput{WorkerID: "worker-1"})
	if err != nil {
		t.Fatalf("ClaimOperation() error = %v", err)
	}
	if claimed.ID != operation.ID || claimed.Status != OperationStatusRunning || claimed.ClaimedByWorkerID != "worker-1" {
		t.Fatalf("claimed operation = %#v, want id=%s running worker-1", claimed, operation.ID)
	}

	updated, err := service.RecordOperationCallback(context.Background(), domaindocker.OperationCallbackInput{
		OperationID: claimed.ID,
		WorkerID:    "worker-1",
		Status:      OperationStatusCompleted,
		Payload: map[string]any{
			"services": []any{
				map[string]any{"name": "web", "image": "nginx:alpine", "status": "running"},
			},
		},
		Logs: []string{"docker compose up completed"},
	})
	if err != nil {
		t.Fatalf("RecordOperationCallback() error = %v", err)
	}
	if updated.Status != OperationStatusCompleted || updated.FinishedAt == nil {
		t.Fatalf("updated operation status = %s finishedAt=%v, want completed with finishedAt", updated.Status, updated.FinishedAt)
	}
	if repo.projects["project-1"].Status != "running" {
		t.Fatalf("project status = %s, want running", repo.projects["project-1"].Status)
	}
	if _, ok := repo.services["project-1:web"]; !ok {
		t.Fatalf("callback did not upsert service web")
	}
	if len(repo.logs[operation.ID]) == 0 {
		t.Fatalf("callback did not append operation logs")
	}
}

func TestRunnerCallbackDoesNotWriteNilRuntimeFields(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "dev-docker", Status: "online"}
	repo.operations["operation-1"] = domaindocker.Operation{
		ID:                "operation-1",
		HostID:            "host-1",
		OperationKind:     OperationKindHostSync,
		Status:            OperationStatusRunning,
		ClaimedByWorkerID: "worker-1",
	}
	service := New(repo, dockerTestPermissions(), nil)

	_, err := service.RecordOperationCallback(context.Background(), domaindocker.OperationCallbackInput{
		OperationID: "operation-1",
		WorkerID:    "worker-1",
		Status:      OperationStatusCompleted,
		Payload: map[string]any{
			"dockerVersion":  "Docker version 29.4.0",
			"composeVersion": "Docker Compose version v5.1.2",
		},
	})
	if err != nil {
		t.Fatalf("RecordOperationCallback() error = %v", err)
	}
	host := repo.hosts["host-1"]
	if host.Endpoint == "<nil>" || host.IPAddress == "<nil>" || host.AgentVersion == "<nil>" {
		t.Fatalf("host runtime fields were polluted by nil payload values: %#v", host)
	}
	if host.Endpoint != "" || host.IPAddress != "" || host.AgentVersion != "" {
		t.Fatalf("host runtime fields = endpoint %q ip %q agentVersion %q, want empty values", host.Endpoint, host.IPAddress, host.AgentVersion)
	}
}

func TestStartContainerCreatesSingleContainerProjectAndDomainMapping(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "dev-docker", Status: "online", IPAddress: "10.0.0.10"}
	service := New(repo, dockerTestPermissions(), nil)

	operation, err := service.StartContainer(context.Background(), dockerTestPrincipal(), domaindocker.ContainerStartInput{
		HostID:           "host-1",
		Name:             "Preview API",
		Image:            "nginx:alpine",
		ContainerPort:    80,
		HostPort:         18080,
		DomainName:       "preview.internal.example.com",
		DomainTLSEnabled: true,
		EnvContent:       "APP_ENV=test",
	})
	if err != nil {
		t.Fatalf("StartContainer() error = %v", err)
	}
	if operation.OperationKind != OperationKindContainerStart || operation.Status != OperationStatusQueued {
		t.Fatalf("operation = %#v, want queued container_start", operation)
	}
	if len(repo.projects) != 1 || len(repo.services) != 1 || len(repo.ports) != 1 {
		t.Fatalf("created projects=%d services=%d ports=%d, want 1 each", len(repo.projects), len(repo.services), len(repo.ports))
	}
	var project domaindocker.Project
	for _, item := range repo.projects {
		project = item
	}
	if project.SourceKind != "single_container" || project.Slug != "preview-api" {
		t.Fatalf("project SourceKind=%s Slug=%s, want single_container preview-api", project.SourceKind, project.Slug)
	}
	if !strings.Contains(project.ComposeContent, "nginx:alpine") || !strings.Contains(project.ComposeContent, "18080:80/tcp") || !strings.Contains(project.ComposeContent, "traefik.http.routers") {
		t.Fatalf("compose content missing generated container settings:\n%s", project.ComposeContent)
	}
	var port domaindocker.PortMapping
	for _, item := range repo.ports {
		port = item
	}
	if port.DomainName != "preview.internal.example.com" || port.DomainScheme != "https" || port.AccessURL != "https://preview.internal.example.com" {
		t.Fatalf("port domain mapping = %#v", port)
	}
	if operation.Payload["portMappingId"] != port.ID {
		t.Fatalf("operation portMappingId = %#v, want %s", operation.Payload["portMappingId"], port.ID)
	}
}

func TestStartContainerDoesNotLeavePartialRecordsWhenTransactionalCreateFails(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.hosts["host-1"] = domaindocker.Host{ID: "host-1", Name: "dev-docker", Status: "online"}
	repo.failCreateOperation = errors.New("operation insert failed")
	service := New(repo, dockerTestPermissions(), nil)

	_, err := service.StartContainer(context.Background(), dockerTestPrincipal(), domaindocker.ContainerStartInput{
		HostID:        "host-1",
		Name:          "Preview API",
		Image:         "nginx:alpine",
		ContainerPort: 80,
		HostPort:      18080,
	})
	if !errors.Is(err, repo.failCreateOperation) {
		t.Fatalf("StartContainer() error = %v, want injected failure", err)
	}
	if len(repo.projects) != 0 || len(repo.services) != 0 || len(repo.ports) != 0 || len(repo.operations) != 0 {
		t.Fatalf("transactional failure left projects=%d services=%d ports=%d operations=%d", len(repo.projects), len(repo.services), len(repo.ports), len(repo.operations))
	}
}

func TestServiceActionRejectsUnsupportedExec(t *testing.T) {
	repo := newMemoryDockerRepo()
	repo.services["service-1"] = domaindocker.Service{ID: "service-1", ProjectID: "project-1", HostID: "host-1", Name: "api"}
	service := New(repo, dockerTestPermissions(), nil)

	_, err := service.ServiceAction(context.Background(), dockerTestPrincipal(), "service-1", "exec")
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("ServiceAction(exec) error = %v, want ErrInvalidArgument", err)
	}
	if len(repo.operations) != 0 {
		t.Fatalf("unsupported exec enqueued %d operations", len(repo.operations))
	}
}

func TestQuickCreateHostEnqueuesVirtualizationVMWhenConnectionProvided(t *testing.T) {
	repo := newMemoryDockerRepo()
	provisioner := &captureHostProvisioner{}
	service := New(repo, dockerTestPermissions(), nil, WithHostProvisioner(provisioner))

	operation, err := service.QuickCreateHost(context.Background(), dockerTestPrincipal(), domaindocker.QuickCreateHostInput{
		Name:                       "docker-dev-1",
		VirtualizationConnectionID: "pve-1",
		ImageID:                    "image-1",
		FlavorID:                   "flavor-1",
		CPUCoreCount:               2,
		MemoryBytes:                4 * 1024 * 1024 * 1024,
		DiskBytes:                  40 * 1024 * 1024 * 1024,
		Network:                    "vmbr0",
	})
	if err != nil {
		t.Fatalf("QuickCreateHost() error = %v", err)
	}
	if provisioner.input.ConnectionID != "pve-1" || provisioner.input.MemoryMiB != 4096 || provisioner.input.DiskGiB != 40 {
		t.Fatalf("provisioner input = %#v", provisioner.input)
	}
	if operation.Status != OperationStatusRunning {
		t.Fatalf("operation status = %s, want running", operation.Status)
	}
	if operation.Payload["virtualizationTaskId"] != "vm-task-1" {
		t.Fatalf("operation payload virtualizationTaskId = %v, want vm-task-1", operation.Payload["virtualizationTaskId"])
	}
}

func TestQuickCreateHostReconcilesCompletedVirtualizationTask(t *testing.T) {
	repo := newMemoryDockerRepo()
	provisioner := &captureHostProvisioner{}
	service := New(repo, dockerTestPermissions(), nil, WithHostProvisioner(provisioner))

	operation, err := service.QuickCreateHost(context.Background(), dockerTestPrincipal(), domaindocker.QuickCreateHostInput{
		Name:                       "docker-dev-1",
		VirtualizationConnectionID: "pve-1",
		ImageID:                    "image-1",
		CPUCoreCount:               2,
		MemoryBytes:                4 * 1024 * 1024 * 1024,
		DiskBytes:                  40 * 1024 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("QuickCreateHost() error = %v", err)
	}
	provisioner.tasks["vm-task-1"] = HostProvisionTask{
		ID:           "vm-task-1",
		Provider:     "pve",
		ConnectionID: "pve-1",
		Status:       OperationStatusCompleted,
		VMID:         "vm-1",
		VMName:       "docker-dev-1",
		Result:       map[string]any{"vmId": "vm-1", "name": "docker-dev-1", "ipAddress": "10.0.0.21"},
	}

	hosts, err := service.ListHosts(context.Background(), dockerTestPrincipal(), domaindocker.HostFilter{})
	if err != nil {
		t.Fatalf("ListHosts() error = %v", err)
	}
	if hosts.Total != 1 {
		t.Fatalf("hosts total = %d, want 1", hosts.Total)
	}
	host := hosts.Items[0]
	if host.Status != "ready" || host.VMID != "vm-1" || host.VMName != "docker-dev-1" || host.IPAddress != "10.0.0.21" {
		t.Fatalf("host after reconcile = %#v", host)
	}
	updated := repo.operations[operation.ID]
	if updated.Status != OperationStatusCompleted || updated.FinishedAt == nil {
		t.Fatalf("operation after reconcile = %#v", updated)
	}
	if updated.Result["virtualizationTaskStatus"] != OperationStatusCompleted || updated.Result["vmId"] != "vm-1" {
		t.Fatalf("operation result after reconcile = %#v", updated.Result)
	}
}

func TestQuickCreateHostReconcilesFailedVirtualizationTask(t *testing.T) {
	repo := newMemoryDockerRepo()
	provisioner := &captureHostProvisioner{}
	service := New(repo, dockerTestPermissions(), nil, WithHostProvisioner(provisioner))

	operation, err := service.QuickCreateHost(context.Background(), dockerTestPrincipal(), domaindocker.QuickCreateHostInput{
		Name:                       "docker-dev-failed",
		VirtualizationConnectionID: "pve-1",
		ImageID:                    "image-1",
	})
	if err != nil {
		t.Fatalf("QuickCreateHost() error = %v", err)
	}
	provisioner.tasks["vm-task-1"] = HostProvisionTask{
		ID:           "vm-task-1",
		Provider:     "pve",
		ConnectionID: "pve-1",
		Status:       OperationStatusFailed,
		Result:       map[string]any{"error": "pve api returned status 400"},
	}

	_, err = service.ListOperations(context.Background(), dockerTestPrincipal(), domaindocker.OperationFilter{})
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	host := repo.hosts["host-1"]
	if host.Status != "degraded" {
		t.Fatalf("host status = %s, want degraded", host.Status)
	}
	updated := repo.operations[operation.ID]
	if updated.Status != OperationStatusFailed || updated.FinishedAt == nil {
		t.Fatalf("operation after reconcile = %#v", updated)
	}
	if updated.Result["message"] != "pve api returned status 400" {
		t.Fatalf("operation result message = %#v", updated.Result)
	}
}

func TestCancelHostProvisionCancelsVirtualizationTask(t *testing.T) {
	repo := newMemoryDockerRepo()
	provisioner := &captureHostProvisioner{}
	service := New(repo, dockerTestPermissions(), nil, WithHostProvisioner(provisioner))

	operation, err := service.QuickCreateHost(context.Background(), dockerTestPrincipal(), domaindocker.QuickCreateHostInput{
		Name:                       "docker-dev-cancel",
		VirtualizationConnectionID: "pve-1",
		ImageID:                    "image-1",
	})
	if err != nil {
		t.Fatalf("QuickCreateHost() error = %v", err)
	}

	canceled, err := service.CancelOperation(context.Background(), dockerTestPrincipal(), operation.ID)
	if err != nil {
		t.Fatalf("CancelOperation() error = %v", err)
	}
	if canceled.Status != OperationStatusCanceled || canceled.FinishedAt == nil {
		t.Fatalf("canceled operation = %#v", canceled)
	}
	if len(provisioner.canceledIDs) != 1 || provisioner.canceledIDs[0] != "vm-task-1" {
		t.Fatalf("canceled virtualization task ids = %#v", provisioner.canceledIDs)
	}
	if task := provisioner.tasks["vm-task-1"]; task.Status != OperationStatusCanceled {
		t.Fatalf("linked virtualization task after cancel = %#v", task)
	}
	if canceled.Result["virtualizationTaskStatus"] != OperationStatusCanceled {
		t.Fatalf("canceled operation result = %#v", canceled.Result)
	}
}

func TestRetryHostProvisionRetriesVirtualizationTask(t *testing.T) {
	repo := newMemoryDockerRepo()
	provisioner := &captureHostProvisioner{}
	service := New(repo, dockerTestPermissions(), nil, WithHostProvisioner(provisioner))

	operation, err := service.QuickCreateHost(context.Background(), dockerTestPrincipal(), domaindocker.QuickCreateHostInput{
		Name:                       "docker-dev-retry",
		VirtualizationConnectionID: "pve-1",
		ImageID:                    "image-1",
	})
	if err != nil {
		t.Fatalf("QuickCreateHost() error = %v", err)
	}
	provisioner.tasks["vm-task-1"] = HostProvisionTask{
		ID:           "vm-task-1",
		Provider:     "pve",
		ConnectionID: "pve-1",
		Status:       OperationStatusFailed,
		Result:       map[string]any{"error": "pve api returned status 400"},
	}
	if _, err := service.ListOperations(context.Background(), dockerTestPrincipal(), domaindocker.OperationFilter{}); err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	if failed := repo.operations[operation.ID]; failed.Status != OperationStatusFailed {
		t.Fatalf("operation before retry = %#v", failed)
	}

	retried, err := service.RetryOperation(context.Background(), dockerTestPrincipal(), operation.ID)
	if err != nil {
		t.Fatalf("RetryOperation() error = %v", err)
	}
	if retried.Status != OperationStatusQueued || retried.FinishedAt != nil {
		t.Fatalf("retried operation = %#v", retried)
	}
	if len(provisioner.retriedIDs) != 1 || provisioner.retriedIDs[0] != "vm-task-1" {
		t.Fatalf("retried virtualization task ids = %#v", provisioner.retriedIDs)
	}
	if task := provisioner.tasks["vm-task-1"]; task.Status != OperationStatusQueued {
		t.Fatalf("linked virtualization task after retry = %#v", task)
	}
	if retried.Result["virtualizationTaskStatus"] != OperationStatusQueued {
		t.Fatalf("retried operation result = %#v", retried.Result)
	}
}

func dockerTestPermissions() *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{
		"admin": {
			appaccess.PermDockerOverviewView,
			appaccess.PermDockerHostsView,
			appaccess.PermDockerHostsManage,
			appaccess.PermDockerProjectsView,
			appaccess.PermDockerProjectsManage,
			appaccess.PermDockerProjectsDeploy,
			appaccess.PermDockerServicesView,
			appaccess.PermDockerServicesManage,
			appaccess.PermDockerPortsView,
			appaccess.PermDockerPortsManage,
			appaccess.PermDockerTemplatesView,
			appaccess.PermDockerTemplatesManage,
			appaccess.PermDockerOperationsView,
			appaccess.PermDockerOperationsManage,
		},
	}})
}

func dockerTestPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "admin", UserName: "Admin", Roles: []string{"admin"}}
}

type dockerTestRoleReader struct {
	matrix map[string][]string
}

func (r dockerTestRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

type captureDockerOperations struct {
	entries []domainoperation.Entry
}

func (c *captureDockerOperations) Record(_ context.Context, entry domainoperation.Entry) error {
	c.entries = append(c.entries, entry)
	return nil
}

type captureHostProvisioner struct {
	input       HostProvisionInput
	tasks       map[string]HostProvisionTask
	canceledIDs []string
	retriedIDs  []string
}

func (c *captureHostProvisioner) ProvisionDockerHost(_ context.Context, _ domainidentity.Principal, input HostProvisionInput) (HostProvisionTask, error) {
	c.input = input
	task := HostProvisionTask{ID: "vm-task-1", Provider: "pve", ConnectionID: input.ConnectionID, Status: OperationStatusQueued}
	if c.tasks == nil {
		c.tasks = map[string]HostProvisionTask{}
	}
	c.tasks[task.ID] = task
	return task, nil
}

func (c *captureHostProvisioner) GetProvisionTask(_ context.Context, taskID string) (HostProvisionTask, error) {
	if c.tasks == nil {
		return HostProvisionTask{}, apperrors.ErrNotFound
	}
	task, ok := c.tasks[taskID]
	if !ok {
		return HostProvisionTask{}, apperrors.ErrNotFound
	}
	return task, nil
}

func (c *captureHostProvisioner) CancelProvisionTask(_ context.Context, _ domainidentity.Principal, taskID string) (HostProvisionTask, error) {
	task, err := c.GetProvisionTask(context.Background(), taskID)
	if err != nil {
		return HostProvisionTask{}, err
	}
	task.Status = OperationStatusCanceled
	task.Result = mergeMap(task.Result, map[string]any{"message": "operation canceled"})
	c.tasks[taskID] = task
	c.canceledIDs = append(c.canceledIDs, taskID)
	return task, nil
}

func (c *captureHostProvisioner) RetryProvisionTask(_ context.Context, _ domainidentity.Principal, taskID string) (HostProvisionTask, error) {
	task, err := c.GetProvisionTask(context.Background(), taskID)
	if err != nil {
		return HostProvisionTask{}, err
	}
	task.Status = OperationStatusQueued
	task.Result = mergeMap(task.Result, map[string]any{"message": "operation queued for retry"})
	c.tasks[taskID] = task
	c.retriedIDs = append(c.retriedIDs, taskID)
	return task, nil
}

type memoryDockerRepo struct {
	hosts               map[string]domaindocker.Host
	projects            map[string]domaindocker.Project
	services            map[string]domaindocker.Service
	ports               map[string]domaindocker.PortMapping
	templates           map[string]domaindocker.Template
	operations          map[string]domaindocker.Operation
	logs                map[string][]domaindocker.OperationLog
	failCreateOperation error
}

func newMemoryDockerRepo() *memoryDockerRepo {
	return &memoryDockerRepo{
		hosts:      map[string]domaindocker.Host{},
		projects:   map[string]domaindocker.Project{},
		services:   map[string]domaindocker.Service{},
		ports:      map[string]domaindocker.PortMapping{},
		templates:  map[string]domaindocker.Template{},
		operations: map[string]domaindocker.Operation{},
		logs:       map[string][]domaindocker.OperationLog{},
	}
}

func (r *memoryDockerRepo) ListHosts(context.Context, domaindocker.HostFilter) ([]domaindocker.Host, error) {
	items := make([]domaindocker.Host, 0, len(r.hosts))
	for _, item := range r.hosts {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryDockerRepo) CountHosts(ctx context.Context, filter domaindocker.HostFilter) (int, error) {
	items, err := r.ListHosts(ctx, filter)
	return len(items), err
}

func (r *memoryDockerRepo) GetHost(_ context.Context, id string) (domaindocker.Host, error) {
	item, ok := r.hosts[id]
	if !ok {
		return domaindocker.Host{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryDockerRepo) CreateHost(_ context.Context, input domaindocker.HostInput) (domaindocker.Host, error) {
	item := domaindocker.Host{ID: nextDockerID("host", len(r.hosts)), Name: input.Name, Status: firstNonEmpty(input.Status, "pending"), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	r.hosts[item.ID] = item
	return item, nil
}

func (r *memoryDockerRepo) UpdateHost(_ context.Context, id string, input domaindocker.HostInput) (domaindocker.Host, error) {
	item, ok := r.hosts[id]
	if !ok {
		return domaindocker.Host{}, apperrors.ErrNotFound
	}
	item.Name = input.Name
	item.Status = firstNonEmpty(input.Status, item.Status)
	r.hosts[id] = item
	return item, nil
}

func (r *memoryDockerRepo) TouchHostRuntime(_ context.Context, id string, input domaindocker.HostInput) (domaindocker.Host, error) {
	item, ok := r.hosts[id]
	if !ok {
		return domaindocker.Host{}, apperrors.ErrNotFound
	}
	if input.Status != "" {
		item.Status = input.Status
	}
	if input.AgentID != "" {
		item.AgentID = input.AgentID
	}
	if input.AgentVersion != "" {
		item.AgentVersion = input.AgentVersion
	}
	if input.DockerVersion != "" {
		item.DockerVersion = input.DockerVersion
	}
	if input.ComposeVersion != "" {
		item.ComposeVersion = input.ComposeVersion
	}
	if input.Endpoint != "" {
		item.Endpoint = input.Endpoint
	}
	if input.IPAddress != "" {
		item.IPAddress = input.IPAddress
	}
	if input.VMID != "" {
		item.VMID = input.VMID
	}
	if input.VMName != "" {
		item.VMName = input.VMName
	}
	if input.Config != nil {
		item.Config = mergeMap(item.Config, input.Config)
	}
	now := time.Now().UTC()
	item.LastHeartbeatAt = &now
	r.hosts[id] = item
	return item, nil
}

func (r *memoryDockerRepo) DeleteHost(_ context.Context, id string) error {
	delete(r.hosts, id)
	return nil
}

func (r *memoryDockerRepo) ListProjects(context.Context, domaindocker.ProjectFilter) ([]domaindocker.Project, error) {
	items := make([]domaindocker.Project, 0, len(r.projects))
	for _, item := range r.projects {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryDockerRepo) CountProjects(ctx context.Context, filter domaindocker.ProjectFilter) (int, error) {
	items, err := r.ListProjects(ctx, filter)
	return len(items), err
}

func (r *memoryDockerRepo) GetProject(_ context.Context, id string) (domaindocker.Project, error) {
	item, ok := r.projects[id]
	if !ok {
		return domaindocker.Project{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryDockerRepo) CreateProject(_ context.Context, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	item := domaindocker.Project{ID: nextDockerID("project", len(r.projects)), HostID: input.HostID, Name: input.Name, Slug: firstNonEmpty(input.Slug, input.Name), SourceKind: input.SourceKind, ComposeContent: input.ComposeContent, EnvContent: input.EnvContent, Status: firstNonEmpty(input.Status, "draft"), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	r.projects[item.ID] = item
	return item, nil
}

func (r *memoryDockerRepo) UpdateProject(_ context.Context, id string, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	item, ok := r.projects[id]
	if !ok {
		return domaindocker.Project{}, apperrors.ErrNotFound
	}
	item.HostID = input.HostID
	item.Name = input.Name
	item.ComposeContent = input.ComposeContent
	item.EnvContent = input.EnvContent
	item.Status = firstNonEmpty(input.Status, item.Status)
	r.projects[id] = item
	return item, nil
}

func (r *memoryDockerRepo) UpdateProjectRuntime(_ context.Context, id string, status string, desiredState string, lastDeployedAt *time.Time) (domaindocker.Project, error) {
	item, ok := r.projects[id]
	if !ok {
		return domaindocker.Project{}, apperrors.ErrNotFound
	}
	if status != "" {
		item.Status = status
	}
	if desiredState != "" {
		item.DesiredState = desiredState
	}
	if lastDeployedAt != nil {
		item.LastDeployedAt = lastDeployedAt
	}
	r.projects[id] = item
	return item, nil
}

func (r *memoryDockerRepo) DeleteProject(_ context.Context, id string) error {
	delete(r.projects, id)
	return nil
}

func (r *memoryDockerRepo) ListServices(context.Context, domaindocker.ServiceFilter) ([]domaindocker.Service, error) {
	items := make([]domaindocker.Service, 0, len(r.services))
	for _, item := range r.services {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryDockerRepo) CountServices(ctx context.Context, filter domaindocker.ServiceFilter) (int, error) {
	items, err := r.ListServices(ctx, filter)
	return len(items), err
}

func (r *memoryDockerRepo) GetService(_ context.Context, id string) (domaindocker.Service, error) {
	item, ok := r.services[id]
	if !ok {
		return domaindocker.Service{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryDockerRepo) UpsertService(_ context.Context, input domaindocker.ServiceInput) (domaindocker.Service, error) {
	id := input.ID
	if id == "" {
		id = input.ProjectID + ":" + input.Name
	}
	item := domaindocker.Service{ID: id, ProjectID: input.ProjectID, HostID: input.HostID, Name: input.Name, Image: input.Image, Status: firstNonEmpty(input.Status, "unknown"), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	r.services[id] = item
	return item, nil
}

func (r *memoryDockerRepo) DeleteService(_ context.Context, id string) error {
	delete(r.services, id)
	return nil
}

func (r *memoryDockerRepo) ListPortMappings(context.Context, domaindocker.PortMappingFilter) ([]domaindocker.PortMapping, error) {
	items := make([]domaindocker.PortMapping, 0, len(r.ports))
	for _, item := range r.ports {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryDockerRepo) CountPortMappings(ctx context.Context, filter domaindocker.PortMappingFilter) (int, error) {
	items, err := r.ListPortMappings(ctx, filter)
	return len(items), err
}

func (r *memoryDockerRepo) GetPortMapping(_ context.Context, id string) (domaindocker.PortMapping, error) {
	item, ok := r.ports[id]
	if !ok {
		return domaindocker.PortMapping{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryDockerRepo) CreatePortMapping(_ context.Context, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	item := domaindocker.PortMapping{ID: nextDockerID("port", len(r.ports)), HostID: input.HostID, ProjectID: input.ProjectID, ServiceID: input.ServiceID, Name: input.Name, HostIP: input.HostIP, HostPort: input.HostPort, ContainerPort: input.ContainerPort, Protocol: firstNonEmpty(input.Protocol, "tcp"), ExposureScope: firstNonEmpty(input.ExposureScope, "internal"), Status: firstNonEmpty(input.Status, "active"), DomainName: input.DomainName, DomainScheme: input.DomainScheme, DomainTLSEnabled: input.DomainTLSEnabled, AccessURL: input.AccessURL}
	if input.ID != "" {
		item.ID = input.ID
	}
	r.ports[item.ID] = item
	return item, nil
}

func (r *memoryDockerRepo) UpdatePortMapping(_ context.Context, id string, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	item, ok := r.ports[id]
	if !ok {
		return domaindocker.PortMapping{}, apperrors.ErrNotFound
	}
	item.Name = input.Name
	item.HostPort = input.HostPort
	item.ContainerPort = input.ContainerPort
	r.ports[id] = item
	return item, nil
}

func (r *memoryDockerRepo) DeletePortMapping(_ context.Context, id string) error {
	delete(r.ports, id)
	return nil
}

func (r *memoryDockerRepo) CreateContainerStart(ctx context.Context, input domaindocker.ContainerStartCreateInput) (domaindocker.ContainerStartCreateResult, error) {
	snapshotProjects := cloneDockerMap(r.projects)
	snapshotServices := cloneDockerMap(r.services)
	snapshotPorts := cloneDockerMap(r.ports)
	snapshotOperations := cloneDockerMap(r.operations)
	project, err := r.CreateProject(ctx, input.Project)
	if err != nil {
		return domaindocker.ContainerStartCreateResult{}, err
	}
	serviceInput := input.Service
	serviceInput.ProjectID = project.ID
	if serviceInput.HostID == "" {
		serviceInput.HostID = project.HostID
	}
	service, err := r.UpsertService(ctx, serviceInput)
	if err != nil {
		r.projects = snapshotProjects
		r.services = snapshotServices
		r.ports = snapshotPorts
		r.operations = snapshotOperations
		return domaindocker.ContainerStartCreateResult{}, err
	}
	portInput := input.PortMapping
	portInput.ProjectID = project.ID
	portInput.ServiceID = service.ID
	port, err := r.CreatePortMapping(ctx, portInput)
	if err != nil {
		r.projects = snapshotProjects
		r.services = snapshotServices
		r.ports = snapshotPorts
		r.operations = snapshotOperations
		return domaindocker.ContainerStartCreateResult{}, err
	}
	operationInput := input.Operation
	operationInput.ProjectID = project.ID
	operationInput.ServiceID = service.ID
	if operationInput.HostID == "" {
		operationInput.HostID = project.HostID
	}
	operation, err := r.CreateOperation(ctx, operationInput)
	if err != nil {
		r.projects = snapshotProjects
		r.services = snapshotServices
		r.ports = snapshotPorts
		r.operations = snapshotOperations
		return domaindocker.ContainerStartCreateResult{}, err
	}
	return domaindocker.ContainerStartCreateResult{Project: project, Service: service, PortMapping: port, Operation: operation}, nil
}

func (r *memoryDockerRepo) ListTemplates(context.Context, domaindocker.TemplateFilter) ([]domaindocker.Template, error) {
	items := make([]domaindocker.Template, 0, len(r.templates))
	for _, item := range r.templates {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryDockerRepo) CountTemplates(ctx context.Context, filter domaindocker.TemplateFilter) (int, error) {
	items, err := r.ListTemplates(ctx, filter)
	return len(items), err
}

func (r *memoryDockerRepo) GetTemplate(_ context.Context, id string) (domaindocker.Template, error) {
	item, ok := r.templates[id]
	if !ok {
		return domaindocker.Template{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryDockerRepo) CreateTemplate(_ context.Context, input domaindocker.TemplateInput) (domaindocker.Template, error) {
	item := domaindocker.Template{ID: nextDockerID("template", len(r.templates)), Name: input.Name, TemplateKind: firstNonEmpty(input.TemplateKind, "compose"), ComposeContent: input.ComposeContent, Enabled: input.Enabled}
	r.templates[item.ID] = item
	return item, nil
}

func (r *memoryDockerRepo) UpdateTemplate(_ context.Context, id string, input domaindocker.TemplateInput) (domaindocker.Template, error) {
	item, ok := r.templates[id]
	if !ok {
		return domaindocker.Template{}, apperrors.ErrNotFound
	}
	item.Name = input.Name
	item.ComposeContent = input.ComposeContent
	item.Enabled = input.Enabled
	r.templates[id] = item
	return item, nil
}

func (r *memoryDockerRepo) DeleteTemplate(_ context.Context, id string) error {
	delete(r.templates, id)
	return nil
}

func (r *memoryDockerRepo) CreateOperation(_ context.Context, input domaindocker.OperationInput) (domaindocker.Operation, error) {
	if r.failCreateOperation != nil {
		return domaindocker.Operation{}, r.failCreateOperation
	}
	item := domaindocker.Operation{ID: nextDockerID("operation", len(r.operations)), HostID: input.HostID, ProjectID: input.ProjectID, ServiceID: input.ServiceID, OperationKind: input.OperationKind, Status: firstNonEmpty(input.Status, "queued"), Payload: input.Payload, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	r.operations[item.ID] = item
	return item, nil
}

func (r *memoryDockerRepo) UpdateOperation(_ context.Context, item domaindocker.Operation) (domaindocker.Operation, error) {
	r.operations[item.ID] = item
	return item, nil
}

func (r *memoryDockerRepo) ClaimOperation(_ context.Context, workerID string, agentID string, _ []string, _ []string, now time.Time) (domaindocker.Operation, error) {
	for id, item := range r.operations {
		if item.Status != OperationStatusQueued {
			continue
		}
		item.Status = OperationStatusRunning
		item.ClaimedByWorkerID = workerID
		item.AttemptCount++
		item.StartedAt = &now
		item.LastHeartbeatAt = &now
		r.operations[id] = item
		return item, nil
	}
	return domaindocker.Operation{}, apperrors.ErrNotFound
}

func (r *memoryDockerRepo) GetOperation(_ context.Context, id string) (domaindocker.Operation, error) {
	item, ok := r.operations[id]
	if !ok {
		return domaindocker.Operation{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryDockerRepo) ListOperations(context.Context, domaindocker.OperationFilter) ([]domaindocker.Operation, error) {
	items := make([]domaindocker.Operation, 0, len(r.operations))
	for _, item := range r.operations {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryDockerRepo) CountOperations(ctx context.Context, filter domaindocker.OperationFilter) (int, error) {
	items, err := r.ListOperations(ctx, filter)
	return len(items), err
}

func (r *memoryDockerRepo) CreateOperationLog(_ context.Context, item domaindocker.OperationLog) error {
	r.logs[item.OperationID] = append(r.logs[item.OperationID], item)
	return nil
}

func (r *memoryDockerRepo) ListOperationLogs(_ context.Context, operationID string, _ int) ([]domaindocker.OperationLog, error) {
	return r.logs[operationID], nil
}

func nextDockerID(prefix string, current int) string {
	return fmt.Sprintf("%s-%d", prefix, current+1)
}

func cloneDockerMap[T any](items map[string]T) map[string]T {
	out := make(map[string]T, len(items))
	for key, value := range items {
		out[key] = value
	}
	return out
}
