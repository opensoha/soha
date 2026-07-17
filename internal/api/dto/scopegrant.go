package dto

type ScopeGrantRequest struct {
	ID                string   `json:"id"`
	SubjectType       string   `json:"subjectType"`
	SubjectID         string   `json:"subjectId"`
	BusinessLineID    string   `json:"businessLineId"`
	EnvironmentIDs    []string `json:"environmentIds"`
	ApplicationIDs    []string `json:"applicationIds"`
	ScopeType         string   `json:"scopeType"`
	ClusterIDs        []string `json:"clusterIds"`
	Namespaces        []string `json:"namespaces"`
	NamespaceSelector string   `json:"namespaceSelector"`
	ResourceGroups    []string `json:"resourceGroups"`
	ResourceKinds     []string `json:"resourceKinds"`
	Role              string   `json:"role"`
	Effect            string   `json:"effect"`
	Enabled           bool     `json:"enabled"`
}
