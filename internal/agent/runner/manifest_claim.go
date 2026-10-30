package runner

import (
	"context"
	"errors"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"net/http"
	"strings"
)

// These wire values are owned by Contracts' claimExecutionTask specification.
// The released v0.1.26 SDK already supports the required HTTP transport hook.
const manifestAdmissionProviderPrefix = "manifest_delivery_v1."
const manifestAdmissionHeader = "X-Soha-Manifest-Admission-Version"

var errManifestAdmission = errors.New("manifest claim admission was not confirmed")

type manifestClaimTransport struct {
	base         http.RoundTripper
	confirmation []string
}

func (transport *manifestClaimTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	transport.confirmation = nil
	if err == nil && response != nil {
		transport.confirmation = append([]string(nil), response.Header.Values(manifestAdmissionHeader)...)
	}
	return response, err
}

// Each claim gets its own transport observation. A confirmation from another
// claim, callback or status GET cannot authorize this task.
func claimExecutionWithManifestAdmission(ctx context.Context, client *sohaapi.Client, request claimRequest) (ExecutionTask, error) {
	scoped := *client
	httpClient := *http.DefaultClient
	if client.HTTPClient != nil {
		httpClient = *client.HTTPClient
	}
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &manifestClaimTransport{base: base}
	httpClient.Transport = transport
	scoped.HTTPClient = &httpClient
	task, err := scoped.ClaimExecutionTask(ctx, request)
	if err != nil {
		return task, err
	}
	if !strings.HasPrefix(task.ProviderKind, manifestAdmissionProviderPrefix) {
		if legacyManifestPlanTask(task) {
			return task, errManifestAdmission
		}
		return task, nil
	}
	if !strings.HasPrefix(task.TaskKind, "manifest_") || len(transport.confirmation) != 1 || transport.confirmation[0] != "1" {
		return task, errManifestAdmission
	}
	return task, nil
}

// Legacy markers can only reject a claim. They never reconstruct Plan identity
// or grant execution in place of the durable Core operation admission.
func legacyManifestPlanTask(task ExecutionTask) bool {
	if !strings.HasPrefix(task.TaskKind, "manifest_") {
		return false
	}
	manager, _ := task.Payload["fieldManager"].(string)
	initialization, _ := task.Payload["initializationPlanId"].(string)
	return strings.HasPrefix(manager, "opensoha-manifest/") || initialization != ""
}

func protectedManifestProviders(clusterID string) []string {
	return []string{
		manifestAdmissionProviderPrefix + resourceruntime.ManifestAgentProviderPrefix + clusterID,
		manifestAdmissionProviderPrefix + resourceruntime.RootCRAgentProviderPrefix + clusterID,
		manifestAdmissionProviderPrefix + resourceruntime.ManifestInitializationProviderPrefix + clusterID,
	}
}

func (r *Runner) rejectUnadmittedManifestClaim(ctx context.Context, task ExecutionTask, err error) {
	if task.ID == "" || !errors.Is(err, errManifestAdmission) {
		return
	}
	r.finalCallback(ctx, task, "failed", map[string]any{"error": "Manifest Plan admission was not confirmed; upgrade Core and create a new Plan"})
	r.metrics.markOutcome(metricScopeExecution, "failed")
}
