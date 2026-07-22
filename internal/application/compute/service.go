package compute

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxReadLimit = 1000
	generation   = int64(1)
)

type VirtualizationReader interface {
	ListConnections(context.Context, domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error)
	ListVMs(context.Context, domainvirtualization.VMFilter) ([]domainvirtualization.VM, error)
	ListTasks(context.Context, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error)
}

type RuntimeReader interface {
	ListHosts(context.Context, domaindocker.HostFilter) ([]domaindocker.Host, error)
	ListProjects(context.Context, domaindocker.ProjectFilter) ([]domaindocker.Project, error)
	ListServices(context.Context, domaindocker.ServiceFilter) ([]domaindocker.Service, error)
	ListPortMappings(context.Context, domaindocker.PortMappingFilter) ([]domaindocker.PortMapping, error)
	ListOperations(context.Context, domaindocker.OperationFilter) ([]domaindocker.Operation, error)
}

type VirtualizationTaskController interface {
	GetOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domainvirtualization.TaskLog, error)
	CancelOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	RetryOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
}

type RuntimeTaskController interface {
	GetOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
	ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domaindocker.OperationLog, error)
	CancelOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
	RetryOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
}

type Service struct {
	virtualization        VirtualizationReader
	runtime               RuntimeReader
	virtualizationTasks   VirtualizationTaskController
	runtimeTasks          RuntimeTaskController
	permissions           *appaccess.PermissionResolver
	virtualizationEnabled bool
	runtimeEnabled        bool
	modules               interface{ ModuleEnabled(string) bool }
}

type Options struct {
	VirtualizationEnabled bool
	RuntimeEnabled        bool
	VirtualizationTasks   VirtualizationTaskController
	RuntimeTasks          RuntimeTaskController
	ModuleState           interface{ ModuleEnabled(string) bool }
}

func New(virtualization VirtualizationReader, runtime RuntimeReader, permissions *appaccess.PermissionResolver, options Options) *Service {
	return &Service{
		virtualization: virtualization, runtime: runtime, permissions: permissions,
		virtualizationTasks: options.VirtualizationTasks, runtimeTasks: options.RuntimeTasks,
		virtualizationEnabled: options.VirtualizationEnabled, runtimeEnabled: options.RuntimeEnabled,
		modules: options.ModuleState,
	}
}

func (s *Service) virtualizationAvailable() bool {
	if s.modules != nil {
		return s.modules.ModuleEnabled("virtualization")
	}
	return s.virtualizationEnabled
}

func (s *Service) runtimeAvailable() bool {
	if s.modules != nil {
		return s.modules.ModuleEnabled("docker")
	}
	return s.runtimeEnabled
}

func (s *Service) Overview(ctx context.Context, principal domainidentity.Principal) (sohaapi.ComputeOverview, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeOverview{}, err
	}
	out := sohaapi.ComputeOverview{Attention: []sohaapi.ComputeAttention{}, ProviderHealth: []sohaapi.ComputeProviderHealth{}, Warnings: []sohaapi.ComputeWarning{}}
	authorized := s.appendVirtualizationOverview(ctx, keys, &out)
	authorized = s.appendRuntimeOverview(ctx, keys, &out) || authorized
	authorized = s.appendTaskOverview(ctx, keys, &out) || authorized
	if !authorized {
		return sohaapi.ComputeOverview{}, fmt.Errorf("%w: compute overview is not visible", apperrors.ErrAccessDenied)
	}
	return out, nil
}

func (s *Service) appendVirtualizationOverview(ctx context.Context, keys []string, out *sohaapi.ComputeOverview) bool {
	if !s.virtualizationAvailable() || !virtualizationDomainVisible(keys) {
		return false
	}
	readConnections := hasAny(keys, appaccess.PermVirtualizationOverviewView, appaccess.PermVirtualizationClustersView)
	readVMs := hasAny(keys, appaccess.PermVirtualizationOverviewView, appaccess.PermVirtualizationVMsView)
	if !readConnections && !readVMs {
		out.Warnings = append(out.Warnings, sohaapi.ComputeWarning{Code: "virtualization_summary_redacted", Message: "Virtualization summary is hidden by resource permissions"})
		return true
	}
	section, health, attention, failed := s.virtualizationOverview(ctx, readConnections, readVMs)
	out.Virtualization = &section
	out.ProviderHealth = append(out.ProviderHealth, health...)
	out.Attention = append(out.Attention, attention...)
	out.Partial = out.Partial || failed
	return true
}

func (s *Service) appendRuntimeOverview(ctx context.Context, keys []string, out *sohaapi.ComputeOverview) bool {
	runtimeVisible := s.runtimeAvailable() && runtimeDomainVisible(keys)
	if !runtimeVisible {
		return false
	}
	hostsAllowed := runtimeVisible && hasAny(keys, appaccess.PermDockerOverviewView, appaccess.PermDockerHostsView)
	if hostsAllowed {
		hosts, hostsErr := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
		agents, runtimes, health, attention := runtimeHostOverview(hosts, hostsErr)
		out.Agents, out.Runtimes = &agents, &runtimes
		out.ProviderHealth = append(out.ProviderHealth, health)
		out.Attention = append(out.Attention, attention...)
		out.Partial = out.Partial || hostsErr != nil
	}

	readProjects := runtimeVisible && hasAny(keys, appaccess.PermDockerOverviewView, appaccess.PermDockerProjectsView)
	readServices := runtimeVisible && hasAny(keys, appaccess.PermDockerOverviewView, appaccess.PermDockerServicesView)
	readPorts := runtimeVisible && hasAny(keys, appaccess.PermDockerOverviewView, appaccess.PermDockerPortsView)
	if readProjects || readServices || readPorts {
		section, failed := s.runtimeWorkloadOverview(ctx, readProjects, readServices, readPorts)
		out.RuntimeWorkloads = &section
		out.Partial = out.Partial || failed
	} else if runtimeVisible && !hostsAllowed && !has(keys, appaccess.PermDockerOperationsView) {
		out.Warnings = append(out.Warnings, sohaapi.ComputeWarning{Code: "runtime_summary_redacted", Message: "Container runtime summary is hidden by resource permissions"})
	}
	return true
}

