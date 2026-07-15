package compute

import (
	"context"
	"encoding/base64"
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
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxReadLimit = 1000
	generation   = int64(1)
)

type VirtualizationReader interface {
	GetConnection(context.Context, string) (domainvirtualization.Connection, error)
	ListConnections(context.Context, domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error)
	GetVM(context.Context, string) (domainvirtualization.VM, error)
	ListVMs(context.Context, domainvirtualization.VMFilter) ([]domainvirtualization.VM, error)
	GetImage(context.Context, string) (domainvirtualization.Image, error)
	GetFlavor(context.Context, string) (domainvirtualization.Flavor, error)
	GetTask(context.Context, string) (domainvirtualization.Task, error)
	ListTasks(context.Context, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error)
}

type RuntimeReader interface {
	GetHost(context.Context, string) (domaindocker.Host, error)
	ListHosts(context.Context, domaindocker.HostFilter) ([]domaindocker.Host, error)
	GetProject(context.Context, string) (domaindocker.Project, error)
	ListProjects(context.Context, domaindocker.ProjectFilter) ([]domaindocker.Project, error)
	GetService(context.Context, string) (domaindocker.Service, error)
	ListServices(context.Context, domaindocker.ServiceFilter) ([]domaindocker.Service, error)
	GetPortMapping(context.Context, string) (domaindocker.PortMapping, error)
	ListPortMappings(context.Context, domaindocker.PortMappingFilter) ([]domaindocker.PortMapping, error)
	GetTemplate(context.Context, string) (domaindocker.Template, error)
	GetOperation(context.Context, string) (domaindocker.Operation, error)
	ListOperations(context.Context, domaindocker.OperationFilter) ([]domaindocker.Operation, error)
}

type PluginReader interface {
	ListInstalled(context.Context) ([]domainplugin.InstalledPlugin, error)
}

type Service struct {
	virtualization        VirtualizationReader
	runtime               RuntimeReader
	plugins               PluginReader
	permissions           *appaccess.PermissionResolver
	virtualizationEnabled bool
	runtimeEnabled        bool
}

type Options struct{ VirtualizationEnabled, RuntimeEnabled bool }

func New(virtualization VirtualizationReader, runtime RuntimeReader, plugins PluginReader, permissions *appaccess.PermissionResolver, options Options) *Service {
	return &Service{virtualization: virtualization, runtime: runtime, plugins: plugins, permissions: permissions, virtualizationEnabled: options.VirtualizationEnabled, runtimeEnabled: options.RuntimeEnabled}
}

