package dto

type TriggerBuildRequest struct {
	ApplicationID string         `json:"applicationId"`
	RefType       string         `json:"refType"`
	RefName       string         `json:"refName"`
	ImageTag      string         `json:"imageTag"`
	BuildArgs     map[string]any `json:"buildArgs"`
}