func (s *Service) appendTaskOverview(ctx context.Context, keys []string, out *sohaapi.ComputeOverview) bool {
	virtualizationVisible := s.virtualizationAvailable() && hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView)
	runtimeVisible := s.runtimeAvailable() && has(keys, appaccess.PermDockerOperationsView)
	if !virtualizationVisible && !runtimeVisible {
		return false
	}
	section, failed := s.taskOverview(ctx, keys)
	out.Tasks = &section
	out.Partial = out.Partial || failed
	return true
}

func (s *Service) virtualizationOverview(ctx context.Context, readConnections, readVMs bool) (sohaapi.ComputeVirtualizationOverviewSection, []sohaapi.ComputeProviderHealth, []sohaapi.ComputeAttention, bool) {
	connections := []domainvirtualization.Connection{}
	vms := []domainvirtualization.VM{}
	warnings := []sohaapi.ComputeWarning{}
	failed, successfulReads := false, 0
	if readConnections {
		items, err := s.virtualization.ListConnections(ctx, domainvirtualization.ConnectionFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
			warnings = append(warnings, sohaapi.ComputeWarning{Code: "virtualization_connections_read_failed", Message: "Virtualization connections are temporarily unavailable"})
		} else {
			connections = items
			successfulReads++
		}
	} else {
		warnings = append(warnings, sohaapi.ComputeWarning{Code: "virtualization_connections_redacted", Message: "Connection counts are hidden by permissions"})
	}
	if readVMs {
		items, err := s.virtualization.ListVMs(ctx, domainvirtualization.VMFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
			warnings = append(warnings, sohaapi.ComputeWarning{Code: "virtualization_vms_read_failed", Message: "Virtual machine counts are temporarily unavailable"})
		} else {
			vms = items
			successfulReads++
		}
	} else {
		warnings = append(warnings, sohaapi.ComputeWarning{Code: "virtualization_vms_redacted", Message: "Virtual machine counts are hidden by permissions"})
	}
	summary := sohaapi.ComputeVirtualizationSummary{ConnectionsTotal: len(connections), VmsTotal: len(vms)}
	attention := []sohaapi.ComputeAttention{}
	for _, item := range connections {
		status := connectionHealth(item)
		switch status {
		case sohaapi.ComputeHealthStatusHealthy:
			summary.ConnectionsHealthy++
		case sohaapi.ComputeHealthStatusDegraded, sohaapi.ComputeHealthStatusUnavailable:
			summary.ConnectionsDegraded++
		}
		if item.LastSyncedAt == nil {
			summary.ConnectionsUnsynced++
		}
		if status == sohaapi.ComputeHealthStatusUnavailable {
			attention = append(attention, attentionFor("virtualization_connection_unavailable", sohaapi.ComputeAttentionSeverityCritical, "Virtualization connection unavailable", connectionRef(item)))
		}
	}
	for _, item := range vms {
		switch normalizedVMState(item) {
		case "running":
			summary.VmsRunning++
		case "stopped":
			summary.VmsStopped++
		case "error":
			summary.VmsError++
		}
	}
	status := sohaapi.ComputeSectionStatusOK
	if failed {
		status = sohaapi.ComputeSectionStatusDegraded
		if successfulReads == 0 {
			status = sohaapi.ComputeSectionStatusUnavailable
		}
	}
	health := []sohaapi.ComputeProviderHealth{}
	if readConnections {
		health = builtinVirtualizationHealth(connections)
		if failed && successfulReads == 0 {
			health = builtinVirtualizationHealth(nil)
		}
	}
	return sohaapi.ComputeVirtualizationOverviewSection{Status: status, Summary: &summary, Warnings: warnings}, health, attention, failed
}

func runtimeHostOverview(hosts []domaindocker.Host, err error) (sohaapi.ComputeAgentOverviewSection, sohaapi.ComputeRuntimeOverviewSection, sohaapi.ComputeProviderHealth, []sohaapi.ComputeAttention) {
	if err != nil {
		warning := []sohaapi.ComputeWarning{{Code: "runtime_host_read_failed", Message: "Runtime hosts are temporarily unavailable"}}
		return sohaapi.ComputeAgentOverviewSection{Status: sohaapi.ComputeSectionStatusUnavailable, Warnings: warning}, sohaapi.ComputeRuntimeOverviewSection{Status: sohaapi.ComputeSectionStatusUnavailable, Warnings: warning}, providerHealth(sohaapi.ComputeProviderDomainContainerRuntime, "docker", sohaapi.ComputeHealthStatusUnavailable), nil
	}
	agents := sohaapi.ComputeAgentSummary{}
	runtimes := sohaapi.ComputeRuntimeSummary{Total: len(hosts)}
	attention := []sohaapi.ComputeAttention{}
	for _, host := range hosts {
		if strings.TrimSpace(host.AgentID) != "" {
			agents.Total++
			if runtimeHostStatus(host) == sohaapi.ComputeHealthStatusHealthy {
				agents.Online++
			} else {
				agents.Offline++
			}
		}
		switch runtimeHostStatus(host) {
		case sohaapi.ComputeHealthStatusHealthy:
			runtimes.Available++
		case sohaapi.ComputeHealthStatusPending:
			runtimes.WaitingAgent++
		case sohaapi.ComputeHealthStatusDegraded, sohaapi.ComputeHealthStatusUnavailable:
			runtimes.Error++
			attention = append(attention, attentionFor("runtime_host_unavailable", sohaapi.ComputeAttentionSeverityWarning, "Container runtime host needs attention", runtimeHostRef(host)))
		}
	}
	health := sohaapi.ComputeHealthStatusUnknown
	if len(hosts) > 0 {
		health = sohaapi.ComputeHealthStatusHealthy
		if runtimes.Error > 0 {
			health = sohaapi.ComputeHealthStatusDegraded
		}
	}
	return sohaapi.ComputeAgentOverviewSection{Status: sohaapi.ComputeSectionStatusOK, Summary: &agents, Warnings: []sohaapi.ComputeWarning{}}, sohaapi.ComputeRuntimeOverviewSection{Status: sohaapi.ComputeSectionStatusOK, Summary: &runtimes, Warnings: []sohaapi.ComputeWarning{}}, providerHealth(sohaapi.ComputeProviderDomainContainerRuntime, "docker", health), attention
}