func (s *Service) Overview(ctx context.Context, principal domainidentity.Principal) (sohaapi.ComputeOverview, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeOverview{}, err
	}
	out := sohaapi.ComputeOverview{Attention: []sohaapi.ComputeAttention{}, ProviderHealth: []sohaapi.ComputeProviderHealth{}, Warnings: []sohaapi.ComputeWarning{}}
	authorized := false

	if s.virtualizationEnabled && virtualizationDomainVisible(keys) {
		authorized = true
		readConnections := hasAny(keys, appaccess.PermVirtualizationOverviewView, appaccess.PermVirtualizationClustersView)
		readVMs := hasAny(keys, appaccess.PermVirtualizationOverviewView, appaccess.PermVirtualizationVMsView)
		if readConnections || readVMs {
			section, health, attention, failed := s.virtualizationOverview(ctx, readConnections, readVMs)
			out.Virtualization = &section
			out.ProviderHealth = append(out.ProviderHealth, health...)
			out.Attention = append(out.Attention, attention...)
			out.Partial = out.Partial || failed
		} else {
			out.Warnings = append(out.Warnings, sohaapi.ComputeWarning{Code: "virtualization_summary_redacted", Message: "Virtualization summary is hidden by resource permissions"})
		}
	}

	runtimeVisible := s.runtimeEnabled && runtimeDomainVisible(keys)
	if runtimeVisible {
		authorized = true
	}
	hostsAllowed := runtimeVisible && hasAny(keys, appaccess.PermDockerOverviewView, appaccess.PermDockerHostsView)
	var hosts []domaindocker.Host
	var hostsErr error
	if hostsAllowed {
		hosts, hostsErr = s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
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

	if (s.virtualizationEnabled && hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView)) || (s.runtimeEnabled && has(keys, appaccess.PermDockerOperationsView)) {
		authorized = true
		section, failed := s.taskOverview(ctx, keys)
		out.Tasks = &section
		out.Partial = out.Partial || failed
	}
	if !authorized {
		return sohaapi.ComputeOverview{}, fmt.Errorf("%w: compute overview is not visible", apperrors.ErrAccessDenied)
	}
	return out, nil
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
	if s.virtualizationEnabled && hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView) {
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
	if s.runtimeEnabled && has(keys, appaccess.PermDockerOperationsView) {
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
	accessVisible := (s.virtualizationEnabled && has(keys, appaccess.PermVirtualizationClustersView)) || (s.runtimeEnabled && has(keys, appaccess.PermDockerHostsView))
	if !accessVisible {
		return sohaapi.ComputeAccessSourceListEnvelope{}, fmt.Errorf("%w: compute access sources are not visible", apperrors.ErrAccessDenied)
	}
	items := []sohaapi.ComputeAccessSource{}
	if s.virtualizationEnabled && (filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeVirtualizationConnection)) {
		if has(keys, appaccess.PermVirtualizationClustersView) {
			connections, readErr := s.virtualization.ListConnections(ctx, domainvirtualization.ConnectionFilter{Provider: filter.ProviderKey, Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeAccessSourceListEnvelope{}, readErr
			}
			manage := has(keys, appaccess.PermVirtualizationClustersManage)
			for _, item := range connections {
				items = append(items, connectionAccessSource(item, manage))
			}
		}
	}
	if s.runtimeEnabled && (filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeRuntimeHost) || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeAgentHost)) {
		if has(keys, appaccess.PermDockerHostsView) && providerMatches(filter.ProviderKey, "docker") {
			hosts, readErr := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeAccessSourceListEnvelope{}, readErr
			}
			manage := has(keys, appaccess.PermDockerHostsManage)
			for _, host := range hosts {
				if filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeRuntimeHost) {
					items = append(items, runtimeAccessSource(host, manage))
				}
				if strings.TrimSpace(host.AgentID) != "" && (filter.SourceType == "" || filter.SourceType == string(sohaapi.ComputeAccessSourceTypeAgentHost)) {
					items = append(items, agentAccessSource(host))
				}
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	page, next, err := paginate(items, filter.Cursor, filter.Limit)
	if err != nil {
		return sohaapi.ComputeAccessSourceListEnvelope{}, err
	}
	return sohaapi.ComputeAccessSourceListEnvelope{Items: page, NextCursor: next}, nil
}

type ProviderFilter struct {
	Domain, Source, Cursor string
	Limit                  int
}

func (s *Service) ListProviders(ctx context.Context, principal domainidentity.Principal, filter ProviderFilter) (sohaapi.ComputeProviderListEnvelope, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeProviderListEnvelope{}, err
	}
	virtualizationVisible := s.virtualizationEnabled && virtualizationDomainVisible(keys)
	runtimeVisible := s.runtimeEnabled && runtimeDomainVisible(keys)
	if !virtualizationVisible && !runtimeVisible {
		return sohaapi.ComputeProviderListEnvelope{}, fmt.Errorf("%w: compute providers are not visible", apperrors.ErrAccessDenied)
	}
	items := []sohaapi.ComputeProviderDescriptor{}
	if filter.Source == "" || filter.Source == string(sohaapi.ComputeProviderSourceBuiltin) {
		if virtualizationVisible && domainMatches(filter.Domain, string(sohaapi.ComputeProviderDomainVirtualization)) {
			items = append(items, builtinVirtualizationProvider("pve", "Proxmox VE"), builtinVirtualizationProvider("kubevirt", "KubeVirt"))
		}
		if runtimeVisible && domainMatches(filter.Domain, string(sohaapi.ComputeProviderDomainContainerRuntime)) {
			items = append(items, builtinRuntimeProvider())
		}
	}
	if s.plugins != nil && (filter.Source == "" || filter.Source == string(sohaapi.ComputeProviderSourcePlugin)) {
		installed, readErr := s.plugins.ListInstalled(ctx)
		if readErr != nil {
			return sohaapi.ComputeProviderListEnvelope{}, readErr
		}
		items = append(items, pluginProviderDescriptors(installed, filter.Domain, virtualizationVisible, runtimeVisible)...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Domain != items[j].Domain {
			return items[i].Domain < items[j].Domain
		}
		return items[i].ProviderKey < items[j].ProviderKey
	})
	page, next, err := paginate(items, filter.Cursor, filter.Limit)
	if err != nil {
		return sohaapi.ComputeProviderListEnvelope{}, err
	}
	return sohaapi.ComputeProviderListEnvelope{Items: page, NextCursor: next}, nil
}

