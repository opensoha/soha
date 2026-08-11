package compute

import (
	"context"
	"fmt"
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

const computeContractVersion = "0.1.0"

type ProviderFilter struct {
	Domain, Source, Cursor string
	Limit                  int
}

type ProviderInstanceFilter struct {
	Domain, ProviderKey, Cursor string
	Limit                       int
}

type VirtualizationActionInput struct {
	Action, IdempotencyKey  string
	CPU, MemoryMiB, DiskGiB int
	Reason                  string
}

func (s *Service) Capabilities(ctx context.Context, principal domainidentity.Principal) (sohaapi.ComputeCapabilityManifest, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeCapabilityManifest{}, err
	}
	read := (s.virtualizationAvailable() && virtualizationDomainVisible(keys)) || (s.runtimeAvailable() && runtimeDomainVisible(keys))
	write := (s.virtualizationAvailable() && virtualizationWriteVisible(keys)) || (s.runtimeAvailable() && runtimeWriteVisible(keys))
	taskActions := hasAny(keys,
		appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "cancel"),
		appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "retry"),
		appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "cancel"),
		appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "retry"),
	)
	if !read && !write && !taskActions {
		return sohaapi.ComputeCapabilityManifest{}, fmt.Errorf("%w: compute capabilities are not visible", apperrors.ErrAccessDenied)
	}
	return sohaapi.ComputeCapabilityManifest{Generation: generation, Features: []sohaapi.ComputeFeatureCapability{
		featureCapability(sohaapi.ComputeFeatureWorkbench, read, sohaapi.ComputeProviderActivationLevelRead, "compute resources are not visible"),
		featureCapability(sohaapi.ComputeFeatureProviderRead, read, sohaapi.ComputeProviderActivationLevelRead, "provider reads are not visible"),
		featureCapability(sohaapi.ComputeFeatureProviderWrite, write, sohaapi.ComputeProviderActivationLevelWrite, "provider writes are not permitted"),
		featureCapability(sohaapi.ComputeFeatureRelations, read, sohaapi.ComputeProviderActivationLevelRead, "resource relations are not visible"),
		featureCapability(sohaapi.ComputeFeatureTaskActions, taskActions, sohaapi.ComputeProviderActivationLevelWrite, "task actions are not permitted"),
		featureCapability(sohaapi.ComputeFeaturePluginProviders, false, sohaapi.ComputeProviderActivationLevelDescriptor, "plugin providers are not configured"),
	}}, nil
}

func featureCapability(id sohaapi.ComputeFeatureID, enabled bool, level sohaapi.ComputeProviderActivationLevel, reason string) sohaapi.ComputeFeatureCapability {
	item := sohaapi.ComputeFeatureCapability{ID: id, Enabled: enabled, RolloutStage: sohaapi.ComputeRolloutStageDefault, MaxActivationLevel: level}
	if !enabled {
		item.Reason = reason
	}
	return item
}

func (s *Service) ListProviders(ctx context.Context, principal domainidentity.Principal, filter ProviderFilter) (sohaapi.ComputeProviderListEnvelope, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeProviderListEnvelope{}, err
	}
	if !providerDomainVisible(s, keys) {
		return sohaapi.ComputeProviderListEnvelope{}, fmt.Errorf("%w: compute providers are not visible", apperrors.ErrAccessDenied)
	}
	if filter.Domain != "" && !sohaapi.ComputeProviderDomain(filter.Domain).Valid() {
		return sohaapi.ComputeProviderListEnvelope{}, invalidProviderInput("domain")
	}
	if filter.Source != "" && !sohaapi.ComputeProviderSource(filter.Source).Valid() {
		return sohaapi.ComputeProviderListEnvelope{}, invalidProviderInput("source")
	}
	items := []sohaapi.ComputeProviderDescriptor{}
	if filter.Source == "" || filter.Source == string(sohaapi.ComputeProviderSourceBuiltin) {
		items, err = s.builtinProviders(ctx, keys, filter.Domain)
		if err != nil {
			return sohaapi.ComputeProviderListEnvelope{}, err
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Domain == items[j].Domain {
			return items[i].ProviderKey < items[j].ProviderKey
		}
		return items[i].Domain < items[j].Domain
	})
	page, next, err := paginate(items, filter.Cursor, filter.Limit)
	return sohaapi.ComputeProviderListEnvelope{Items: page, NextCursor: next}, err
}