func (s *Service) runtimeWorkloadOverview(ctx context.Context, readProjects, readServices, readPorts bool) (sohaapi.ComputeRuntimeWorkloadOverviewSection, bool) {
	projects := []domaindocker.Project{}
	services := []domaindocker.Service{}
	ports := []domaindocker.PortMapping{}
	warnings := []sohaapi.ComputeWarning{}
	failed, successfulReads := false, 0
	if readProjects {
		items, err := s.runtime.ListProjects(ctx, domaindocker.ProjectFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
			warnings = append(warnings, sohaapi.ComputeWarning{Code: "runtime_projects_read_failed", Message: "Project counts are temporarily unavailable"})
		} else {
			projects = items
			successfulReads++
		}
	} else {
		warnings = append(warnings, sohaapi.ComputeWarning{Code: "runtime_projects_redacted", Message: "Project counts are hidden by permissions"})
	}
	if readServices {
		items, err := s.runtime.ListServices(ctx, domaindocker.ServiceFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
			warnings = append(warnings, sohaapi.ComputeWarning{Code: "runtime_services_read_failed", Message: "Service counts are temporarily unavailable"})
		} else {
			services = items
			successfulReads++
		}
	} else {
		warnings = append(warnings, sohaapi.ComputeWarning{Code: "runtime_services_redacted", Message: "Service and container counts are hidden by permissions"})
	}
	if readPorts {
		items, err := s.runtime.ListPortMappings(ctx, domaindocker.PortMappingFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
			warnings = append(warnings, sohaapi.ComputeWarning{Code: "runtime_ports_read_failed", Message: "Port counts are temporarily unavailable"})
		} else {
			ports = items
			successfulReads++
		}
	} else {
		warnings = append(warnings, sohaapi.ComputeWarning{Code: "runtime_ports_redacted", Message: "Port counts are hidden by permissions"})
	}
	summary := sohaapi.ComputeRuntimeWorkloadSummary{Projects: len(projects), Services: len(services), Containers: len(services), Ports: len(ports)}
	now := time.Now().UTC()
	for _, project := range projects {
		if project.ExpiresAt != nil && project.ExpiresAt.After(now) && project.ExpiresAt.Before(now.Add(72*time.Hour)) {
			summary.Expiring++
		}
	}
	status := sohaapi.ComputeSectionStatusOK
	if failed {
		status = sohaapi.ComputeSectionStatusDegraded
		if successfulReads == 0 {
			status = sohaapi.ComputeSectionStatusUnavailable
		}
	}
	return sohaapi.ComputeRuntimeWorkloadOverviewSection{Status: status, Summary: &summary, Warnings: warnings}, failed
}

func (s *Service) taskOverview(ctx context.Context, keys []string) (sohaapi.ComputeTaskOverviewSection, bool) {
	summary := sohaapi.ComputeTaskSummary{}
	failed := false
	if s.virtualizationAvailable() && hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView) {
		items, err := s.virtualization.ListTasks(ctx, domainvirtualization.TaskFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
		} else {
			for _, item := range items {
				if virtualizationTaskVisible(keys, item.TaskKind) {
					addTaskSummary(&summary, normalizeTaskStatus(item.Status))
				}
			}
		}
	}
	if s.runtimeAvailable() && has(keys, appaccess.PermDockerOperationsView) {
		items, err := s.runtime.ListOperations(ctx, domaindocker.OperationFilter{Limit: maxReadLimit})
		if err != nil {
			failed = true
		} else {
			for _, item := range items {
				addTaskSummary(&summary, normalizeTaskStatus(item.Status))
			}
		}
	}
	if failed {
		return sohaapi.ComputeTaskOverviewSection{Status: sohaapi.ComputeSectionStatusDegraded, Summary: &summary, Warnings: []sohaapi.ComputeWarning{{Code: "task_read_partial", Message: "Some task sources are temporarily unavailable"}}}, true
	}
	return sohaapi.ComputeTaskOverviewSection{Status: sohaapi.ComputeSectionStatusOK, Summary: &summary, Warnings: []sohaapi.ComputeWarning{}}, false
}

type AccessSourceFilter struct {
	SourceType  string
	ProviderKey string
	Cursor      string
	Limit       int
}

func (s *Service) ListAccessSources(ctx context.Context, principal domainidentity.Principal, filter AccessSourceFilter) (sohaapi.ComputeAccessSourceListEnvelope, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeAccessSourceListEnvelope{}, err
	}
	accessVisible := (s.virtualizationAvailable() && has(keys, appaccess.PermVirtualizationClustersView)) || (s.runtimeAvailable() && has(keys, appaccess.PermDockerHostsView))
	if !accessVisible {
		return sohaapi.ComputeAccessSourceListEnvelope{}, fmt.Errorf("%w: compute access sources are not visible", apperrors.ErrAccessDenied)
	}
	items, err := s.virtualizationAccessSources(ctx, keys, filter)
	if err != nil {
		return sohaapi.ComputeAccessSourceListEnvelope{}, err
	}
	runtimeItems, err := s.runtimeAccessSources(ctx, keys, filter)
	if err != nil {
		return sohaapi.ComputeAccessSourceListEnvelope{}, err
	}
	items = append(items, runtimeItems...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	page, next, err := paginate(items, filter.Cursor, filter.Limit)
	if err != nil {
		return sohaapi.ComputeAccessSourceListEnvelope{}, err
	}
	return sohaapi.ComputeAccessSourceListEnvelope{Items: page, NextCursor: next}, nil
}