type TaskFilter struct {
	Domain, ProviderKey, Status, Category, Cursor string
	Limit                                         int
}

func (s *Service) ListTasks(ctx context.Context, principal domainidentity.Principal, filter TaskFilter) (sohaapi.ComputeTaskListEnvelope, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeTaskListEnvelope{}, err
	}
	items := []sohaapi.ComputeTaskView{}
	virtualizationVisible := s.virtualizationEnabled && hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView)
	runtimeVisible := s.runtimeEnabled && has(keys, appaccess.PermDockerOperationsView)
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
				items = appendIfTaskMatches(items, virtualizationTaskView(task), filter)
			}
		}
	}
	if runtimeVisible && (filter.Domain == "" || filter.Domain == string(sohaapi.ComputeTaskDomainContainerRuntime)) && providerMatches(filter.ProviderKey, "docker") {
		tasks, readErr := s.runtime.ListOperations(ctx, domaindocker.OperationFilter{Limit: maxReadLimit})
		if readErr != nil {
			return sohaapi.ComputeTaskListEnvelope{}, readErr
		}
		for _, task := range tasks {
			items = appendIfTaskMatches(items, runtimeTaskView(task), filter)
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

func (s *Service) GetTask(ctx context.Context, principal domainidentity.Principal, domain, id string) (sohaapi.ComputeTaskView, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	switch strings.TrimSpace(domain) {
	case string(sohaapi.ComputeTaskDomainVirtualization):
		if !s.virtualizationEnabled {
			return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: virtualization module is disabled", apperrors.ErrNotFound)
		}
		if !hasAny(keys, appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationSyncView) {
			return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: virtualization tasks are not visible", apperrors.ErrAccessDenied)
		}
		item, readErr := s.virtualization.GetTask(ctx, id)
		if readErr != nil {
			return sohaapi.ComputeTaskView{}, readErr
		}
		if !virtualizationTaskVisible(keys, item.TaskKind) {
			return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: virtualization task is not visible", apperrors.ErrAccessDenied)
		}
		return virtualizationTaskView(item), nil
	case string(sohaapi.ComputeTaskDomainContainerRuntime):
		if !s.runtimeEnabled {
			return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: container runtime module is disabled", apperrors.ErrNotFound)
		}
		if !has(keys, appaccess.PermDockerOperationsView) {
			return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: runtime tasks are not visible", apperrors.ErrAccessDenied)
		}
		item, readErr := s.runtime.GetOperation(ctx, id)
		if readErr != nil {
			return sohaapi.ComputeTaskView{}, readErr
		}
		return runtimeTaskView(item), nil
	default:
		return sohaapi.ComputeTaskView{}, fmt.Errorf("%w: invalid compute task domain", apperrors.ErrInvalidArgument)
	}
}

func (s *Service) ListRelations(ctx context.Context, principal domainidentity.Principal, domain, kind, id, cursor string, limit int) (sohaapi.ComputeResourceRelations, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeResourceRelations{}, err
	}
	domain, kind, id = strings.TrimSpace(domain), strings.TrimSpace(kind), strings.TrimSpace(id)
	if id == "" {
		return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: compute resource id is required", apperrors.ErrInvalidArgument)
	}
	var result sohaapi.ComputeResourceRelations
	if domain == string(sohaapi.ComputeDomainVirtualization) {
		if !s.virtualizationEnabled {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: virtualization module is disabled", apperrors.ErrNotFound)
		}
		result, err = s.virtualizationRelations(ctx, keys, kind, id)
	} else if domain == string(sohaapi.ComputeDomainContainerRuntime) || domain == string(sohaapi.ComputeDomainAgent) {
		if !s.runtimeEnabled {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: container runtime module is disabled", apperrors.ErrNotFound)
		}
		result, err = s.runtimeRelations(ctx, keys, kind, id)
	} else {
		return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: invalid compute resource domain", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return sohaapi.ComputeResourceRelations{}, err
	}
	result.Relations, result.NextCursor, err = paginate(result.Relations, cursor, limit)
	return result, err
}

func (s *Service) virtualizationRelations(ctx context.Context, keys []string, kind, id string) (sohaapi.ComputeResourceRelations, error) {
	if kind == string(sohaapi.ComputeResourceKindConnection) {
		if !has(keys, appaccess.PermVirtualizationClustersView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: virtualization connections are not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.virtualization.GetConnection(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		resource := connectionRef(item)
		relations := []sohaapi.ComputeResourceRelation{}
		if has(keys, appaccess.PermVirtualizationVMsView) {
			vms, readErr := s.virtualization.ListVMs(ctx, domainvirtualization.VMFilter{ConnectionID: id, Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeResourceRelations{}, readErr
			}
			for _, vm := range vms {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeContains, vmRef(vm), maxTime(item.UpdatedAt, vm.UpdatedAt)))
			}
		}
		if has(keys, appaccess.PermDockerHostsView) {
			hosts, readErr := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeResourceRelations{}, readErr
			}
			for _, host := range hosts {
				if host.VirtualizationConnectionID == id {
					relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeProvisions, runtimeHostRef(host), maxTime(item.UpdatedAt, host.UpdatedAt)))
				}
			}
		}
		return sohaapi.ComputeResourceRelations{Resource: resource, Relations: relations}, nil
	}
	if kind == string(sohaapi.ComputeResourceKindVM) {
		if !has(keys, appaccess.PermVirtualizationVMsView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: virtual machines are not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.virtualization.GetVM(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		resource := vmRef(item)
		relations := []sohaapi.ComputeResourceRelation{}
		if has(keys, appaccess.PermVirtualizationClustersView) && item.ConnectionID != "" {
			if connection, readErr := s.virtualization.GetConnection(ctx, item.ConnectionID); readErr == nil {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeConnectedBy, connectionRef(connection), maxTime(item.UpdatedAt, connection.UpdatedAt)))
			}
		}
		if has(keys, appaccess.PermDockerHostsView) {
			hosts, readErr := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeResourceRelations{}, readErr
			}
			for _, host := range hosts {
				if host.VMID == id {
					relations = append(relations, relation(runtimeHostRef(host), sohaapi.ComputeRelationTypeRunsOn, resource, maxTime(item.UpdatedAt, host.UpdatedAt)))
				}
			}
		}
		return sohaapi.ComputeResourceRelations{Resource: resource, Relations: relations}, nil
	}
	if kind == string(sohaapi.ComputeResourceKindImage) && has(keys, appaccess.PermVirtualizationImagesView) {
		item, err := s.virtualization.GetImage(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		return emptyRelations(virtualizationResourceRef(sohaapi.ComputeResourceKindImage, item.ID, item.Name, item.Provider, item.ConnectionID)), nil
	}
	if kind == string(sohaapi.ComputeResourceKindFlavor) && has(keys, appaccess.PermVirtualizationFlavorsView) {
		item, err := s.virtualization.GetFlavor(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		return emptyRelations(virtualizationResourceRef(sohaapi.ComputeResourceKindFlavor, item.ID, item.Name, item.Provider, item.ConnectionID)), nil
	}
	return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: compute resource is not visible or unsupported", apperrors.ErrNotFound)
}

func (s *Service) runtimeRelations(ctx context.Context, keys []string, kind, id string) (sohaapi.ComputeResourceRelations, error) {
	switch kind {
	case string(sohaapi.ComputeResourceKindRuntimeHost):
		if !has(keys, appaccess.PermDockerHostsView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: runtime hosts are not visible", apperrors.ErrAccessDenied)
		}
		host, err := s.runtime.GetHost(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		resource := runtimeHostRef(host)
		relations := []sohaapi.ComputeResourceRelation{}
		if host.VMID != "" && has(keys, appaccess.PermVirtualizationVMsView) {
			if vm, readErr := s.virtualization.GetVM(ctx, host.VMID); readErr == nil {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeRunsOn, vmRef(vm), maxTime(host.UpdatedAt, vm.UpdatedAt)))
			}
		}
		if has(keys, appaccess.PermDockerProjectsView) {
			projects, readErr := s.runtime.ListProjects(ctx, domaindocker.ProjectFilter{HostID: id, Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeResourceRelations{}, readErr
			}
			for _, project := range projects {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeContains, projectRef(project), maxTime(host.UpdatedAt, project.UpdatedAt)))
			}
		}
		return sohaapi.ComputeResourceRelations{Resource: resource, Relations: relations}, nil
	case string(sohaapi.ComputeResourceKindAgentHost):
		if !has(keys, appaccess.PermDockerHostsView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: agent hosts are not visible", apperrors.ErrAccessDenied)
		}
		hosts, err := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		for _, host := range hosts {
			if host.AgentID == id {
				resource := agentHostRef(host)
				return sohaapi.ComputeResourceRelations{Resource: resource, Relations: []sohaapi.ComputeResourceRelation{relation(resource, sohaapi.ComputeRelationTypeManages, runtimeHostRef(host), host.UpdatedAt)}}, nil
			}
		}
		return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: agent host not found", apperrors.ErrNotFound)
	case string(sohaapi.ComputeResourceKindProject):
		if !has(keys, appaccess.PermDockerProjectsView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: runtime projects are not visible", apperrors.ErrAccessDenied)
		}
		project, err := s.runtime.GetProject(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		resource := projectRef(project)
		relations := []sohaapi.ComputeResourceRelation{}
		if has(keys, appaccess.PermDockerHostsView) {
			if host, readErr := s.runtime.GetHost(ctx, project.HostID); readErr == nil {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeRunsOn, runtimeHostRef(host), maxTime(project.UpdatedAt, host.UpdatedAt)))
			}
		}
		if has(keys, appaccess.PermDockerServicesView) {
			services, readErr := s.runtime.ListServices(ctx, domaindocker.ServiceFilter{ProjectID: id, Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeResourceRelations{}, readErr
			}
			for _, service := range services {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeContains, serviceRef(service), maxTime(project.UpdatedAt, service.UpdatedAt)))
			}
		}
		return sohaapi.ComputeResourceRelations{Resource: resource, Relations: relations}, nil
	case string(sohaapi.ComputeResourceKindService), string(sohaapi.ComputeResourceKindContainer):
		if !has(keys, appaccess.PermDockerServicesView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: runtime services are not visible", apperrors.ErrAccessDenied)
		}
		service, err := s.runtime.GetService(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		resource := serviceRef(service)
		relations := []sohaapi.ComputeResourceRelation{}
		if has(keys, appaccess.PermDockerProjectsView) {
			if project, readErr := s.runtime.GetProject(ctx, service.ProjectID); readErr == nil {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeRunsOn, projectRef(project), maxTime(service.UpdatedAt, project.UpdatedAt)))
			}
		}
		if has(keys, appaccess.PermDockerPortsView) {
			ports, readErr := s.runtime.ListPortMappings(ctx, domaindocker.PortMappingFilter{ServiceID: id, Limit: maxReadLimit})
			if readErr != nil {
				return sohaapi.ComputeResourceRelations{}, readErr
			}
			for _, port := range ports {
				relations = append(relations, relation(resource, sohaapi.ComputeRelationTypeExposes, portRef(port), maxTime(service.UpdatedAt, port.UpdatedAt)))
			}
		}
		return sohaapi.ComputeResourceRelations{Resource: resource, Relations: relations}, nil
	case string(sohaapi.ComputeResourceKindPort):
		if !has(keys, appaccess.PermDockerPortsView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: runtime ports are not visible", apperrors.ErrAccessDenied)
		}
		port, err := s.runtime.GetPortMapping(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		resource := portRef(port)
		relations := []sohaapi.ComputeResourceRelation{}
		if port.ServiceID != "" && has(keys, appaccess.PermDockerServicesView) {
			if service, readErr := s.runtime.GetService(ctx, port.ServiceID); readErr == nil {
				relations = append(relations, relation(serviceRef(service), sohaapi.ComputeRelationTypeExposes, resource, maxTime(port.UpdatedAt, service.UpdatedAt)))
			}
		}
		return sohaapi.ComputeResourceRelations{Resource: resource, Relations: relations}, nil
	case string(sohaapi.ComputeResourceKindTemplate):
		if !has(keys, appaccess.PermDockerTemplatesView) {
			return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: runtime templates are not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.runtime.GetTemplate(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRelations{}, err
		}
		return emptyRelations(runtimeResourceRef(sohaapi.ComputeResourceKindTemplate, item.ID, item.Name)), nil
	default:
		return sohaapi.ComputeResourceRelations{}, fmt.Errorf("%w: compute resource kind is unsupported", apperrors.ErrNotFound)
	}
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

func builtinVirtualizationProvider(key, name string) sohaapi.ComputeProviderDescriptor {
	kinds := []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindConnection, sohaapi.ComputeResourceKindVM, sohaapi.ComputeResourceKindImage, sohaapi.ComputeResourceKindFlavor}
	return builtinProvider(key, name, sohaapi.ComputeProviderDomainVirtualization, kinds, []sohaapi.ComputeProviderCapability{
		capability("connection.read", sohaapi.ComputeProviderActivationLevelRead, []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindConnection}),
		capability("resource.read", sohaapi.ComputeProviderActivationLevelRead, kinds),
		capability("lifecycle.write", sohaapi.ComputeProviderActivationLevelWrite, []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindVM}),
	})
}

