package bootstrap

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appexecution "github.com/opensoha/soha/internal/application/execution"
	appnetworkproxy "github.com/opensoha/soha/internal/application/networkproxy"
	appoperation "github.com/opensoha/soha/internal/application/operation"
	deployment "github.com/opensoha/soha/internal/application/proxydeployment"
	publication "github.com/opensoha/soha/internal/application/proxypublication"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	"github.com/opensoha/soha/internal/domain/identity"
	domaindeployment "github.com/opensoha/soha/internal/domain/proxydeployment"
	domainpublication "github.com/opensoha/soha/internal/domain/proxypublication"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	poolinfra "github.com/opensoha/soha/internal/infrastructure/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/proxysource"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	deploymentrepo "github.com/opensoha/soha/internal/repository/proxydeployment"
	publicationrepo "github.com/opensoha/soha/internal/repository/proxypublication"
)

type proxyWorkspaceServices struct {
	sources    *publication.Sources
	rules      *publication.Rules
	bundles    *publication.Bundles
	deployment *deployment.Service
	executor   *deployment.ExecutorService
}

func newProxyWorkspaceServices(cfg cfgpkg.Config, infra *infrastructure, repos *repositories, permissions *appaccess.PermissionResolver, audit *appaudit.Service, operations *appoperation.Service, execution *appexecution.Service, management *appnetworkproxy.Management) (*proxyWorkspaceServices, error) {
	store := publicationrepo.New(infra.databaseStore.DB())
	deploymentStore := deploymentrepo.New(infra.databaseStore.DB())
	repos.networkProxyRepository.SetBatchFence(deploymentStore)
	slots := make([]domaindeployment.Slot, 0, len(cfg.Runtime.ProxySlots))
	for _, slot := range cfg.Runtime.ProxySlots {
		slots = append(slots, slot.Slot())
	}
	authority := proxyWorkspaceAuthority{management: management, audit: audit, operations: operations}
	lifecycle, err := deployment.New(deploymentStore, permissions, slots, authority, authority)
	if err != nil {
		return nil, err
	}
	credentials, err := proxyExecutorCredentials(cfg)
	if err != nil {
		return nil, err
	}
	executor, err := deployment.NewExecutorService(lifecycle, execution, credentials)
	if err != nil {
		return nil, err
	}
	pool, err := poolinfra.NewPool(lifecycle, repos.networkProxyRepository, cfg.Runtime.ProxySlots)
	if err != nil {
		return nil, err
	}
	compiler := proxysource.NewCompiler()
	fetcher, err := proxysource.NewFetcher(proxysource.Options{Protected: proxySourceProtection(store), ProtectedHost: proxySourceHostProtection(store)})
	if err != nil {
		return nil, err
	}
	sources, err := publication.NewSources(store, authority, authority, compiler, fetcher, cfg.Security.CredentialEncryptionKeys)
	if err != nil {
		return nil, err
	}
	rules, err := publication.NewRules(store, authority, authority)
	if err != nil {
		return nil, err
	}
	destination, err := publication.NewDestinationPolicy(store, proxysource.Resolver{})
	if err != nil {
		return nil, err
	}
	// Runtime credentials and device projections are served exclusively by the
	// independent mTLS network-control listener, never by console handlers.
	denyRuntime := proxyConsoleOnlyIdentity{}
	bundles, err := publication.NewBundles(store, store, store, authority, authority, pool, management, compiler, denyRuntime, denyRuntime, repos.networkRuntimeRepository, destination, cfg.Security.CredentialEncryptionKeys)
	if err != nil {
		return nil, err
	}
	management.SetProbeProfiles(proxyInstalledProbes(cfg.Runtime.ProxyProbeProfiles))
	execution.AddExecutionTaskSink(lifecycle)
	return &proxyWorkspaceServices{sources: sources, rules: rules, bundles: bundles, deployment: lifecycle, executor: executor}, nil
}