func (s *Service) virtualizationAccessSources(ctx context.Context, keys []string, filter AccessSourceFilter) ([]sohaapi.ComputeAccessSource, error) {
	wantsConnections := filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeVirtualizationConnection)
	if !s.virtualizationAvailable() || !wantsConnections || !has(keys, appaccess.PermVirtualizationClustersView) {
		return []sohaapi.ComputeAccessSource{}, nil
	}
	connections, err := s.virtualization.ListConnections(ctx, domainvirtualization.ConnectionFilter{Provider: filter.ProviderKey, Limit: maxReadLimit})
	if err != nil {
		return nil, err
	}
	items := make([]sohaapi.ComputeAccessSource, 0, len(connections))
	manage := has(keys, appaccess.PermVirtualizationClustersManage)
	for _, connection := range connections {
		items = append(items, connectionAccessSource(connection, manage))
	}
	return items, nil
}

func (s *Service) runtimeAccessSources(ctx context.Context, keys []string, filter AccessSourceFilter) ([]sohaapi.ComputeAccessSource, error) {
	wantsRuntime := filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeRuntimeHost)
	wantsAgent := filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeAgentHost)
	if !s.runtimeAvailable() || (!wantsRuntime && !wantsAgent) || !has(keys, appaccess.PermDockerHostsView) || !providerMatches(filter.ProviderKey, "docker") {
		return []sohaapi.ComputeAccessSource{}, nil
	}
	hosts, err := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
	if err != nil {
		return nil, err
	}
	items := make([]sohaapi.ComputeAccessSource, 0, len(hosts)*2)
	manage := has(keys, appaccess.PermDockerHostsManage)
	for _, host := range hosts {
		if wantsRuntime {
			items = append(items, runtimeAccessSource(host, manage))
		}
		if wantsAgent && strings.TrimSpace(host.AgentID) != "" {
			items = append(items, agentAccessSource(host))
		}
	}
	return items, nil
}

type TaskFilter struct {
	Domain, ProviderKey, Status, Category, ResourceKind, ResourceID, Cursor string
	Limit                                                                   int
}

func (s *Service) ListTasks(ctx context.Context, principal domainidentity.Principal, filter TaskFilter) (sohaapi.ComputeTaskListEnvelope, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeTaskListEnvelope{}, err
	}
	items := []sohaapi.ComputeTaskView{}
	virtualizationVisible := s.virtualizationAvailable() && hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView)
	runtimeVisible := s.runtimeAvailable() && has(keys, appaccess.PermDockerOperationsView)
	if !virtualizationVisible && !runtimeVisible {
		return sohaapi.ComputeTaskListEnvelope{}, fmt.Errorf("%w: compute tasks are not visible", apperrors.ErrAccessDenied)
	}
	if virtualizationVisible && (filter.Domain == "" || filter.Domain == string(sohaapi.ComputeTaskDomainVirtualization)) {
		tasks, readErr := s.virtualization.ListTasks(ctx, domainvirtualization.TaskFilter{Provider: filter.ProviderKey, Limit: maxReadLimit})
		if readErr != nil {
			return sohaapi.ComputeTaskListEnvelope{}, readErr
		}
		for _, task := range tasks {
			if virtualizationTaskVisible(keys, task.TaskKind) {
				items = appendIfTaskMatches(items, virtualizationTaskView(task, has(keys, appaccess.PermVirtualizationOperationsManage)), filter)
			}
		}
	}
	if runtimeVisible && (filter.Domain == "" || filter.Domain == string(sohaapi.ComputeTaskDomainContainerRuntime)) && providerMatches(filter.ProviderKey, "docker") {
		tasks, readErr := s.runtime.ListOperations(ctx, domaindocker.OperationFilter{Limit: maxReadLimit})
		if readErr != nil {
			return sohaapi.ComputeTaskListEnvelope{}, readErr
		}
		for _, task := range tasks {
			items = appendIfTaskMatches(items, runtimeTaskView(task, has(keys, appaccess.PermDockerOperationsManage)), filter)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return taskCursorTie(items[i]) > taskCursorTie(items[j])
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page, next, err := paginateTasks(items, filter.Cursor, filter.Limit)
	if err != nil {
		return sohaapi.ComputeTaskListEnvelope{}, err
	}
	return sohaapi.ComputeTaskListEnvelope{Items: page, NextCursor: next}, nil
}

func (s *Service) GetTask(ctx context.Context, principal domainidentity.Principal, domain, taskID string) (sohaapi.ComputeTaskView, error) {
	switch strings.TrimSpace(domain) {
	case string(sohaapi.ComputeTaskDomainVirtualization):
		if !s.virtualizationAvailable() || s.virtualizationTasks == nil {
			return sohaapi.ComputeTaskView{}, unavailableTaskDomain(domain)
		}
		item, err := s.virtualizationTasks.GetOperation(ctx, principal, strings.TrimSpace(taskID))
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		if !virtualizationTaskVisible(keys, item.TaskKind) {
			return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: compute task is not visible", apperrors.ErrAccessDenied)
		}
		return virtualizationTaskView(item, has(keys, appaccess.PermVirtualizationOperationsManage)), nil
	case string(sohaapi.ComputeTaskDomainContainerRuntime):
		if !s.runtimeAvailable() || s.runtimeTasks == nil {
			return sohaapi.ComputeTaskView{}, unavailableTaskDomain(domain)
		}
		item, err := s.runtimeTasks.GetOperation(ctx, principal, strings.TrimSpace(taskID))
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		return runtimeTaskView(item, has(keys, appaccess.PermDockerOperationsManage)), nil
	default:
		return sohaapi.ComputeTaskView{}, invalidTaskDomain(domain)
	}
}

func (s *Service) ListTaskLogs(ctx context.Context, principal domainidentity.Principal, domain, taskID string) (sohaapi.ComputeTaskLogListEnvelope, error) {
	switch strings.TrimSpace(domain) {
	case string(sohaapi.ComputeTaskDomainVirtualization):
		if !s.virtualizationAvailable() || s.virtualizationTasks == nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, unavailableTaskDomain(domain)
		}
		if _, err := s.virtualizationTasks.GetOperation(ctx, principal, strings.TrimSpace(taskID)); err != nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, err
		}
		items, err := s.virtualizationTasks.ListOperationLogs(ctx, principal, strings.TrimSpace(taskID), maxReadLimit)
		if err != nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, err
		}
		return virtualizationTaskLogs(items)
	case string(sohaapi.ComputeTaskDomainContainerRuntime):
		if !s.runtimeAvailable() || s.runtimeTasks == nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, unavailableTaskDomain(domain)
		}
		if _, err := s.runtimeTasks.GetOperation(ctx, principal, strings.TrimSpace(taskID)); err != nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, err
		}
		items, err := s.runtimeTasks.ListOperationLogs(ctx, principal, strings.TrimSpace(taskID), maxReadLimit)
		if err != nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, err
		}
		return runtimeTaskLogs(items)
	default:
		return sohaapi.ComputeTaskLogListEnvelope{}, invalidTaskDomain(domain)
	}
}