func builtinRuntimeProvider() sohaapi.ComputeProviderDescriptor {
	kinds := []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindRuntimeHost, sohaapi.ComputeResourceKindProject, sohaapi.ComputeResourceKindContainer, sohaapi.ComputeResourceKindService, sohaapi.ComputeResourceKindPort, sohaapi.ComputeResourceKindTemplate}
	return builtinProvider("docker", "Docker Engine", sohaapi.ComputeProviderDomainContainerRuntime, kinds, []sohaapi.ComputeProviderCapability{
		capability("runtime.read", sohaapi.ComputeProviderActivationLevelRead, kinds), capability("runtime.write", sohaapi.ComputeProviderActivationLevelWrite, kinds),
	})
}

func builtinProvider(key, name string, domain sohaapi.ComputeProviderDomain, kinds []sohaapi.ComputeResourceKind, capabilities []sohaapi.ComputeProviderCapability) sohaapi.ComputeProviderDescriptor {
	return sohaapi.ComputeProviderDescriptor{ProviderKey: key, Domain: domain, DisplayName: name, Version: "builtin", Source: sohaapi.ComputeProviderSourceBuiltin, ContractVersion: "v1", ActivationLevel: sohaapi.ComputeProviderActivationLevelWrite, ResourceKinds: kinds, Capabilities: capabilities, RuntimeMode: sohaapi.ComputePluginRuntimeModeBuiltin, Generation: generation, Health: providerHealth(domain, key, sohaapi.ComputeHealthStatusUnknown), ResourceSchemas: []sohaapi.ComputeProviderResourceSchema{}, StatusMappings: []sohaapi.ComputeProviderStatusMapping{}}
}