func (s *Service) builtinProviders(ctx context.Context, keys []string, domain string) ([]sohaapi.ComputeProviderDescriptor, error) {
	items := []sohaapi.ComputeProviderDescriptor{}
	if s.virtualizationAvailable() && virtualizationDomainVisible(keys) && (domain == "" || domain == string(sohaapi.ComputeProviderDomainVirtualization)) {
		connections := []domainvirtualization.Connection{}
		if has(keys, appaccess.PermVirtualizationClustersView) {
			var err error
			connections, err = s.virtualization.ListConnections(ctx, domainvirtualization.ConnectionFilter{Limit: maxReadLimit})
			if err != nil {
				return nil, err
			}
		}
		for _, provider := range []string{"pve", "kubevirt"} {
			items = append(items, virtualizationProviderDescriptor(provider, keys, providerHealthFromConnections(provider, connections)))
		}
	}
	if s.runtimeAvailable() && runtimeDomainVisible(keys) && (domain == "" || domain == string(sohaapi.ComputeProviderDomainContainerRuntime)) {
		health := sohaapi.ComputeHealthStatusUnknown
		if has(keys, appaccess.PermDockerHostsView) {
			hosts, err := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
			if err != nil {
				return nil, err
			}
			health = aggregateRuntimeHealth(hosts)
		}
		items = append(items, runtimeProviderDescriptor(keys, health))
	}
	return items, nil
}

func virtualizationProviderDescriptor(provider string, keys []string, health sohaapi.ComputeHealthStatus) sohaapi.ComputeProviderDescriptor {
	kinds := []sohaapi.ComputeResourceKind{}
	if has(keys, appaccess.PermVirtualizationClustersView) {
		kinds = append(kinds, sohaapi.ComputeResourceKindConnection)
	}
	if has(keys, appaccess.PermVirtualizationVMsView) {
		kinds = append(kinds, sohaapi.ComputeResourceKindVM)
	}
	if has(keys, appaccess.PermVirtualizationImagesView) {
		kinds = append(kinds, sohaapi.ComputeResourceKindImage)
	}
	if has(keys, appaccess.PermVirtualizationFlavorsView) {
		kinds = append(kinds, sohaapi.ComputeResourceKindFlavor)
	}
	write := virtualizationWriteVisible(keys)
	capabilities := []sohaapi.ComputeProviderCapability{
		providerCapability("read", sohaapi.ComputeProviderActivationLevelRead, kinds, len(kinds) > 0, "resource reads are not permitted"),
		providerCapability("health_check", sohaapi.ComputeProviderActivationLevelWrite, []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindConnection}, has(keys, appaccess.ManagedActionPermission(appaccess.PermVirtualizationClustersManage, "test")), "connection tests are not permitted"),
		providerCapability("discovery", sohaapi.ComputeProviderActivationLevelWrite, []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindConnection}, has(keys, appaccess.ManagedActionPermission(appaccess.PermVirtualizationSyncManage, "sync")), "connection sync is not permitted"),
		providerCapability("vm_action", sohaapi.ComputeProviderActivationLevelWrite, []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindVM}, hasAny(keys, appaccess.PermVirtualizationVMsPower, appaccess.PermVirtualizationVMsResize, appaccess.PermVirtualizationVMsDelete), "virtual machine actions are not permitted"),
	}
	level := sohaapi.ComputeProviderActivationLevelRead
	if write {
		level = sohaapi.ComputeProviderActivationLevelWrite
	} else if len(kinds) == 0 {
		level = sohaapi.ComputeProviderActivationLevelDescriptor
	}
	display := strings.ToUpper(provider)
	if provider == "kubevirt" {
		display = "KubeVirt"
	}
	return sohaapi.ComputeProviderDescriptor{ProviderKey: provider, Domain: sohaapi.ComputeProviderDomainVirtualization, DisplayName: display, Version: "builtin", Source: sohaapi.ComputeProviderSourceBuiltin, ContractVersion: computeContractVersion, ActivationLevel: level, ResourceKinds: kinds, Capabilities: capabilities, RuntimeMode: sohaapi.ComputePluginRuntimeModeBuiltin, Generation: generation, Health: providerHealth(sohaapi.ComputeProviderDomainVirtualization, provider, health)}
}