func (s *Service) CancelTask(ctx context.Context, principal domainidentity.Principal, domain, taskID string) (sohaapi.ComputeTaskView, error) {
	return s.mutateTask(ctx, principal, domain, taskID, true)
}

func (s *Service) RetryTask(ctx context.Context, principal domainidentity.Principal, domain, taskID string) (sohaapi.ComputeTaskView, error) {
	return s.mutateTask(ctx, principal, domain, taskID, false)
}

func (s *Service) mutateTask(ctx context.Context, principal domainidentity.Principal, domain, taskID string, cancel bool) (sohaapi.ComputeTaskView, error) {
	switch strings.TrimSpace(domain) {
	case string(sohaapi.ComputeTaskDomainVirtualization):
		if !s.virtualizationAvailable() || s.virtualizationTasks == nil {
			return sohaapi.ComputeTaskView{}, unavailableTaskDomain(domain)
		}
		var item domainvirtualization.Task
		var err error
		if cancel {
			item, err = s.virtualizationTasks.CancelOperation(ctx, principal, strings.TrimSpace(taskID))
		} else {
			item, err = s.virtualizationTasks.RetryOperation(ctx, principal, strings.TrimSpace(taskID))
		}
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		return virtualizationTaskView(item, true), nil
	case string(sohaapi.ComputeTaskDomainContainerRuntime):
		if !s.runtimeAvailable() || s.runtimeTasks == nil {
			return sohaapi.ComputeTaskView{}, unavailableTaskDomain(domain)
		}
		var item domaindocker.Operation
		var err error
		if cancel {
			item, err = s.runtimeTasks.CancelOperation(ctx, principal, strings.TrimSpace(taskID))
		} else {
			item, err = s.runtimeTasks.RetryOperation(ctx, principal, strings.TrimSpace(taskID))
		}
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		return runtimeTaskView(item, true), nil
	default:
		return sohaapi.ComputeTaskView{}, invalidTaskDomain(domain)
	}
}

func invalidTaskDomain(domain string) error {
	return fmt.Errorf("%w: unsupported compute task domain %q", apperrors.ErrInvalidArgument, strings.TrimSpace(domain))
}

func unavailableTaskDomain(domain string) error {
	return fmt.Errorf("%w: compute task domain %q is unavailable", apperrors.ErrUnsupportedOperation, strings.TrimSpace(domain))
}

func virtualizationTaskLogs(items []domainvirtualization.TaskLog) (sohaapi.ComputeTaskLogListEnvelope, error) {
	out := make([]sohaapi.ComputeTaskLog, 0, len(items))
	for _, item := range items {
		payload, err := taskLogPayload(item.Payload)
		if err != nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, err
		}
		out = append(out, sohaapi.ComputeTaskLog{ID: item.ID, TaskID: item.TaskID, LogLevel: item.LogLevel, Message: item.Message, Payload: payload, CreatedAt: item.CreatedAt})
	}
	return sohaapi.ComputeTaskLogListEnvelope{Items: out}, nil
}

func runtimeTaskLogs(items []domaindocker.OperationLog) (sohaapi.ComputeTaskLogListEnvelope, error) {
	out := make([]sohaapi.ComputeTaskLog, 0, len(items))
	for _, item := range items {
		payload, err := taskLogPayload(item.Payload)
		if err != nil {
			return sohaapi.ComputeTaskLogListEnvelope{}, err
		}
		out = append(out, sohaapi.ComputeTaskLog{ID: item.ID, TaskID: item.OperationID, LogLevel: item.LogLevel, Message: item.Message, Payload: payload, CreatedAt: item.CreatedAt})
	}
	return sohaapi.ComputeTaskLogListEnvelope{Items: out}, nil
}

func taskLogPayload(payload map[string]any) (string, error) {
	if len(payload) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode compute task log payload: %w", err)
	}
	return string(raw), nil
}

func connectionAccessSource(item domainvirtualization.Connection, manage bool) sohaapi.ComputeAccessSource {
	actions := []string{}
	if manage {
		actions = []string{"test", "sync", "edit", "delete"}
	}
	return sohaapi.ComputeAccessSource{ID: item.ID, SourceType: sohaapi.ComputeAccessSourceTypeVirtualizationConnection, Resource: connectionRef(item), Status: connectionHealth(item), ProviderKey: item.Provider, ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, AccessMode: sohaapi.ComputeAccessModeDirect, AvailableActions: actions, LastObservedAt: item.LastSyncedAt, RelatedResources: []sohaapi.ComputeResourceRef{}}
}