func capability(id string, level sohaapi.ComputeProviderActivationLevel, kinds []sohaapi.ComputeResourceKind) sohaapi.ComputeProviderCapability {
	return sohaapi.ComputeProviderCapability{ID: id, Level: level, ResourceKinds: kinds, Enabled: true}
}

func pluginProviderDescriptors(installed []domainplugin.InstalledPlugin, domainFilter string, virtualizationVisible, runtimeVisible bool) []sohaapi.ComputeProviderDescriptor {
	out := []sohaapi.ComputeProviderDescriptor{}
	for _, plugin := range installed {
		if plugin.Status != "enabled" || plugin.Manifest.ExtensionPoints == nil || plugin.Manifest.ExtensionPoints.Compute == nil {
			continue
		}
		compute := plugin.Manifest.ExtensionPoints.Compute
		if virtualizationVisible && domainMatches(domainFilter, string(sohaapi.ComputeProviderDomainVirtualization)) {
			for _, provider := range compute.VirtualizationProviders {
				out = append(out, pluginProvider(plugin, provider.ProviderKey, provider.DisplayName, sohaapi.ComputeProviderDomainVirtualization, provider.ActivationLevel, stringsToKinds(provider.ResourceKinds), provider.Capabilities))
			}
		}
		if runtimeVisible && domainMatches(domainFilter, string(sohaapi.ComputeProviderDomainContainerRuntime)) {
			for _, provider := range compute.ContainerRuntimeProviders {
				out = append(out, pluginProvider(plugin, provider.ProviderKey, provider.DisplayName, sohaapi.ComputeProviderDomainContainerRuntime, provider.ActivationLevel, stringsToKinds(provider.ResourceKinds), provider.Capabilities))
			}
		}
	}
	return out
}