func runtimeProviderDescriptor(keys []string, health sohaapi.ComputeHealthStatus) sohaapi.ComputeProviderDescriptor {
	kinds := []sohaapi.ComputeResourceKind{}
	for _, item := range []struct {
		permission string
		kind       sohaapi.ComputeResourceKind
	}{{appaccess.PermDockerHostsView, sohaapi.ComputeResourceKindRuntimeHost}, {appaccess.PermDockerProjectsView, sohaapi.ComputeResourceKindProject}, {appaccess.PermDockerServicesView, sohaapi.ComputeResourceKindService}, {appaccess.PermDockerPortsView, sohaapi.ComputeResourceKindPort}} {
		if has(keys, item.permission) {
			kinds = append(kinds, item.kind)
		}
	}
	write := runtimeWriteVisible(keys)
	level := sohaapi.ComputeProviderActivationLevelRead
	if write {
		level = sohaapi.ComputeProviderActivationLevelWrite
	} else if len(kinds) == 0 {
		level = sohaapi.ComputeProviderActivationLevelDescriptor
	}
	return sohaapi.ComputeProviderDescriptor{ProviderKey: "docker", Domain: sohaapi.ComputeProviderDomainContainerRuntime, DisplayName: "Docker", Version: "builtin", Source: sohaapi.ComputeProviderSourceBuiltin, ContractVersion: computeContractVersion, ActivationLevel: level, ResourceKinds: kinds, Capabilities: []sohaapi.ComputeProviderCapability{
		providerCapability("read", sohaapi.ComputeProviderActivationLevelRead, kinds, len(kinds) > 0, "resource reads are not permitted"),
		providerCapability("service_action", sohaapi.ComputeProviderActivationLevelWrite, []sohaapi.ComputeResourceKind{sohaapi.ComputeResourceKindService}, dockerServiceActionVisible(keys), "service actions are not permitted"),
	}, RuntimeMode: sohaapi.ComputePluginRuntimeModeBuiltin, Generation: generation, Health: providerHealth(sohaapi.ComputeProviderDomainContainerRuntime, "docker", health)}
}

func providerCapability(id string, level sohaapi.ComputeProviderActivationLevel, kinds []sohaapi.ComputeResourceKind, enabled bool, reason string) sohaapi.ComputeProviderCapability {
	item := sohaapi.ComputeProviderCapability{ID: id, Level: level, ResourceKinds: kinds, Enabled: enabled}
	if !enabled {
		item.Reason = reason
	}
	return item
}

func (s *Service) ListProviderInstances(ctx context.Context, principal domainidentity.Principal, filter ProviderInstanceFilter) (sohaapi.ComputeProviderInstanceListEnvelope, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeProviderInstanceListEnvelope{}, err
	}
	if !providerDomainVisible(s, keys) {
		return sohaapi.ComputeProviderInstanceListEnvelope{}, fmt.Errorf("%w: compute provider instances are not visible", apperrors.ErrAccessDenied)
	}
	if filter.Domain != "" && !sohaapi.ComputeProviderDomain(filter.Domain).Valid() {
		return sohaapi.ComputeProviderInstanceListEnvelope{}, invalidProviderInput("domain")
	}
	items := []sohaapi.ComputeProviderInstance{}
	if s.virtualizationAvailable() && has(keys, appaccess.PermVirtualizationClustersView) && (filter.Domain == "" || filter.Domain == string(sohaapi.ComputeProviderDomainVirtualization)) {
		connections, err := s.virtualization.ListConnections(ctx, domainvirtualization.ConnectionFilter{Provider: filter.ProviderKey, Limit: maxReadLimit})
		if err != nil {
			return sohaapi.ComputeProviderInstanceListEnvelope{}, err
		}
		for _, connection := range connections {
			items = append(items, virtualizationProviderInstance(connection, keys))
		}
	}
	if s.runtimeAvailable() && has(keys, appaccess.PermDockerHostsView) && providerMatches(filter.ProviderKey, "docker") && (filter.Domain == "" || filter.Domain == string(sohaapi.ComputeProviderDomainContainerRuntime)) {
		hosts, err := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
		if err != nil {
			return sohaapi.ComputeProviderInstanceListEnvelope{}, err
		}
		for _, host := range hosts {
			items = append(items, runtimeProviderInstance(host, keys))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].InstanceRef < items[j].InstanceRef })
	page, next, err := paginate(items, filter.Cursor, filter.Limit)
	return sohaapi.ComputeProviderInstanceListEnvelope{Items: page, NextCursor: next}, err
}

func (s *Service) GetProviderInstance(ctx context.Context, principal domainidentity.Principal, domain, providerKey, instanceRef string) (sohaapi.ComputeProviderInstance, error) {
	result, err := s.ListProviderInstances(ctx, principal, ProviderInstanceFilter{Domain: strings.TrimSpace(domain), ProviderKey: strings.TrimSpace(providerKey), Limit: maxReadLimit})
	if err != nil {
		return sohaapi.ComputeProviderInstance{}, err
	}
	for _, item := range result.Items {
		if item.InstanceRef == strings.TrimSpace(instanceRef) {
			return item, nil
		}
	}
	return sohaapi.ComputeProviderInstance{}, fmt.Errorf("%w: compute provider instance", apperrors.ErrNotFound)
}

