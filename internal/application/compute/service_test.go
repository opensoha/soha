package compute

import (
	"context"
	"errors"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type roleReader struct{ keys []string }

func (r roleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"test": r.keys}, nil
}

type virtualizationFake struct {
	connections     []domainvirtualization.Connection
	vms             []domainvirtualization.VM
	tasks           []domainvirtualization.Task
	err             error
	connectionReads int
	vmReads         int
	logs            []domainvirtualization.TaskLog
	canceled        string
	retried         string
}

func (f *virtualizationFake) GetConnection(_ context.Context, id string) (domainvirtualization.Connection, error) {
	for _, item := range f.connections {
		if item.ID == id {
			return item, nil
		}
	}
	return domainvirtualization.Connection{}, apperrors.ErrNotFound
}
func (f *virtualizationFake) ListConnections(_ context.Context, filter domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error) {
	f.connectionReads++
	if f.err != nil {
		return nil, f.err
	}
	out := []domainvirtualization.Connection{}
	for _, item := range f.connections {
		if filter.Provider == "" || filter.Provider == item.Provider {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *virtualizationFake) GetVM(_ context.Context, id string) (domainvirtualization.VM, error) {
	for _, item := range f.vms {
		if item.ID == id {
			return item, nil
		}
	}
	return domainvirtualization.VM{}, apperrors.ErrNotFound
}
func (f *virtualizationFake) ListVMs(_ context.Context, filter domainvirtualization.VMFilter) ([]domainvirtualization.VM, error) {
	f.vmReads++
	if f.err != nil {
		return nil, f.err
	}
	out := []domainvirtualization.VM{}
	for _, item := range f.vms {
		if filter.ConnectionID == "" || filter.ConnectionID == item.ConnectionID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *virtualizationFake) GetImage(context.Context, string) (domainvirtualization.Image, error) {
	return domainvirtualization.Image{}, apperrors.ErrNotFound
}
func (f *virtualizationFake) GetFlavor(context.Context, string) (domainvirtualization.Flavor, error) {
	return domainvirtualization.Flavor{}, apperrors.ErrNotFound
}
func (f *virtualizationFake) GetTask(_ context.Context, id string) (domainvirtualization.Task, error) {
	for _, item := range f.tasks {
		if item.ID == id {
			return item, nil
		}
	}
	return domainvirtualization.Task{}, apperrors.ErrNotFound
}
func (f *virtualizationFake) ListTasks(context.Context, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]domainvirtualization.Task(nil), f.tasks...), nil
}
func (f *virtualizationFake) GetOperation(_ context.Context, _ domainidentity.Principal, id string) (domainvirtualization.Task, error) {
	return f.GetTask(context.Background(), id)
}
func (f *virtualizationFake) ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domainvirtualization.TaskLog, error) {
	return append([]domainvirtualization.TaskLog(nil), f.logs...), nil
}
func (f *virtualizationFake) CancelOperation(_ context.Context, _ domainidentity.Principal, id string) (domainvirtualization.Task, error) {
	f.canceled = id
	return f.GetTask(context.Background(), id)
}
func (f *virtualizationFake) RetryOperation(_ context.Context, _ domainidentity.Principal, id string) (domainvirtualization.Task, error) {
	f.retried = id
	return f.GetTask(context.Background(), id)
}

type runtimeFake struct {
	hosts        []domaindocker.Host
	projects     []domaindocker.Project
	services     []domaindocker.Service
	ports        []domaindocker.PortMapping
	operations   []domaindocker.Operation
	hostReads    int
	projectReads int
	serviceReads int
	portReads    int
	logs         []domaindocker.OperationLog
	canceled     string
	retried      string
}