func pluginProvider(plugin domainplugin.InstalledPlugin, key, name string, domain sohaapi.ComputeProviderDomain, level sohaapi.ComputeProviderActivationLevel, kinds []sohaapi.ComputeResourceKind, capabilityIDs []string) sohaapi.ComputeProviderDescriptor {
	capabilities := make([]sohaapi.ComputeProviderCapability, 0, len(capabilityIDs))
	for _, id := range capabilityIDs {
		capabilities = append(capabilities, sohaapi.ComputeProviderCapability{ID: id, Level: level, ResourceKinds: kinds, Enabled: false, Reason: "provider invocation is not enabled"})
	}
	gen := plugin.UpdatedAt.Unix()
	if gen < 1 {
		gen = generation
	}
	return sohaapi.ComputeProviderDescriptor{ProviderKey: key, Domain: domain, DisplayName: name, Version: plugin.Version, Source: sohaapi.ComputeProviderSourcePlugin, PluginID: plugin.ID, PluginVersion: plugin.Version, ContractVersion: "v1", ActivationLevel: sohaapi.ComputeProviderActivationLevelDescriptor, ResourceKinds: kinds, Capabilities: capabilities, RuntimeMode: pluginRuntimeMode(plugin), Generation: gen, Health: sohaapi.ComputeProviderHealth{Domain: domain, ProviderKey: key, Status: sohaapi.ComputeHealthStatusPending, Generation: gen, Code: "descriptor_only", Message: "Provider invocation is not enabled", CheckedAt: &plugin.UpdatedAt}, ResourceSchemas: []sohaapi.ComputeProviderResourceSchema{}, StatusMappings: []sohaapi.ComputeProviderStatusMapping{}}
}