func virtualizationProviderInstance(item domainvirtualization.Connection, keys []string) sohaapi.ComputeProviderInstance {
	descriptor := virtualizationProviderDescriptor(item.Provider, keys, connectionHealth(item))
	return sohaapi.ComputeProviderInstance{InstanceRef: item.ID, DisplayName: firstNonEmpty(item.Name, item.ID), AccessMode: sohaapi.ComputeAccessModeDirect, Enabled: item.Enabled, Snapshot: providerSnapshot(descriptor), Health: providerHealth(sohaapi.ComputeProviderDomainVirtualization, item.Provider, connectionHealth(item)), EffectiveCapabilities: descriptor.Capabilities, Resource: resourcePtr(connectionRef(item)), LastObservedAt: item.LastSyncedAt}
}

func runtimeProviderInstance(item domaindocker.Host, keys []string) sohaapi.ComputeProviderInstance {
	descriptor := runtimeProviderDescriptor(keys, runtimeHostStatus(item))
	return sohaapi.ComputeProviderInstance{InstanceRef: item.ID, DisplayName: firstNonEmpty(item.Name, item.ID), AccessMode: runtimeAccessMode(item), Enabled: true, Snapshot: providerSnapshot(descriptor), Health: providerHealth(sohaapi.ComputeProviderDomainContainerRuntime, "docker", runtimeHostStatus(item)), EffectiveCapabilities: descriptor.Capabilities, Resource: resourcePtr(runtimeHostRef(item)), LastObservedAt: item.LastHeartbeatAt}
}

func providerSnapshot(item sohaapi.ComputeProviderDescriptor) sohaapi.ComputeProviderSnapshot {
	return sohaapi.ComputeProviderSnapshot{Domain: item.Domain, ProviderKey: item.ProviderKey, Source: item.Source, Version: item.Version, ContractVersion: item.ContractVersion, RuntimeMode: item.RuntimeMode, Generation: item.Generation}
}

func (s *Service) CheckProviderInstanceHealth(ctx context.Context, principal domainidentity.Principal, domain, providerKey, instanceRef, idempotencyKey string, input sohaapi.ComputeProviderReadRequest) (sohaapi.ComputeTaskView, error) {
	if err := validateProviderGeneration(input.ExpectedGeneration); err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	connection, err := s.virtualizationProviderConnection(ctx, domain, providerKey, instanceRef, "health check")
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	task, err := s.virtualizationControl.TestConnectionIdempotent(ctx, principal, connection.ID, idempotencyKey)
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	return s.virtualizationTaskForPrincipal(ctx, principal, task)
}

func (s *Service) DiscoverProviderInstance(ctx context.Context, principal domainidentity.Principal, domain, providerKey, instanceRef, idempotencyKey string, input sohaapi.ComputeProviderDiscoverRequest) (sohaapi.ComputeTaskView, error) {
	if err := validateProviderGeneration(input.ExpectedGeneration); err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	connection, err := s.virtualizationProviderConnection(ctx, domain, providerKey, instanceRef, "discovery")
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	task, err := s.virtualizationControl.SyncConnectionIdempotent(ctx, principal, connection.ID, idempotencyKey)
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	return s.virtualizationTaskForPrincipal(ctx, principal, task)
}

func (s *Service) virtualizationProviderConnection(ctx context.Context, domain, providerKey, instanceRef, operation string) (domainvirtualization.Connection, error) {
	if strings.TrimSpace(domain) != string(sohaapi.ComputeProviderDomainVirtualization) || !s.virtualizationAvailable() || s.virtualization == nil || s.virtualizationControl == nil {
		return domainvirtualization.Connection{}, unsupportedProviderOperation(domain, operation)
	}
	connection, err := s.virtualization.GetConnection(ctx, strings.TrimSpace(instanceRef))
	if err != nil {
		return domainvirtualization.Connection{}, err
	}
	if !strings.EqualFold(connection.Provider, strings.TrimSpace(providerKey)) {
		return domainvirtualization.Connection{}, fmt.Errorf("%w: compute provider instance", apperrors.ErrNotFound)
	}
	return connection, nil
}

func (s *Service) GetResource(ctx context.Context, principal domainidentity.Principal, domain, kind, id string) (map[string]any, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return nil, err
	}
	ref, err := s.resourceRef(ctx, keys, domain, kind, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"resource": ref, "providerSnapshot": snapshotForResource(ref), "availableActions": availableResourceActions(keys, ref)}, nil
}

func (s *Service) ListResourceRelations(ctx context.Context, principal domainidentity.Principal, domain, kind, id, cursor string, limit int) (sohaapi.ComputeResourceRelations, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeResourceRelations{}, err
	}
	resource, err := s.resourceRef(ctx, keys, domain, kind, id)
	if err != nil {
		return sohaapi.ComputeResourceRelations{}, err
	}
	relations, err := s.derivedRelations(ctx, keys, resource)
	if err != nil {
		return sohaapi.ComputeResourceRelations{}, err
	}
	page, next, err := paginate(relations, cursor, limit)
	return sohaapi.ComputeResourceRelations{Resource: resource, Relations: page, NextCursor: next}, err
}

