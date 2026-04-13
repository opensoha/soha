package dto

type TriggerReleaseRequest struct {
	ApplicationID  string `json:"applicationId"`
	ClusterID      string `json:"clusterId"`
	Namespace      string `json:"namespace"`
	DeploymentName string `json:"deploymentName"`
	ContainerName  string `json:"containerName"`
	Image          string `json:"image"`
	ImageTag       string `json:"imageTag"`
	ReleaseName    string `json:"releaseName"`
}