func pluginRuntimeMode(plugin domainplugin.InstalledPlugin) sohaapi.ComputePluginRuntimeMode {
	if plugin.Manifest.Runtime == nil {
		return sohaapi.ComputePluginRuntimeModeManifestOnly
	}
	switch string(plugin.Manifest.Runtime.Mode) {
	case "external-http":
		return sohaapi.ComputePluginRuntimeModeExternal
	case "managed-container":
		return sohaapi.ComputePluginRuntimeModeManaged
	default:
		return sohaapi.ComputePluginRuntimeModeManifestOnly
	}
}

func stringsToKinds[T ~string](values []T) []sohaapi.ComputeResourceKind {
	out := make([]sohaapi.ComputeResourceKind, 0, len(values))
	for _, value := range values {
		kind := sohaapi.ComputeResourceKind(value)
		if kind.Valid() {
			out = append(out, kind)
		}
	}
	return out
}

func virtualizationTaskView(item domainvirtualization.Task) sohaapi.ComputeTaskView {
	status := normalizeTaskStatus(item.Status)
	state := domainvirtualization.BuildOperationState(item, time.Now().UTC())
	resources := []sohaapi.ComputeResourceRef{}
	if item.ConnectionID != "" {
		resources = append(resources, virtualizationResourceRef(sohaapi.ComputeResourceKindConnection, item.ConnectionID, item.ConnectionID, item.Provider, item.ConnectionID))
	}
	if item.VMID != "" {
		resources = append(resources, virtualizationResourceRef(sohaapi.ComputeResourceKindVM, item.VMID, item.VMID, item.Provider, item.ConnectionID))
	}
	return sohaapi.ComputeTaskView{ID: item.ID, Domain: sohaapi.ComputeTaskDomainVirtualization, SourceType: "virtualization_task", SourceID: item.ID, ProviderKey: item.Provider, ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, Kind: item.TaskKind, Category: taskCategory(item.TaskKind), NormalizedStatus: status, RawStatus: item.Status, Resources: resources, RequestedBy: item.RequestedBy, Worker: item.ClaimedByWorkerID, AttemptCount: item.AttemptCount, Cancelable: state.Cancelable, Retryable: state.Retryable, AvailableActions: taskActions(state.Cancelable, state.Retryable), ErrorCode: state.FailureReason, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, Summary: state.FailureMessage}
}