func (s *Service) ExecuteResourceAction(ctx context.Context, principal domainidentity.Principal, domain, kind, id, action, idempotencyKey string, input sohaapi.ComputeResourceActionRequest) (sohaapi.ComputeTaskView, error) {
	domain, kind, action = strings.TrimSpace(domain), strings.TrimSpace(kind), strings.TrimSpace(action)
	switch {
	case domain == string(sohaapi.ComputeDomainVirtualization) && kind == string(sohaapi.ComputeResourceKindVM):
		if s.virtualizationControl == nil {
			return sohaapi.ComputeTaskView{}, unsupportedProviderOperation(domain, "resource action")
		}
		parsed, err := virtualizationActionInput(action, idempotencyKey, input)
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		task, err := s.virtualizationControl.ExecuteVMAction(ctx, principal, strings.TrimSpace(id), parsed)
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		return s.virtualizationTaskForPrincipal(ctx, principal, task)
	case domain == string(sohaapi.ComputeDomainContainerRuntime) && kind == string(sohaapi.ComputeResourceKindService):
		if s.runtimeControl == nil {
			return sohaapi.ComputeTaskView{}, unsupportedProviderOperation(domain, "resource action")
		}
		task, err := s.runtimeControl.ServiceAction(ctx, principal, strings.TrimSpace(id), domaindocker.ServiceActionInput{Action: action, IdempotencyKey: idempotencyKey})
		if err != nil {
			return sohaapi.ComputeTaskView{}, err
		}
		return s.runtimeTaskForPrincipal(ctx, principal, task)
	default:
		return sohaapi.ComputeTaskView{}, unsupportedProviderOperation(domain, "resource action")
	}
}

func (s *Service) virtualizationTaskForPrincipal(ctx context.Context, principal domainidentity.Principal, task domainvirtualization.Task) (sohaapi.ComputeTaskView, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	return virtualizationTaskView(task, has(keys, appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "cancel")), has(keys, appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "retry"))), nil
}

func (s *Service) runtimeTaskForPrincipal(ctx context.Context, principal domainidentity.Principal, task domaindocker.Operation) (sohaapi.ComputeTaskView, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return sohaapi.ComputeTaskView{}, err
	}
	return runtimeTaskView(task, has(keys, appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "cancel")), has(keys, appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "retry"))), nil
}

