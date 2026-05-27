package dto

type TriggerWorkflowRequest struct {
	ApplicationID            string         `json:"applicationId"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId"`
	WorkflowName             string         `json:"workflowName"`
	ClusterID                string         `json:"clusterId"`
	Namespace                string         `json:"namespace"`
	DeploymentName           string         `json:"deploymentName"`
	BuildSourceID            string         `json:"buildSourceId"`
	RefType                  string         `json:"refType"`
	RefName                  string         `json:"refName"`
	ImageTag                 string         `json:"imageTag"`
	ReleaseName              string         `json:"releaseName"`
	ContainerName            string         `json:"containerName"`
	Variables                map[string]any `json:"variables"`
	BuildArgs                map[string]any `json:"buildArgs"`
	TriggerBuild             bool           `json:"triggerBuild"`
	TriggerRelease           bool           `json:"triggerRelease"`
}

type WorkflowApprovalRequest struct {
	Comment string `json:"comment"`
}
