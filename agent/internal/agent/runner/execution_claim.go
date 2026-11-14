package runner

import (
	"context"
	"reflect"
	"slices"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/helmrelease"
	resourceruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

type clusterExecutionClaim struct {
	clusterID string
	token     string
	providers []string
}

func (r *Runner) controlPlaneEnabled() bool {
	return r.cfg.Enabled && strings.TrimSpace(r.cfg.BaseURL) != "" && strings.TrimSpace(r.cfg.BearerToken) != ""
}

func (r *Runner) ExecutionClaimCapabilities() []string {
	if !r.controlPlaneEnabled() {
		return nil
	}
	var capabilities []string
	if len(r.standaloneManifestProviders()) > 0 {
		capabilities = append(capabilities, resourceruntime.RootCRAgentCapability)
	}
	if len(r.manifestExecutionClaim().providers) > 0 {
		capabilities = append(capabilities, manifestAdmissionCapability, resourceruntime.ManifestInitializationCapability)
	}
	if len(r.helmExecutionClaim().providers) > 0 {
		capabilities = append(capabilities, helmrelease.InitializationCapability)
	}
	return capabilities
}

func (r *Runner) executionClaimEnabled() bool {
	return len(r.helmExecutionClaim().providers) > 0 ||
		len(r.manifestExecutionClaim().providers) > 0 ||
		len(r.generalExecutionProviders()) > 0
}

func (r *Runner) claim(ctx context.Context) (ExecutionTask, bool) {
	if r.cfg.Buildpacks.Enabled && r.buildpacksBlocked.Load() {
		return ExecutionTask{}, false
	}
	helm, manifest := r.helmExecutionClaim(), r.manifestExecutionClaim()
	if helm.clusterID == manifest.clusterID && helm.token == manifest.token {
		helm.providers = append(helm.providers, manifest.providers...)
		manifest.providers = nil
	}
	for _, claim := range []clusterExecutionClaim{helm, manifest} {
		if len(claim.providers) == 0 {
			continue
		}
		client := sohaapi.NewClient(r.cfg.BaseURL, sohaapi.WithBearerToken(claim.token), sohaapi.WithHTTPClient(r.httpClient))
		if task, ok := r.claimExecutionTask(ctx, client, claim.providers); ok {
			return task, true
		}
	}
	providers := r.generalExecutionProviders()
	if len(providers) == 0 {
		return ExecutionTask{}, false
	}
	return r.claimExecutionTask(ctx, r.apiClient(), providers)
}

func (r *Runner) generalExecutionProviders() []string {
	providers := slices.DeleteFunc(slices.Clone(r.cfg.ProviderKinds), executorOwnedProvider)
	if r.cfg.Buildpacks.Enabled {
		providers = []string{"buildpacks_runner." + r.cfg.AgentID}
	}
	if r.sshClusterID != "" {
		providers = append(providers, "vm_ssh."+r.sshClusterID)
	}
	return append(providers, r.standaloneManifestProviders()...)
}

func executorOwnedProvider(provider string) bool {
	provider = strings.TrimSpace(provider)
	for _, prefix := range []string{
		helmAgentProviderPrefix,
		helmrelease.InitializationProviderPrefix,
		legacyManifestAgentProviderPrefix,
		resourceruntime.ManifestAgentProviderPrefix,
		resourceruntime.RootCRAgentProviderPrefix,
		resourceruntime.ManifestInitializationProviderPrefix,
		manifestAdmissionProviderPrefix,
	} {
		if strings.HasPrefix(provider, prefix) {
			return true
		}
	}
	return false
}

func (r *Runner) claimExecutionTask(ctx context.Context, client *sohaapi.Client, providers []string) (ExecutionTask, bool) {
	task, err := claimExecutionWithManifestAdmission(ctx, client, claimRequest{
		AgentID:         firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		ProviderKinds:   providers,
		RuntimeEndpoint: strings.TrimSpace(r.cfg.RuntimeEndpoint),
	})
	if err == nil && strings.HasPrefix(task.ProviderKind, manifestAdmissionProviderPrefix) && !slices.Contains(providers, task.ProviderKind) {
		err = errManifestAdmission
	}
	r.rejectUnadmittedManifestClaim(ctx, task, err)
	ok := err == nil && strings.TrimSpace(task.ID) != ""
	r.metrics.markClaim(metricScopeExecution, ok)
	if !ok {
		return ExecutionTask{}, false
	}
	return task, true
}

func nilExecutionPort(executor any) bool {
	switch value := reflect.ValueOf(executor); value.Kind() {
	case reflect.Invalid:
		return true
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
