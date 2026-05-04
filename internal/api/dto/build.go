package dto

type TriggerBuildRequest struct {
	ApplicationID            string         `json:"applicationId"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId"`
	BuildSourceID            string         `json:"buildSourceId"`
	RefType                  string         `json:"refType"`
	RefName                  string         `json:"refName"`
	ImageTag                 string         `json:"imageTag"`
	BuildArgs                map[string]any `json:"buildArgs"`
	Variables                map[string]any `json:"variables"`
}