func runtimeAccessSource(host domaindocker.Host, manage bool) sohaapi.ComputeAccessSource {
	actions := []string{}
	if manage {
		actions = []string{"edit", "delete"}
	}
	return sohaapi.ComputeAccessSource{ID: host.ID, SourceType: sohaapi.ComputeAccessSourceTypeRuntimeHost, Resource: runtimeHostRef(host), Status: runtimeHostStatus(host), ProviderKey: "docker", ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, AccessMode: runtimeAccessMode(host), AvailableActions: actions, LastObservedAt: host.LastHeartbeatAt, RelatedResources: relatedHostResources(host)}
}

func agentAccessSource(host domaindocker.Host) sohaapi.ComputeAccessSource {
	return sohaapi.ComputeAccessSource{ID: host.AgentID, SourceType: sohaapi.ComputeAccessSourceTypeAgentHost, Resource: agentHostRef(host), Status: runtimeHostStatus(host), ProviderKey: "docker", ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, AccessMode: sohaapi.ComputeAccessModeAgentProxy, AvailableActions: []string{}, LastObservedAt: host.LastHeartbeatAt, RelatedResources: []sohaapi.ComputeResourceRef{runtimeHostRef(host)}}
}

func virtualizationTaskView(item domainvirtualization.Task, manage bool) sohaapi.ComputeTaskView {
	status := normalizeTaskStatus(item.Status)
	state := domainvirtualization.BuildOperationState(item, time.Now().UTC())
	resources := []sohaapi.ComputeResourceRef{}
	if item.ConnectionID != "" {
		resources = append(resources, virtualizationResourceRef(sohaapi.ComputeResourceKindConnection, item.ConnectionID, item.ConnectionID, item.Provider, item.ConnectionID))
	}
	if item.VMID != "" {
		resources = append(resources, virtualizationResourceRef(sohaapi.ComputeResourceKindVM, item.VMID, item.VMID, item.Provider, item.ConnectionID))
	}
	cancelable, retryable := state.Cancelable && manage, state.Retryable && manage
	return sohaapi.ComputeTaskView{ID: item.ID, Domain: sohaapi.ComputeTaskDomainVirtualization, SourceType: "virtualization_task", SourceID: item.ID, ProviderKey: item.Provider, ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, Kind: item.TaskKind, Category: taskCategory(item.TaskKind), NormalizedStatus: status, RawStatus: item.Status, Resources: resources, RequestedBy: item.RequestedBy, Worker: item.ClaimedByWorkerID, AttemptCount: item.AttemptCount, Cancelable: cancelable, Retryable: retryable, AvailableActions: taskActions(cancelable, retryable), ErrorCode: state.FailureReason, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, Summary: state.FailureMessage}
}

func runtimeTaskView(item domaindocker.Operation, manage bool) sohaapi.ComputeTaskView {
	status := normalizeTaskStatus(item.Status)
	state := domaindocker.BuildOperationState(item, time.Now().UTC())
	resources := []sohaapi.ComputeResourceRef{}
	if item.HostID != "" {
		resources = append(resources, runtimeResourceRef(sohaapi.ComputeResourceKindRuntimeHost, item.HostID, item.HostID))
	}
	if item.ProjectID != "" {
		resources = append(resources, runtimeResourceRef(sohaapi.ComputeResourceKindProject, item.ProjectID, item.ProjectID))
	}
	if item.ServiceID != "" {
		resources = append(resources, runtimeResourceRef(sohaapi.ComputeResourceKindService, item.ServiceID, item.ServiceID))
	}
	cancelable, retryable := state.Cancelable && manage, state.Retryable && manage
	return sohaapi.ComputeTaskView{ID: item.ID, Domain: sohaapi.ComputeTaskDomainContainerRuntime, SourceType: "docker_operation", SourceID: item.ID, ProviderKey: "docker", ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, Kind: item.OperationKind, Category: taskCategory(item.OperationKind), NormalizedStatus: status, RawStatus: item.Status, Resources: resources, RequestedBy: item.RequestedBy, Worker: item.ClaimedByWorkerID, AttemptCount: item.AttemptCount, Cancelable: cancelable, Retryable: retryable, AvailableActions: taskActions(cancelable, retryable), ErrorCode: state.FailureReason, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, Summary: state.FailureMessage}
}

func appendIfTaskMatches(items []sohaapi.ComputeTaskView, item sohaapi.ComputeTaskView, filter TaskFilter) []sohaapi.ComputeTaskView {
	if filter.Status != "" && string(item.NormalizedStatus) != filter.Status {
		return items
	}
	if filter.Category != "" && !taskCategoryMatches(filter.Category, item.Category) {
		return items
	}
	if !taskResourceMatches(item.Resources, filter.ResourceKind, filter.ResourceID) {
		return items
	}
	return append(items, item)
}

func taskResourceMatches(resources []sohaapi.ComputeResourceRef, kind, id string) bool {
	kind, id = strings.TrimSpace(kind), strings.TrimSpace(id)
	if kind == "" && id == "" {
		return true
	}
	for _, resource := range resources {
		if (kind == "" || string(resource.Kind) == kind) && (id == "" || resource.ID == id) {
			return true
		}
	}
	return false
}

func taskCategoryMatches(filter string, category sohaapi.ComputeTaskCategory) bool {
	if filter == string(sohaapi.ComputeTaskCategoryOperation) {
		return category == sohaapi.ComputeTaskCategoryOperation || category == sohaapi.ComputeTaskCategoryLifecycle
	}
	return string(category) == filter
}

