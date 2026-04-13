package dto

type TriggerWorkflowRequest struct {
	ApplicationID  string `json:"applicationId"`
	WorkflowName   string `json:"workflowName"`
	ClusterID      string `json:"clusterId"`
	Namespace      string `json:"namespace"`
	DeploymentName string `json:"deploymentName"`
	TriggerBuild   bool   `json:"triggerBuild"`
	TriggerRelease bool   `json:"triggerRelease"`
}
