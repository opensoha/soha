package catalog

type WorkflowCatalogFilter struct {
	Kind, ApplicationID, EnvironmentID, Search string
	Offset, Limit                              int
}

type WorkflowCatalogScope struct {
	ApplicationID            string `json:"applicationId"`
	ApplicationName          string `json:"applicationName"`
	ServiceID                string `json:"serviceId,omitempty"`
	ApplicationEnvironmentID string `json:"applicationEnvironmentId,omitempty"`
	EnvironmentID            string `json:"environmentId,omitempty"`
	EnvironmentName          string `json:"environmentName,omitempty"`
}

type WorkflowCatalogEntry struct {
	ID         string                 `json:"id"`
	SourceKind string                 `json:"sourceKind"`
	SourceID   string                 `json:"sourceId"`
	Name       string                 `json:"name"`
	Context    string                 `json:"context"`
	Enabled    bool                   `json:"enabled"`
	Scopes     []WorkflowCatalogScope `json:"scopes"`
}

type WorkflowCatalogOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type WorkflowCatalogPage struct {
	Items        []WorkflowCatalogEntry  `json:"items"`
	Total        int                     `json:"total"`
	Applications []WorkflowCatalogOption `json:"applications"`
	Environments []WorkflowCatalogOption `json:"environments"`
}

// WorkflowCatalogCandidate carries authorization attributes only inside Core.
type WorkflowCatalogCandidate struct {
	WorkflowCatalogEntry
	AuthorizationScopes []WorkflowCatalogAuthorizationScope
}

type WorkflowCatalogAuthorizationScope struct {
	WorkflowCatalogScope
	ApplicationKey   string `json:"applicationKey"`
	BusinessLineID   string `json:"businessLineId"`
	ApplicationGroup string `json:"applicationGroup"`
	EnvironmentKey   string `json:"environmentKey"`
	Exists           bool   `json:"exists"`
}