func proxyExecutorCredentials(cfg cfgpkg.Config) ([]deployment.ExecutorCredential, error) {
	reserved := map[string]bool{cfg.Runtime.ExecutionRunnerToken: true}
	for _, item := range cfg.Runtime.OutpostRuntimeExecutors {
		token, err := item.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("load Outpost executor credential for isolation: %w", err)
		}
		reserved[token] = true
	}
	out := make([]deployment.ExecutorCredential, 0, len(cfg.Runtime.ProxyRuntimeExecutors))
	for _, item := range cfg.Runtime.ProxyRuntimeExecutors {
		token, err := item.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("load proxy executor credential: %w", err)
		}
		if reserved[token] {
			return nil, fmt.Errorf("proxy executor requires a dedicated credential")
		}
		reserved[token] = true
		out = append(out, deployment.ExecutorCredential{ExecutorID: item.ExecutorID, Token: token})
	}
	return out, nil
}

type proxyWorkspaceAuthority struct {
	management *appnetworkproxy.Management
	audit      *appaudit.Service
	operations *appoperation.Service
}

func (a proxyWorkspaceAuthority) Authorize(ctx context.Context, p identity.Principal, id, action string) error {
	return a.management.AuthorizePublication(ctx, p, id, action)
}
func (a proxyWorkspaceAuthority) AuthorizeDeployment(ctx context.Context, p identity.Principal, id string, write bool) error {
	action := "view"
	if write {
		action = "update"
	}
	return a.Authorize(ctx, p, id, action)
}
func (a proxyWorkspaceAuthority) RecordPublication(ctx context.Context, p identity.Principal, instance, action, object string, revision int64) error {
	meta := requestctx.FromContext(ctx)
	return a.Record(ctx, domainaudit.Entry{ActorID: p.UserID, ActorName: p.UserName, Roles: p.Roles, Teams: p.Teams,
		ResourceKind: "NetworkProxyInstance", ResourceName: instance, Action: "network_access.proxy_instances." + action,
		Result: "success", RequestID: meta.RequestID, RequestPath: meta.Path, RequestMethod: meta.Method, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"objectId": object, "revision": revision}})
}

func (a proxyWorkspaceAuthority) Record(ctx context.Context, entry domainaudit.Entry) error {
	if err := a.audit.Record(ctx, entry); err != nil {
		return err
	}
	p := identity.Principal{UserID: entry.ActorID, UserName: entry.ActorName, Roles: entry.Roles, Teams: entry.Teams}
	return a.operations.Record(ctx, operationentry.New(ctx, p, entry.Action, map[string]any{
		"module": "network_access", "resourceKind": entry.ResourceKind, "targetId": entry.ResourceName,
	}, entry.Result, entry.Action+" succeeded", entry.Metadata))
}

type proxyConsoleOnlyIdentity struct{}

func (proxyConsoleOnlyIdentity) AuthorizeProxyRuntime(context.Context, networkidentity.Identity) error {
	return apperrors.ErrAccessDenied
}
func (proxyConsoleOnlyIdentity) AuthorizeProjection(context.Context, networkidentity.Identity, string) (domainpublication.EndpointContext, error) {
	return domainpublication.EndpointContext{}, apperrors.ErrAccessDenied
}

func proxySourceProtection(store publication.ProtectionReader) func(context.Context, netip.Addr) bool {
	return func(ctx context.Context, address netip.Addr) bool {
		set, err := store.CurrentProtection(ctx)
		if err != nil || set == nil || set.PolicyVersion < 1 {
			return true
		}
		for _, cidr := range set.CIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || prefix.Contains(address.Unmap()) {
				return true
			}
		}
		return false
	}
}

func proxySourceHostProtection(store publication.ProtectionReader) func(context.Context, string) bool {
	return func(ctx context.Context, host string) bool {
		set, err := store.CurrentProtection(ctx)
		if err != nil || set == nil || set.PolicyVersion < 1 {
			return true
		}
		host = strings.TrimSuffix(strings.ToLower(host), ".")
		for _, entry := range set.Hosts {
			suffix := strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(entry), "."), "*.")
			if suffix == "" || host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
		}
		return false
	}
}

type proxyInstalledProbes []cfgpkg.ProxyProbeProfileConfig

func (profiles proxyInstalledProbes) ApprovedProbeProfile(_ context.Context, instance, profile string) (bool, error) {
	for _, installed := range profiles {
		if installed.InstanceID == instance && installed.ProfileID == profile {
			return true, nil
		}
	}
	return false, nil
}

var _ publication.Authorizer = proxyWorkspaceAuthority{}
var _ publication.EndpointAuthorizer = proxyConsoleOnlyIdentity{}
