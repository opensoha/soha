package delivery

// ServiceReferencePlanImpact is private durable evidence. Never place it in a public Plan.
type ServiceReferencePlanImpact struct {
	Version          int                      `json:"version"`
	PlanID           string                   `json:"planId"`
	ApplicationID    string                   `json:"applicationId"`
	SourceBindingID  string                   `json:"sourceBindingId"`
	DatabaseRevision string                   `json:"databaseRevision"`
	Revision         string                   `json:"revision"`
	Consumers        []ServiceReferenceRecord `json:"consumers"`
	// SourceRevisions freezes identity/control; native runtime versions remain owned by each operation.
	SourceRevisions map[string]string `json:"sourceRevisions"`
}

type ServiceReferenceConsumer struct {
	ApplicationEnvironmentID       string   `json:"applicationEnvironmentId"`
	EnvironmentID                  string   `json:"environmentId"`
	EnvironmentKey                 string   `json:"environmentKey"`
	ReferenceID                    string   `json:"referenceId"`
	ReferenceRevision              int64    `json:"referenceRevision"`
	ServiceID                      string   `json:"serviceId"`
	SourceApplicationEnvironmentID string   `json:"sourceApplicationEnvironmentId"`
	SourceTargetIDs                []string `json:"sourceTargetIds"`
}

type ServiceReferenceConsumerPage struct {
	HasRestrictedConsumers bool                       `json:"hasRestrictedConsumers"`
	Items                  []ServiceReferenceConsumer `json:"items"`
	NextCursor             string                     `json:"nextCursor,omitempty"`
}

type DeliveryPlanServiceReferenceImpact struct {
	CurrentRevision                string                     `json:"currentRevision"`
	HasRestrictedConsumers         bool                       `json:"hasRestrictedConsumers"`
	IsCurrent                      bool                       `json:"isCurrent"`
	Items                          []ServiceReferenceConsumer `json:"items"`
	NextCursor                     string                     `json:"nextCursor,omitempty"`
	PlanID                         string                     `json:"planId"`
	Revision                       string                     `json:"revision"`
	SourceApplicationEnvironmentID string                     `json:"sourceApplicationEnvironmentId"`
}
