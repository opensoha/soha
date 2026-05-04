package delivery

import (
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
)

type ApplicationBindingSummary struct {
	ApplicationEnvironmentID string                 `json:"applicationEnvironmentId"`
	EnvironmentID            string                 `json:"environmentId"`
	EnvironmentName          string                 `json:"environmentName,omitempty"`
	EnvironmentKey           string                 `json:"environmentKey,omitempty"`
	ActionKind               string                 `json:"actionKind,omitempty"`
	RequiresApproval         bool                   `json:"requiresApproval"`
	WorkflowTemplateID       string                 `json:"workflowTemplateId,omitempty"`
	WorkflowTemplateName     string                 `json:"workflowTemplateName,omitempty"`
	TargetCount              int                    `json:"targetCount"`
	BuildSourceID            string                 `json:"buildSourceId,omitempty"`
	BuildSource              *domainapp.BuildSource `json:"buildSource,omitempty"`
	LatestBuild              *domainbuild.Record    `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run    `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record  `json:"latestRelease,omitempty"`
}

type ApplicationDetail struct {
	Application    domainapp.App               `json:"application"`
	Bindings       []ApplicationBindingSummary `json:"bindings,omitempty"`
	LatestBuild    *domainbuild.Record         `json:"latestBuild,omitempty"`
	LatestWorkflow *domainworkflow.Run         `json:"latestWorkflow,omitempty"`
	LatestRelease  *domainrelease.Record       `json:"latestRelease,omitempty"`
}

type ApplicationEnvironmentDetail struct {
	Binding          domaincatalog.ApplicationEnvironment `json:"binding"`
	Application      domainapp.App                        `json:"application"`
	Environment      *domaincatalog.Environment           `json:"environment,omitempty"`
	ActionKind       string                               `json:"actionKind,omitempty"`
	RequiresApproval bool                                 `json:"requiresApproval"`
	BuildSource      *domainapp.BuildSource               `json:"buildSource,omitempty"`
	LatestBuild      *domainbuild.Record                  `json:"latestBuild,omitempty"`
	LatestWorkflow   *domainworkflow.Run                  `json:"latestWorkflow,omitempty"`
	LatestRelease    *domainrelease.Record                `json:"latestRelease,omitempty"`
}

type ReleaseBoardEntry struct {
	ApplicationEnvironmentID string                        `json:"applicationEnvironmentId"`
	ApplicationID            string                        `json:"applicationId"`
	ApplicationName          string                        `json:"applicationName"`
	BusinessLineID           string                        `json:"businessLineId,omitempty"`
	EnvironmentID            string                        `json:"environmentId"`
	EnvironmentName          string                        `json:"environmentName,omitempty"`
	EnvironmentKey           string                        `json:"environmentKey,omitempty"`
	ActionKind               string                        `json:"actionKind,omitempty"`
	RequiresApproval         bool                          `json:"requiresApproval"`
	WorkflowTemplateID       string                        `json:"workflowTemplateId,omitempty"`
	WorkflowTemplateName     string                        `json:"workflowTemplateName,omitempty"`
	BuildSourceID            string                        `json:"buildSourceId,omitempty"`
	BuildSource              *domainapp.BuildSource        `json:"buildSource,omitempty"`
	Targets                  []domaincatalog.ReleaseTarget `json:"targets,omitempty"`
	LatestBuild              *domainbuild.Record           `json:"latestBuild,omitempty"`
	LatestWorkflow           *domainworkflow.Run           `json:"latestWorkflow,omitempty"`
	LatestRelease            *domainrelease.Record         `json:"latestRelease,omitempty"`
}

type TargetCandidate struct {
	ClusterID    string            `json:"clusterId"`
	Namespace    string            `json:"namespace"`
	WorkloadKind string            `json:"workloadKind"`
	WorkloadName string            `json:"workloadName"`
	Containers   []string          `json:"containers,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}