func (f *runtimeFake) GetHost(_ context.Context, id string) (domaindocker.Host, error) {
	for _, item := range f.hosts {
		if item.ID == id {
			return item, nil
		}
	}
	return domaindocker.Host{}, apperrors.ErrNotFound
}
func (f *runtimeFake) ListHosts(context.Context, domaindocker.HostFilter) ([]domaindocker.Host, error) {
	f.hostReads++
	return append([]domaindocker.Host(nil), f.hosts...), nil
}
func (f *runtimeFake) GetProject(_ context.Context, id string) (domaindocker.Project, error) {
	for _, item := range f.projects {
		if item.ID == id {
			return item, nil
		}
	}
	return domaindocker.Project{}, apperrors.ErrNotFound
}
func (f *runtimeFake) ListProjects(_ context.Context, filter domaindocker.ProjectFilter) ([]domaindocker.Project, error) {
	f.projectReads++
	out := []domaindocker.Project{}
	for _, item := range f.projects {
		if filter.HostID == "" || item.HostID == filter.HostID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *runtimeFake) GetService(_ context.Context, id string) (domaindocker.Service, error) {
	for _, item := range f.services {
		if item.ID == id {
			return item, nil
		}
	}
	return domaindocker.Service{}, apperrors.ErrNotFound
}
func (f *runtimeFake) ListServices(_ context.Context, filter domaindocker.ServiceFilter) ([]domaindocker.Service, error) {
	f.serviceReads++
	out := []domaindocker.Service{}
	for _, item := range f.services {
		if (filter.ProjectID == "" || item.ProjectID == filter.ProjectID) && (filter.HostID == "" || item.HostID == filter.HostID) {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *runtimeFake) GetPortMapping(_ context.Context, id string) (domaindocker.PortMapping, error) {
	for _, item := range f.ports {
		if item.ID == id {
			return item, nil
		}
	}
	return domaindocker.PortMapping{}, apperrors.ErrNotFound
}
func (f *runtimeFake) ListPortMappings(_ context.Context, filter domaindocker.PortMappingFilter) ([]domaindocker.PortMapping, error) {
	f.portReads++
	out := []domaindocker.PortMapping{}
	for _, item := range f.ports {
		if filter.ServiceID == "" || item.ServiceID == filter.ServiceID {
			out = append(out, item)
		}
	}
	return out, nil
}
func (f *runtimeFake) GetTemplate(context.Context, string) (domaindocker.Template, error) {
	return domaindocker.Template{}, apperrors.ErrNotFound
}
func (f *runtimeFake) GetOperation(_ context.Context, _ domainidentity.Principal, id string) (domaindocker.Operation, error) {
	for _, item := range f.operations {
		if item.ID == id {
			return item, nil
		}
	}
	return domaindocker.Operation{}, apperrors.ErrNotFound
}
func (f *runtimeFake) ListOperations(context.Context, domaindocker.OperationFilter) ([]domaindocker.Operation, error) {
	return append([]domaindocker.Operation(nil), f.operations...), nil
}
func (f *runtimeFake) ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domaindocker.OperationLog, error) {
	return append([]domaindocker.OperationLog(nil), f.logs...), nil
}
func (f *runtimeFake) CancelOperation(_ context.Context, _ domainidentity.Principal, id string) (domaindocker.Operation, error) {
	f.canceled = id
	return f.GetOperation(context.Background(), domainidentity.Principal{}, id)
}
func (f *runtimeFake) RetryOperation(_ context.Context, _ domainidentity.Principal, id string) (domaindocker.Operation, error) {
	f.retried = id
	return f.GetOperation(context.Background(), domainidentity.Principal{}, id)
}

func newTestService(keys ...string) (*Service, *virtualizationFake, *runtimeFake) {
	virt, runtime := &virtualizationFake{}, &runtimeFake{}
	resolver := appaccess.NewPermissionResolver(roleReader{keys: keys})
	return New(virt, runtime, resolver, Options{VirtualizationEnabled: true, RuntimeEnabled: true, VirtualizationTasks: virt, RuntimeTasks: runtime}), virt, runtime
}
func testPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "u", Roles: []string{"test"}}
}