func runtimeTaskView(item domaindocker.Operation) sohaapi.ComputeTaskView {
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
	return sohaapi.ComputeTaskView{ID: item.ID, Domain: sohaapi.ComputeTaskDomainContainerRuntime, SourceType: "docker_operation", SourceID: item.ID, ProviderKey: "docker", ProviderSource: sohaapi.ComputeProviderSourceBuiltin, ProviderGeneration: generation, Kind: item.OperationKind, Category: taskCategory(item.OperationKind), NormalizedStatus: status, RawStatus: item.Status, Resources: resources, RequestedBy: item.RequestedBy, Worker: item.ClaimedByWorkerID, AttemptCount: item.AttemptCount, Cancelable: state.Cancelable, Retryable: state.Retryable, AvailableActions: taskActions(state.Cancelable, state.Retryable), ErrorCode: state.FailureReason, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt, Summary: state.FailureMessage}
}

func appendIfTaskMatches(items []sohaapi.ComputeTaskView, item sohaapi.ComputeTaskView, filter TaskFilter) []sohaapi.ComputeTaskView {
	if filter.Status != "" && string(item.NormalizedStatus) != filter.Status {
		return items
	}
	if filter.Category != "" && !taskCategoryMatches(filter.Category, item.Category) {
		return items
	}
	return append(items, item)
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
func vmRef(item domainvirtualization.VM) sohaapi.ComputeResourceRef {
	return virtualizationResourceRef(sohaapi.ComputeResourceKindVM, item.ID, item.Name, item.Provider, item.ConnectionID)
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
func projectRef(item domaindocker.Project) sohaapi.ComputeResourceRef {
	ref := runtimeResourceRef(sohaapi.ComputeResourceKindProject, item.ID, item.Name)
	ref.ProviderInstanceRef = item.HostID
	return ref
}
func serviceRef(item domaindocker.Service) sohaapi.ComputeResourceRef {
	ref := runtimeResourceRef(sohaapi.ComputeResourceKindService, item.ID, item.Name)
	ref.ProviderInstanceRef = item.HostID
	return ref
}
func portRef(item domaindocker.PortMapping) sohaapi.ComputeResourceRef {
	return runtimeResourceRef(sohaapi.ComputeResourceKindPort, item.ID, firstNonEmpty(item.Name, strconv.Itoa(item.HostPort)))
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

func relation(from sohaapi.ComputeResourceRef, relationType sohaapi.ComputeRelationType, to sohaapi.ComputeResourceRef, observed time.Time) sohaapi.ComputeResourceRelation {
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	return sohaapi.ComputeResourceRelation{From: from, Type: relationType, To: to, Source: sohaapi.ComputeRelationSourceDerived, ObservedAt: observed.UTC(), ProviderGeneration: generation, Metadata: []sohaapi.ComputeMetadataEntry{}}
}
func emptyRelations(resource sohaapi.ComputeResourceRef) sohaapi.ComputeResourceRelations {
	return sohaapi.ComputeResourceRelations{Resource: resource, Relations: []sohaapi.ComputeResourceRelation{}}
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
func domainMatches(filter, domain string) bool { return filter == "" || filter == domain }
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
func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