func normalizeTaskStatus(status string) sohaapi.ComputeTaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending":
		return sohaapi.ComputeTaskStatusQueued
	case "running", "in_progress":
		return sohaapi.ComputeTaskStatusRunning
	case "completed", "succeeded", "success":
		return sohaapi.ComputeTaskStatusSucceeded
	case "failed", "error":
		return sohaapi.ComputeTaskStatusFailed
	case "canceled", "cancelled":
		return sohaapi.ComputeTaskStatusCanceled
	case "callback_timeout", "timeout", "timed_out":
		return sohaapi.ComputeTaskStatusTimeout
	default:
		return sohaapi.ComputeTaskStatusUnknown
	}
}
func taskCategory(kind string) sohaapi.ComputeTaskCategory {
	kind = strings.ToLower(kind)
	switch {
	case strings.Contains(kind, "sync"):
		return sohaapi.ComputeTaskCategorySync
	case strings.Contains(kind, "build"), strings.Contains(kind, "provision"), strings.Contains(kind, "deploy"):
		return sohaapi.ComputeTaskCategoryBuild
	case strings.Contains(kind, "start"), strings.Contains(kind, "stop"), strings.Contains(kind, "action"), strings.Contains(kind, "create"), strings.Contains(kind, "delete"):
		return sohaapi.ComputeTaskCategoryLifecycle
	default:
		return sohaapi.ComputeTaskCategoryOperation
	}
}
func taskActions(cancelable, retryable bool) []sohaapi.ComputeTaskAction {
	out := []sohaapi.ComputeTaskAction{sohaapi.ComputeTaskActionLogs}
	if cancelable {
		out = append(out, sohaapi.ComputeTaskActionCancel)
	}
	if retryable {
		out = append(out, sohaapi.ComputeTaskActionRetry)
	}
	return out
}

func virtualizationTaskVisible(keys []string, kind string) bool {
	if strings.Contains(strings.ToLower(strings.TrimSpace(kind)), "sync") {
		return has(keys, appaccess.PermVirtualizationSyncView)
	}
	return has(keys, appaccess.PermVirtualizationOperationsView)
}
func addTaskSummary(summary *sohaapi.ComputeTaskSummary, status sohaapi.ComputeTaskStatus) {
	switch status {
	case sohaapi.ComputeTaskStatusQueued:
		summary.Queued++
	case sohaapi.ComputeTaskStatusRunning:
		summary.Running++
	case sohaapi.ComputeTaskStatusFailed, sohaapi.ComputeTaskStatusTimeout:
		summary.Failed++
	}
}

func connectionRef(item domainvirtualization.Connection) sohaapi.ComputeResourceRef {
	return virtualizationResourceRef(sohaapi.ComputeResourceKindConnection, item.ID, item.Name, item.Provider, item.ID)
}
func virtualizationResourceRef(kind sohaapi.ComputeResourceKind, id, name, provider, instance string) sohaapi.ComputeResourceRef {
	return sohaapi.ComputeResourceRef{Domain: sohaapi.ComputeDomainVirtualization, Kind: kind, ID: id, DisplayName: firstNonEmpty(name, id), ProviderKey: provider, ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderInstanceRef: instance, ProviderGeneration: generation, AccessMode: sohaapi.ComputeAccessModeDirect}
}
func runtimeHostRef(item domaindocker.Host) sohaapi.ComputeResourceRef {
	ref := runtimeResourceRef(sohaapi.ComputeResourceKindRuntimeHost, item.ID, item.Name)
	ref.ProviderInstanceRef = item.ID
	ref.AccessMode = runtimeAccessMode(item)
	return ref
}
func agentHostRef(item domaindocker.Host) sohaapi.ComputeResourceRef {
	return sohaapi.ComputeResourceRef{Domain: sohaapi.ComputeDomainAgent, Kind: sohaapi.ComputeResourceKindAgentHost, ID: item.AgentID, DisplayName: firstNonEmpty(item.Name, item.AgentID), ProviderKey: "docker", ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderInstanceRef: item.ID, ProviderGeneration: generation, AccessMode: sohaapi.ComputeAccessModeAgentProxy}
}
func runtimeResourceRef(kind sohaapi.ComputeResourceKind, id, name string) sohaapi.ComputeResourceRef {
	return sohaapi.ComputeResourceRef{Domain: sohaapi.ComputeDomainContainerRuntime, Kind: kind, ID: id, DisplayName: firstNonEmpty(name, id), ProviderKey: "docker", ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, AccessMode: sohaapi.ComputeAccessModeAgentProxy}
}
func relatedHostResources(host domaindocker.Host) []sohaapi.ComputeResourceRef {
	out := []sohaapi.ComputeResourceRef{}
	if host.VirtualizationConnectionID != "" {
		out = append(out, virtualizationResourceRef(sohaapi.ComputeResourceKindConnection, host.VirtualizationConnectionID, host.VirtualizationConnectionID, "", host.VirtualizationConnectionID))
	}
	if host.VMID != "" {
		out = append(out, virtualizationResourceRef(sohaapi.ComputeResourceKindVM, host.VMID, firstNonEmpty(host.VMName, host.VMID), "", host.VirtualizationConnectionID))
	}
	if host.AgentID != "" {
		out = append(out, agentHostRef(host))
	}
	return out
}