func TestListAccessSourcesFiltersVirtualizationProvider(t *testing.T) {
	service, virt, _ := newTestService(appaccess.PermVirtualizationClustersView)
	virt.connections = []domainvirtualization.Connection{{ID: "p", Provider: "pve", Name: "PVE", Enabled: true}, {ID: "k", Provider: "kubevirt", Name: "KV", Enabled: true}}
	result, err := service.ListAccessSources(context.Background(), testPrincipal(), AccessSourceFilter{ProviderKey: "pve"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ProviderKey != "pve" {
		t.Fatalf("items = %#v", result.Items)
	}
}

func TestOverviewOmitsUnauthorizedSectionsAndDegradesReadFailure(t *testing.T) {
	service, virt, _ := newTestService(appaccess.PermVirtualizationOverviewView)
	virt.err = errors.New("database unavailable")
	result, err := service.Overview(context.Background(), testPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if result.Virtualization == nil || result.Virtualization.Status != sohaapi.ComputeSectionStatusUnavailable || !result.Partial {
		t.Fatalf("overview = %#v", result)
	}
	if result.Agents != nil || result.Runtimes != nil || result.RuntimeWorkloads != nil || result.Tasks != nil {
		t.Fatalf("unauthorized sections leaked: %#v", result)
	}
}

func TestOverviewDerivesVisibilityFromChildPermissions(t *testing.T) {
	for _, permission := range []string{appaccess.PermVirtualizationImagesView, appaccess.PermVirtualizationFlavorsView} {
		service, virt, runtime := newTestService(permission)
		result, err := service.Overview(context.Background(), testPrincipal())
		if err != nil {
			t.Fatal(err)
		}
		if result.Virtualization != nil || result.Runtimes != nil || virt.connectionReads != 0 || virt.vmReads != 0 || runtime.hostReads != 0 {
			t.Fatalf("%s-only overview = %#v", permission, result)
		}
	}

	service, _, runtime := newTestService(appaccess.PermDockerTemplatesView)
	result, err := service.Overview(context.Background(), testPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtimes != nil || result.RuntimeWorkloads != nil || result.Virtualization != nil || runtime.hostReads != 0 || runtime.projectReads != 0 || runtime.serviceReads != 0 || runtime.portReads != 0 {
		t.Fatalf("template-only overview = %#v", result)
	}
}

func TestOverviewReadsOnlyAuthorizedSummaryResources(t *testing.T) {
	service, virt, runtime := newTestService(appaccess.PermVirtualizationClustersView, appaccess.PermDockerProjectsView)
	result, err := service.Overview(context.Background(), testPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if virt.connectionReads != 1 || virt.vmReads != 0 {
		t.Fatalf("virtualization reads connections=%d vms=%d", virt.connectionReads, virt.vmReads)
	}
	if runtime.projectReads != 1 || runtime.hostReads != 0 || runtime.serviceReads != 0 || runtime.portReads != 0 {
		t.Fatalf("runtime reads hosts=%d projects=%d services=%d ports=%d", runtime.hostReads, runtime.projectReads, runtime.serviceReads, runtime.portReads)
	}
	if result.Virtualization == nil || result.Virtualization.Summary == nil || result.Virtualization.Summary.VmsTotal != 0 || len(result.Virtualization.Warnings) == 0 {
		t.Fatalf("virtualization redaction = %#v", result.Virtualization)
	}
	if result.RuntimeWorkloads == nil || result.RuntimeWorkloads.Summary == nil || result.RuntimeWorkloads.Summary.Services != 0 || len(result.RuntimeWorkloads.Warnings) == 0 {
		t.Fatalf("runtime redaction = %#v", result.RuntimeWorkloads)
	}
}

func TestModuleFlagsIsolateComputeDomains(t *testing.T) {
	service, virt, runtime := newTestService(appaccess.PermVirtualizationOverviewView, appaccess.PermDockerOverviewView, appaccess.PermVirtualizationClustersView, appaccess.PermDockerHostsView)
	virt.connections = []domainvirtualization.Connection{{ID: "v", Provider: "pve", Enabled: true}}
	runtime.hosts = []domaindocker.Host{{ID: "d", Status: "docker_ready"}}
	service.virtualizationEnabled, service.runtimeEnabled = true, false
	overview, err := service.Overview(context.Background(), testPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Virtualization == nil || overview.Runtimes != nil || overview.Agents != nil {
		t.Fatalf("virtualization-only overview = %#v", overview)
	}
	access, err := service.ListAccessSources(context.Background(), testPrincipal(), AccessSourceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(access.Items) != 1 || access.Items[0].SourceType != sohaapi.ComputeAccessSourceTypeVirtualizationConnection {
		t.Fatalf("virtualization-only access = %#v", access.Items)
	}

	service.virtualizationEnabled, service.runtimeEnabled = false, true
	overview, err = service.Overview(context.Background(), testPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if overview.Virtualization != nil || overview.Runtimes == nil || overview.Agents == nil {
		t.Fatalf("runtime-only overview = %#v", overview)
	}
}

func TestVirtualizationTaskPermissionsSeparateSyncAndOperations(t *testing.T) {
	service, virt, _ := newTestService(appaccess.PermVirtualizationSyncView)
	now := time.Now().UTC()
	virt.tasks = []domainvirtualization.Task{{ID: "sync", TaskKind: "asset_sync", Status: "running", CreatedAt: now}, {ID: "action", TaskKind: "vm_action", Status: "queued", CreatedAt: now.Add(-time.Second)}}
	result, err := service.ListTasks(context.Background(), testPrincipal(), TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "sync" {
		t.Fatalf("items = %#v", result.Items)
	}
}

func TestOperationTaskViewIncludesLifecycleAndGenericOperations(t *testing.T) {
	service, virt, _ := newTestService(appaccess.PermVirtualizationOperationsView)
	now := time.Now().UTC()
	virt.tasks = []domainvirtualization.Task{
		{ID: "lifecycle", TaskKind: "vm_action", Status: "succeeded", CreatedAt: now},
		{ID: "operation", TaskKind: "diagnostic", Status: "succeeded", CreatedAt: now.Add(-time.Second)},
		{ID: "sync", TaskKind: "asset_sync", Status: "succeeded", CreatedAt: now.Add(-2 * time.Second)},
	}

	result, err := service.ListTasks(context.Background(), testPrincipal(), TaskFilter{Category: "operation"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].ID != "lifecycle" || result.Items[1].ID != "operation" {
		t.Fatalf("operation view items = %#v", result.Items)
	}
}

func TestTaskCursorRemainsStableWhenNewTaskArrives(t *testing.T) {
	service, virt, _ := newTestService(appaccess.PermVirtualizationOperationsView)
	now := time.Now().UTC()
	virt.tasks = []domainvirtualization.Task{{ID: "three", TaskKind: "vm_action", CreatedAt: now}, {ID: "two", TaskKind: "vm_action", CreatedAt: now.Add(-time.Minute)}, {ID: "one", TaskKind: "vm_action", CreatedAt: now.Add(-2 * time.Minute)}}
	first, err := service.ListTasks(context.Background(), testPrincipal(), TaskFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	virt.tasks = append(virt.tasks, domainvirtualization.Task{ID: "new", TaskKind: "vm_action", CreatedAt: now.Add(time.Minute)})
	second, err := service.ListTasks(context.Background(), testPrincipal(), TaskFilter{Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "two" {
		t.Fatalf("second page = %#v", second.Items)
	}
	if _, err := service.ListTasks(context.Background(), testPrincipal(), TaskFilter{Cursor: "not-a-cursor"}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestListTasksFiltersExactReferencedResource(t *testing.T) {
	service, virt, runtime := newTestService(appaccess.PermVirtualizationOperationsView, appaccess.PermDockerOperationsView)
	now := time.Now().UTC()
	virt.tasks = []domainvirtualization.Task{{ID: "virt-match", TaskKind: "vm_action", ConnectionID: "cluster-1", CreatedAt: now}}
	runtime.operations = []domaindocker.Operation{
		{ID: "runtime-match", OperationKind: "deploy", ProjectID: "project-1", CreatedAt: now},
		{ID: "runtime-other", OperationKind: "deploy", ProjectID: "project-2", CreatedAt: now},
	}

	result, err := service.ListTasks(context.Background(), testPrincipal(), TaskFilter{ResourceKind: "project", ResourceID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "runtime-match" {
		t.Fatalf("resource-filtered tasks = %#v", result.Items)
	}
}

func TestTaskFacadeRoutesDetailLogsCancelAndRetry(t *testing.T) {
	service, virt, runtime := newTestService(appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationOperationsManage, appaccess.PermDockerOperationsView, appaccess.PermDockerOperationsManage)
	now := time.Now().UTC()
	virt.tasks = []domainvirtualization.Task{{ID: "virt-1", TaskKind: "vm_action", Status: "running", CreatedAt: now}}
	virt.logs = []domainvirtualization.TaskLog{{ID: "v-log", TaskID: "virt-1", LogLevel: "info", Message: "started", Payload: map[string]any{"step": 1}, CreatedAt: now}}
	runtime.operations = []domaindocker.Operation{{ID: "docker-1", OperationKind: "deploy", Status: "failed", CreatedAt: now}}
	runtime.logs = []domaindocker.OperationLog{{ID: "d-log", OperationID: "docker-1", LogLevel: "error", Message: "failed", CreatedAt: now}}

	task, err := service.GetTask(context.Background(), testPrincipal(), "virtualization", "virt-1")
	if err != nil || task.ID != "virt-1" {
		t.Fatalf("GetTask = %#v, %v", task, err)
	}
	logs, err := service.ListTaskLogs(context.Background(), testPrincipal(), "virtualization", "virt-1")
	if err != nil || len(logs.Items) != 1 || logs.Items[0].Payload != `{"step":1}` {
		t.Fatalf("ListTaskLogs = %#v, %v", logs, err)
	}
	if _, err := service.CancelTask(context.Background(), testPrincipal(), "virtualization", "virt-1"); err != nil || virt.canceled != "virt-1" {
		t.Fatalf("CancelTask err=%v canceled=%q", err, virt.canceled)
	}
	if _, err := service.RetryTask(context.Background(), testPrincipal(), "container_runtime", "docker-1"); err != nil || runtime.retried != "docker-1" {
		t.Fatalf("RetryTask err=%v retried=%q", err, runtime.retried)
	}
	if _, err := service.GetTask(context.Background(), testPrincipal(), "bogus", "task"); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("invalid domain error = %v", err)
	}
}