func (s *Service) resourceRef(ctx context.Context, keys []string, domain, kind, id string) (sohaapi.ComputeResourceRef, error) {
	domain, kind, id = strings.TrimSpace(domain), strings.TrimSpace(kind), strings.TrimSpace(id)
	if !sohaapi.ComputeDomain(domain).Valid() || !sohaapi.ComputeResourceKind(kind).Valid() || id == "" {
		return sohaapi.ComputeResourceRef{}, invalidProviderInput("resource identity")
	}
	switch domain {
	case string(sohaapi.ComputeDomainVirtualization):
		return s.virtualizationResource(ctx, keys, sohaapi.ComputeResourceKind(kind), id)
	case string(sohaapi.ComputeDomainContainerRuntime):
		return s.runtimeResource(ctx, keys, sohaapi.ComputeResourceKind(kind), id)
	case string(sohaapi.ComputeDomainAgent):
		if kind != string(sohaapi.ComputeResourceKindAgentHost) || !has(keys, appaccess.PermDockerHostsView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		hosts, err := s.runtime.ListHosts(ctx, domaindocker.HostFilter{Limit: maxReadLimit})
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		for _, host := range hosts {
			if host.AgentID == id {
				return agentHostRef(host), nil
			}
		}
	}
	return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource", apperrors.ErrNotFound)
}

func (s *Service) virtualizationResource(ctx context.Context, keys []string, kind sohaapi.ComputeResourceKind, id string) (sohaapi.ComputeResourceRef, error) {
	switch kind {
	case sohaapi.ComputeResourceKindConnection:
		if !s.virtualizationAvailable() || !has(keys, appaccess.PermVirtualizationClustersView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.virtualization.GetConnection(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return connectionRef(item), nil
	case sohaapi.ComputeResourceKindVM:
		if !s.virtualizationAvailable() || !has(keys, appaccess.PermVirtualizationVMsView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.virtualization.GetVM(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return virtualizationResourceRef(kind, item.ID, item.Name, item.Provider, item.ConnectionID), nil
	case sohaapi.ComputeResourceKindImage:
		if !s.virtualizationAvailable() || !has(keys, appaccess.PermVirtualizationImagesView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.virtualization.GetImage(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return virtualizationResourceRef(kind, item.ID, item.Name, item.Provider, item.ConnectionID), nil
	case sohaapi.ComputeResourceKindFlavor:
		if !s.virtualizationAvailable() || !has(keys, appaccess.PermVirtualizationFlavorsView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.virtualization.GetFlavor(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return virtualizationResourceRef(kind, item.ID, item.Name, item.Provider, item.ConnectionID), nil
	default:
		return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: unsupported virtualization resource kind %q", apperrors.ErrUnsupportedOperation, kind)
	}
}

func (s *Service) runtimeResource(ctx context.Context, keys []string, kind sohaapi.ComputeResourceKind, id string) (sohaapi.ComputeResourceRef, error) {
	if !s.runtimeAvailable() {
		return sohaapi.ComputeResourceRef{}, unsupportedProviderOperation(string(sohaapi.ComputeDomainContainerRuntime), "resource read")
	}
	switch kind {
	case sohaapi.ComputeResourceKindRuntimeHost:
		if !has(keys, appaccess.PermDockerHostsView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.runtime.GetHost(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return runtimeHostRef(item), nil
	case sohaapi.ComputeResourceKindProject:
		if !has(keys, appaccess.PermDockerProjectsView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.runtime.GetProject(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return runtimeResourceRef(kind, item.ID, item.Name), nil
	case sohaapi.ComputeResourceKindService:
		if !has(keys, appaccess.PermDockerServicesView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.runtime.GetService(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return runtimeResourceRef(kind, item.ID, item.Name), nil
	case sohaapi.ComputeResourceKindPort:
		if !has(keys, appaccess.PermDockerPortsView) {
			return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: compute resource is not visible", apperrors.ErrAccessDenied)
		}
		item, err := s.runtime.GetPortMapping(ctx, id)
		if err != nil {
			return sohaapi.ComputeResourceRef{}, err
		}
		return runtimeResourceRef(kind, item.ID, item.Name), nil
	default:
		return sohaapi.ComputeResourceRef{}, fmt.Errorf("%w: unsupported runtime resource kind %q", apperrors.ErrUnsupportedOperation, kind)
	}
}

func (s *Service) derivedRelations(ctx context.Context, keys []string, resource sohaapi.ComputeResourceRef) ([]sohaapi.ComputeResourceRelation, error) {
	now := time.Now().UTC()
	relations := []sohaapi.ComputeResourceRelation{}
	add := func(from sohaapi.ComputeResourceRef, relationType sohaapi.ComputeRelationType, to sohaapi.ComputeResourceRef) {
		relations = append(relations, sohaapi.ComputeResourceRelation{From: from, Type: relationType, To: to, Source: sohaapi.ComputeRelationSourceDerived, ObservedAt: now, ProviderGeneration: generation})
	}
	if resource.Domain == sohaapi.ComputeDomainVirtualization {
		if resource.Kind == sohaapi.ComputeResourceKindConnection && has(keys, appaccess.PermVirtualizationVMsView) {
			items, err := s.virtualization.ListVMs(ctx, domainvirtualization.VMFilter{ConnectionID: resource.ID, Limit: maxReadLimit})
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				add(resource, sohaapi.ComputeRelationTypeContains, virtualizationResourceRef(sohaapi.ComputeResourceKindVM, item.ID, item.Name, item.Provider, item.ConnectionID))
			}
		}
		if resource.Kind == sohaapi.ComputeResourceKindVM && has(keys, appaccess.PermVirtualizationClustersView) {
			item, err := s.virtualization.GetVM(ctx, resource.ID)
			if err != nil {
				return nil, err
			}
			connection, err := s.virtualization.GetConnection(ctx, item.ConnectionID)
			if err == nil {
				add(resource, sohaapi.ComputeRelationTypeRunsOn, connectionRef(connection))
			}
		}
	}
	if resource.Domain == sohaapi.ComputeDomainContainerRuntime {
		if err := s.appendRuntimeRelations(ctx, keys, resource, add); err != nil {
			return nil, err
		}
	}
	return relations, nil
}

func (s *Service) appendRuntimeRelations(ctx context.Context, keys []string, resource sohaapi.ComputeResourceRef, add func(sohaapi.ComputeResourceRef, sohaapi.ComputeRelationType, sohaapi.ComputeResourceRef)) error {
	switch resource.Kind {
	case sohaapi.ComputeResourceKindRuntimeHost:
		return s.appendRuntimeHostRelations(ctx, keys, resource, add)
	case sohaapi.ComputeResourceKindProject:
		return s.appendRuntimeProjectRelations(ctx, keys, resource, add)
	case sohaapi.ComputeResourceKindService:
		return s.appendRuntimeServiceRelations(ctx, keys, resource, add)
	}
	return nil
}

func (s *Service) appendRuntimeHostRelations(ctx context.Context, keys []string, resource sohaapi.ComputeResourceRef, add func(sohaapi.ComputeResourceRef, sohaapi.ComputeRelationType, sohaapi.ComputeResourceRef)) error {
	host, err := s.runtime.GetHost(ctx, resource.ID)
	if err != nil {
		return err
	}
	if has(keys, appaccess.PermDockerProjectsView) {
		items, err := s.runtime.ListProjects(ctx, domaindocker.ProjectFilter{HostID: host.ID, Limit: maxReadLimit})
		if err != nil {
			return err
		}
		for _, item := range items {
			add(resource, sohaapi.ComputeRelationTypeContains, runtimeResourceRef(sohaapi.ComputeResourceKindProject, item.ID, item.Name))
		}
	}
	if has(keys, appaccess.PermDockerServicesView) {
		items, err := s.runtime.ListServices(ctx, domaindocker.ServiceFilter{HostID: host.ID, Limit: maxReadLimit})
		if err != nil {
			return err
		}
		for _, item := range items {
			add(resource, sohaapi.ComputeRelationTypeContains, runtimeResourceRef(sohaapi.ComputeResourceKindService, item.ID, item.Name))
		}
	}
	if s.virtualizationAvailable() && s.virtualization != nil && has(keys, appaccess.PermVirtualizationVMsView) && host.VMID != "" {
		if vm, err := s.virtualization.GetVM(ctx, host.VMID); err == nil {
			add(resource, sohaapi.ComputeRelationTypeRunsOn, virtualizationResourceRef(sohaapi.ComputeResourceKindVM, vm.ID, vm.Name, vm.Provider, vm.ConnectionID))
		}
	}
	return nil
}

func (s *Service) appendRuntimeProjectRelations(ctx context.Context, keys []string, resource sohaapi.ComputeResourceRef, add func(sohaapi.ComputeResourceRef, sohaapi.ComputeRelationType, sohaapi.ComputeResourceRef)) error {
	item, err := s.runtime.GetProject(ctx, resource.ID)
	if err != nil {
		return err
	}
	if has(keys, appaccess.PermDockerHostsView) {
		if host, err := s.runtime.GetHost(ctx, item.HostID); err == nil {
			add(resource, sohaapi.ComputeRelationTypeRunsOn, runtimeHostRef(host))
		}
	}
	if !has(keys, appaccess.PermDockerServicesView) {
		return nil
	}
	items, err := s.runtime.ListServices(ctx, domaindocker.ServiceFilter{ProjectID: item.ID, Limit: maxReadLimit})
	if err != nil {
		return err
	}
	for _, service := range items {
		add(resource, sohaapi.ComputeRelationTypeContains, runtimeResourceRef(sohaapi.ComputeResourceKindService, service.ID, service.Name))
	}
	return nil
}

func (s *Service) appendRuntimeServiceRelations(ctx context.Context, keys []string, resource sohaapi.ComputeResourceRef, add func(sohaapi.ComputeResourceRef, sohaapi.ComputeRelationType, sohaapi.ComputeResourceRef)) error {
	item, err := s.runtime.GetService(ctx, resource.ID)
	if err != nil {
		return err
	}
	if has(keys, appaccess.PermDockerProjectsView) {
		if project, err := s.runtime.GetProject(ctx, item.ProjectID); err == nil {
			add(resource, sohaapi.ComputeRelationTypeRunsOn, runtimeResourceRef(sohaapi.ComputeResourceKindProject, project.ID, project.Name))
		}
	}
	if !has(keys, appaccess.PermDockerPortsView) {
		return nil
	}
	items, err := s.runtime.ListPortMappings(ctx, domaindocker.PortMappingFilter{ServiceID: item.ID, Limit: maxReadLimit})
	if err != nil {
		return err
	}
	for _, port := range items {
		add(resource, sohaapi.ComputeRelationTypeExposes, runtimeResourceRef(sohaapi.ComputeResourceKindPort, port.ID, port.Name))
	}
	return nil
}

func virtualizationActionInput(action, idempotencyKey string, input sohaapi.ComputeResourceActionRequest) (VirtualizationActionInput, error) {
	values := map[string]string{}
	for _, entry := range input.Metadata {
		values[strings.TrimSpace(entry.Key)] = strings.TrimSpace(entry.Value)
	}
	parse := func(key string) (int, error) {
		if values[key] == "" {
			return 0, nil
		}
		value, err := strconv.Atoi(values[key])
		if err != nil {
			return 0, fmt.Errorf("%w: action metadata %s must be an integer", apperrors.ErrInvalidArgument, key)
		}
		return value, nil
	}
	cpu, err := parse("cpu")
	if err != nil {
		return VirtualizationActionInput{}, err
	}
	memory, err := parse("memoryMiB")
	if err != nil {
		return VirtualizationActionInput{}, err
	}
	disk, err := parse("diskGiB")
	if err != nil {
		return VirtualizationActionInput{}, err
	}
	return VirtualizationActionInput{Action: action, IdempotencyKey: idempotencyKey, CPU: cpu, MemoryMiB: memory, DiskGiB: disk, Reason: strings.TrimSpace(input.Reason)}, nil
}

func availableResourceActions(keys []string, ref sohaapi.ComputeResourceRef) []string {
	actions := []string{}
	if ref.Domain == sohaapi.ComputeDomainVirtualization && ref.Kind == sohaapi.ComputeResourceKindVM {
		if has(keys, appaccess.PermVirtualizationVMsPower) {
			actions = append(actions, "start", "stop", "reboot")
		}
		if has(keys, appaccess.PermVirtualizationVMsResize) {
			actions = append(actions, "resize")
		}
		if has(keys, appaccess.PermVirtualizationVMsDelete) {
			actions = append(actions, "delete")
		}
	}
	if ref.Domain == sohaapi.ComputeDomainContainerRuntime && ref.Kind == sohaapi.ComputeResourceKindService {
		for _, action := range []string{"start", "stop", "restart", "logs"} {
			if has(keys, appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, action)) {
				actions = append(actions, action)
			}
		}
	}
	return actions
}

func snapshotForResource(ref sohaapi.ComputeResourceRef) sohaapi.ComputeProviderSnapshot {
	domain := sohaapi.ComputeProviderDomainVirtualization
	if ref.Domain == sohaapi.ComputeDomainContainerRuntime || ref.Domain == sohaapi.ComputeDomainAgent {
		domain = sohaapi.ComputeProviderDomainContainerRuntime
	}
	return sohaapi.ComputeProviderSnapshot{Domain: domain, ProviderKey: ref.ProviderKey, Source: sohaapi.ComputeProviderSourceBuiltin, Version: "builtin", ContractVersion: computeContractVersion, RuntimeMode: sohaapi.ComputePluginRuntimeModeBuiltin, Generation: generation}
}

func providerHealthFromConnections(provider string, items []domainvirtualization.Connection) sohaapi.ComputeHealthStatus {
	status := sohaapi.ComputeHealthStatusUnknown
	for _, item := range items {
		if item.Provider != provider {
			continue
		}
		current := connectionHealth(item)
		if status == sohaapi.ComputeHealthStatusUnknown || current == sohaapi.ComputeHealthStatusUnavailable || current == sohaapi.ComputeHealthStatusDegraded {
			status = current
		}
	}
	return status
}

func aggregateRuntimeHealth(items []domaindocker.Host) sohaapi.ComputeHealthStatus {
	if len(items) == 0 {
		return sohaapi.ComputeHealthStatusUnknown
	}
	status := sohaapi.ComputeHealthStatusHealthy
	for _, item := range items {
		current := runtimeHostStatus(item)
		if current == sohaapi.ComputeHealthStatusUnavailable {
			return current
		}
		if current != sohaapi.ComputeHealthStatusHealthy {
			status = current
		}
	}
	return status
}

func providerDomainVisible(s *Service, keys []string) bool {
	return (s.virtualizationAvailable() && virtualizationDomainVisible(keys)) || (s.runtimeAvailable() && runtimeDomainVisible(keys))
}

func virtualizationWriteVisible(keys []string) bool {
	return hasAny(keys,
		appaccess.ManagedActionPermission(appaccess.PermVirtualizationClustersManage, "test"),
		appaccess.ManagedActionPermission(appaccess.PermVirtualizationSyncManage, "sync"),
		appaccess.PermVirtualizationVMsPower, appaccess.PermVirtualizationVMsResize, appaccess.PermVirtualizationVMsDelete,
	)
}

func runtimeWriteVisible(keys []string) bool {
	return dockerServiceActionVisible(keys)
}

func dockerServiceActionVisible(keys []string) bool {
	return hasAny(keys,
		appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "start"),
		appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "stop"),
		appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "restart"),
		appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "logs"),
	)
}

func validateProviderGeneration(value int64) error {
	if value != generation {
		return fmt.Errorf("%w: compute provider generation changed", apperrors.ErrConflict)
	}
	return nil
}

func unsupportedProviderOperation(domain, operation string) error {
	return fmt.Errorf("%w: compute %s is unavailable for domain %q", apperrors.ErrUnsupportedOperation, operation, strings.TrimSpace(domain))
}

func invalidProviderInput(name string) error {
	return fmt.Errorf("%w: invalid compute %s", apperrors.ErrInvalidArgument, name)
}

func resourcePtr(item sohaapi.ComputeResourceRef) *sohaapi.ComputeResourceRef { return &item }
