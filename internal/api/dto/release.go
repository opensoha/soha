package dto

type TriggerReleaseRequest struct {
	ApplicationID            string `json:"applicationId"`
	ApplicationEnvironmentID string `json:"applicationEnvironmentId"`
	ClusterID                string `json:"clusterId"`
	Namespace                string `json:"namespace"`
	DeploymentName           string `json:"deploymentName"`
	ContainerName            string `json:"containerName"`
	Image                    string `json:"image"`
	ImageTag                 string `json:"imageTag"`
	ReleaseName              string `json:"releaseName"`
	ActionKind               string `json:"actionKind"`
	WorkflowRunID            string `json:"workflowRunId"`
}
