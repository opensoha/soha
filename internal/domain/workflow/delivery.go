package workflow

import (
	"sort"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"

	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

const (
	ScopeApplication     = "application"
	ScopeDeliveryBatch   = "delivery_batch"
	DeliveryModeSerial   = "service_serial"
	DeliveryModeBuildAll = "build_all_then_deploy"
)

type DeliveryTargetInput struct {
	ID                       string                      `json:"id"`
	ApplicationID            string                      `json:"applicationId"`
	ServiceID                string                      `json:"serviceId"`
	ApplicationEnvironmentID string                      `json:"applicationEnvironmentId,omitempty"`
	ReleaseTargetID          string                      `json:"releaseTargetId,omitempty"`
	Action                   string                      `json:"action"`
	ReleaseBundleID          string                      `json:"releaseBundleId,omitempty"`
	HelmRevision             int                         `json:"helmRevision,omitempty"`
	RepositoryRefs           []domainbuild.RepositoryRef `json:"repositoryRefs,omitempty"`
	BuildArgs                map[string]string           `json:"buildArgs,omitempty"`
	Group                    int                         `json:"group,omitempty"`
	DependsOn                []string                    `json:"dependsOn,omitempty"`
}

type DeliveryWorkflowDefinition struct {
	Name                    string                `json:"name"`
	Mode                    string                `json:"mode,omitempty"`
	StopOnFailure           *bool                 `json:"stopOnFailure,omitempty"`
	MaxConcurrency          int                   `json:"maxConcurrency,omitempty"`
	WorkflowTemplateID      string                `json:"workflowTemplateId,omitempty"`
	WorkflowTemplateVersion int                   `json:"workflowTemplateVersion,omitempty"`
	Targets                 []DeliveryTargetInput `json:"targets"`
}

func (d DeliveryWorkflowDefinition) StopsOnFailure() bool {
	return d.StopOnFailure == nil || *d.StopOnFailure
}

type DeliveryWorkflowInput struct {
	IdempotencyKey  string                     `json:"idempotencyKey,omitempty"`
	ExpectedVersion *int64                     `json:"expectedVersion,omitempty"`
	Definition      DeliveryWorkflowDefinition `json:"definition"`
}

type DeliveryWorkflow struct {
	InvocationScopes []map[string]string        `json:"-"`
	ID               string                     `json:"id"`
	Version          int64                      `json:"version"`
	Definition       DeliveryWorkflowDefinition `json:"definition"`
	CreatedBy        string                     `json:"createdBy"`
	CreatedAt        time.Time                  `json:"createdAt"`
	UpdatedAt        time.Time                  `json:"updatedAt"`
}

type DeliveryBatchInput struct {
	IdempotencyKey  string                        `json:"idempotencyKey"`
	WorkflowID      string                        `json:"workflowId,omitempty"`
	WorkflowVersion int64                         `json:"workflowVersion,omitempty"`
	Definition      *DeliveryWorkflowDefinition   `json:"definition,omitempty"`
	RetryOfBatchID  string                        `json:"retryOfBatchId,omitempty"`
	SourceCommit    *sohaapi.DeliverySourceCommit `json:"sourceCommit,omitempty"`
	// Trigger delegation is trusted server context and cannot be supplied over HTTP.
	TriggerAuthorizerID      string `json:"-"`
	TriggerAuthorizerTokenID string `json:"-"`
}

type DeliveryTargetSnapshot struct {
	FrozenScope          map[string]string                        `json:"_frozenScope,omitempty"`
	FrozenDocker         *domaindocker.PreparedDeliveryProject    `json:"_frozenDocker,omitempty"`
	FrozenHelm           *sohaapi.HelmDeliverySnapshot            `json:"_frozenHelm,omitempty"`
	FrozenHelmCiphertext string                                   `json:"_frozenHelmCiphertext,omitempty"`
	HealthTimeoutSeconds int                                      `json:"_healthTimeoutSeconds,omitempty"`
	FrozenManifest       *domainmanifest.DeliveryConfiguration    `json:"_frozenManifest,omitempty"`
	FrozenReleaseTarget  *domaincatalog.ReleaseTarget             `json:"_frozenReleaseTarget,omitempty"`
	FrozenBuild          *domainbuild.Prepared                    `json:"_frozenBuild,omitempty"`
	Target               DeliveryTargetInput                      `json:"target"`
	ApplicationName      string                                   `json:"applicationName"`
	ServiceName          string                                   `json:"serviceName"`
	EnvironmentName      string                                   `json:"environmentName,omitempty"`
	ServiceVersion       int64                                    `json:"serviceVersion"`
	ConfigurationDigest  string                                   `json:"configurationDigest"`
	BuildFingerprint     string                                   `json:"buildFingerprint,omitempty"`
	BuildNodeID          string                                   `json:"buildNodeId,omitempty"`
	BuildSourceID        string                                   `json:"buildSourceId,omitempty"`
	RepositoryRefs       []domainbuild.RepositoryRef              `json:"repositoryRefs,omitempty"`
	DeploymentTemplate   *domaincatalog.DeploymentTemplateBinding `json:"deploymentTemplate,omitempty"`
	ManifestPackageID    string                                   `json:"manifestPackageId,omitempty"`
	ManifestBindingID    string                                   `json:"manifestBindingId,omitempty"`
	ManifestRevision     int                                      `json:"manifestRevision,omitempty"`
}

func (s DeliveryTargetSnapshot) ResourceKeys() []string {
	if s.FrozenDocker != nil {
		return []string{"docker:" + s.FrozenDocker.HostID + ":" + s.FrozenDocker.ProjectID}
	}
	if s.FrozenManifest != nil {
		return s.FrozenManifest.ResourceKeys
	}
	if s.FrozenHelm == nil {
		return nil
	}
	return HelmResourceKeys(*s.FrozenHelm)
}

func HelmResourceKeys(snapshot sohaapi.HelmDeliverySnapshot) []string {
	keys := []string{"helm:" + snapshot.ClusterID + ":" + snapshot.Namespace + ":" + snapshot.ReleaseName}
	for _, resource := range snapshot.Resources {
		group, _, found := strings.Cut(resource.APIVersion, "/")
		if !found {
			group = ""
		}
		keys = append(keys, strings.Join([]string{snapshot.ClusterID, group, resource.Kind, resource.Namespace, resource.Name}, "/"))
	}
	sort.Strings(keys)
	return keys
}

// DeliveryBatch stores the frozen intent and projects state from one root Run.
// Tasks, artifacts, plans and logs remain in their existing repositories.
type DeliveryBatch struct {
	InvocationScopes       []map[string]string        `json:"-"`
	PartialView            bool                       `json:"partialView,omitempty"`
	WorkflowTemplateDigest string                     `json:"workflowTemplateDigest,omitempty"`
	ID                     string                     `json:"id"`
	RootRunID              string                     `json:"rootRunId"`
	WorkflowID             string                     `json:"workflowId,omitempty"`
	WorkflowVersion        int64                      `json:"workflowVersion,omitempty"`
	RetryOfBatchID         string                     `json:"retryOfBatchId,omitempty"`
	Definition             DeliveryWorkflowDefinition `json:"definition"`
	Targets                []DeliveryTargetSnapshot   `json:"targets"`
	Status                 string                     `json:"status"`
	StopReason             string                     `json:"stopReason,omitempty"`
	StopSummary            string                     `json:"stopSummary,omitempty"`
	ServiceCount           int                        `json:"serviceCount"`
	TargetCount            int                        `json:"targetCount"`
	BuildCount             int                        `json:"buildCount"`
	Nodes                  []NodeRun                  `json:"nodes"`
	CreatedBy              string                     `json:"createdBy"`
	CreatedAt              time.Time                  `json:"createdAt"`
	UpdatedAt              time.Time                  `json:"updatedAt"`
}