func connectionHealth(item domainvirtualization.Connection) sohaapi.ComputeHealthStatus {
	if !item.Enabled {
		return sohaapi.ComputeHealthStatusUnavailable
	}
	text := strings.ToLower(firstMapString(item.Health, "status", "state"))
	switch text {
	case "healthy", "ok", "ready", "available", "connected":
		return sohaapi.ComputeHealthStatusHealthy
	case "degraded", "warning":
		return sohaapi.ComputeHealthStatusDegraded
	case "unavailable", "offline", "failed", "error":
		return sohaapi.ComputeHealthStatusUnavailable
	}
	if item.LastSyncedAt == nil {
		return sohaapi.ComputeHealthStatusPending
	}
	return sohaapi.ComputeHealthStatusHealthy
}
func runtimeHostStatus(item domaindocker.Host) sohaapi.ComputeHealthStatus {
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "docker_ready", "online", "ready", "healthy", "running", "active":
		return sohaapi.ComputeHealthStatusHealthy
	case "provisioning", "vm_ready", "provisioned_waiting_agent", "agent_bootstrapping", "agent_registered", "pending":
		return sohaapi.ComputeHealthStatusPending
	case "degraded", "warning":
		return sohaapi.ComputeHealthStatusDegraded
	case "offline", "unavailable", "failed", "error", "agent_failed":
		return sohaapi.ComputeHealthStatusUnavailable
	default:
		return sohaapi.ComputeHealthStatusUnknown
	}
}
func runtimeAccessMode(item domaindocker.Host) sohaapi.ComputeAccessMode {
	if strings.TrimSpace(item.AgentID) != "" {
		return sohaapi.ComputeAccessModeAgentProxy
	}
	return sohaapi.ComputeAccessModeDirect
}
func normalizedVMState(item domainvirtualization.VM) string {
	status := strings.ToLower(firstNonEmpty(item.PowerState, item.Status))
	switch status {
	case "running", "active", "started":
		return "running"
	case "stopped", "halted", "shutdown":
		return "stopped"
	case "failed", "error", "unavailable":
		return "error"
	default:
		return "unknown"
	}
}

func builtinVirtualizationHealth(connections []domainvirtualization.Connection) []sohaapi.ComputeProviderHealth {
	out := make([]sohaapi.ComputeProviderHealth, 0, 2)
	for _, provider := range []string{"pve", "kubevirt"} {
		status := sohaapi.ComputeHealthStatusUnknown
		for _, item := range connections {
			if item.Provider != provider {
				continue
			}
			current := connectionHealth(item)
			if status == sohaapi.ComputeHealthStatusUnknown || current == sohaapi.ComputeHealthStatusUnavailable || current == sohaapi.ComputeHealthStatusDegraded {
				status = current
			}
		}
		out = append(out, providerHealth(sohaapi.ComputeProviderDomainVirtualization, provider, status))
	}
	return out
}
func providerHealth(domain sohaapi.ComputeProviderDomain, key string, status sohaapi.ComputeHealthStatus) sohaapi.ComputeProviderHealth {
	now := time.Now().UTC()
	return sohaapi.ComputeProviderHealth{Domain: domain, ProviderKey: key, Status: status, Generation: generation, CheckedAt: &now}
}
func attentionFor(code string, severity sohaapi.ComputeAttentionSeverity, summary string, resource sohaapi.ComputeResourceRef) sohaapi.ComputeAttention {
	return sohaapi.ComputeAttention{Code: code, Severity: severity, Summary: summary, Resources: []sohaapi.ComputeResourceRef{resource}}
}

func paginate[T any](items []T, cursor string, limit int) ([]T, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if offset >= len(items) {
		return []T{}, "", nil
	}
	end := min(offset+limit, len(items))
	next := ""
	if end < len(items) {
		next = encodeCursor(end)
	}
	return items[offset:end], next, nil
}

func paginateTasks(items []sohaapi.ComputeTaskView, cursor string, limit int) ([]sohaapi.ComputeTaskView, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	start := 0
	if strings.TrimSpace(cursor) != "" {
		createdAt, tie, err := decodeTaskCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		start = len(items)
		for index, item := range items {
			if item.CreatedAt.Before(createdAt) || (item.CreatedAt.Equal(createdAt) && taskCursorTie(item) < tie) {
				start = index
				break
			}
		}
	}
	if start >= len(items) {
		return []sohaapi.ComputeTaskView{}, "", nil
	}
	end := min(start+limit, len(items))
	next := ""
	if end < len(items) {
		next = encodeTaskCursor(items[end-1])
	}
	return items[start:end], next, nil
}

func taskCursorTie(item sohaapi.ComputeTaskView) string {
	return string(item.Domain) + "/" + item.SourceID
}

func encodeTaskCursor(item sohaapi.ComputeTaskView) string {
	raw := item.CreatedAt.UTC().Format(time.RFC3339Nano) + "\n" + taskCursorTie(item)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeTaskCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(cursor))
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: invalid task cursor", apperrors.ErrInvalidArgument)
	}
	parts := strings.SplitN(string(raw), "\n", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return time.Time{}, "", fmt.Errorf("%w: invalid task cursor", apperrors.ErrInvalidArgument)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("%w: invalid task cursor", apperrors.ErrInvalidArgument)
	}
	return createdAt.UTC(), parts[1], nil
}
func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}
func decodeCursor(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid cursor", apperrors.ErrInvalidArgument)
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%w: invalid cursor", apperrors.ErrInvalidArgument)
	}
	return offset, nil
}
func has(keys []string, key string) bool { return slices.Contains(keys, key) }
func hasAny(keys []string, wanted ...string) bool {
	for _, key := range wanted {
		if has(keys, key) {
			return true
		}
	}
	return false
}
func virtualizationDomainVisible(keys []string) bool {
	return hasAny(keys, appaccess.PermVirtualizationOverviewView, appaccess.PermVirtualizationVMsView, appaccess.PermVirtualizationClustersView, appaccess.PermVirtualizationImagesView, appaccess.PermVirtualizationFlavorsView, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView, appaccess.PermVirtualizationSyncManage)
}
func runtimeDomainVisible(keys []string) bool {
	return hasAny(keys, appaccess.PermDockerOverviewView, appaccess.PermDockerHostsView, appaccess.PermDockerProjectsView, appaccess.PermDockerServicesView, appaccess.PermDockerPortsView, appaccess.PermDockerTemplatesView, appaccess.PermDockerOperationsView)
}
func providerMatches(filter, provider string) bool {
	return filter == "" || provider == "" || strings.EqualFold(filter, provider)
}
func firstMapString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(values[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
