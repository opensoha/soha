package event

type Envelope struct {
	ID        string         `json:"id"`
	Source    string         `json:"source"`
	Category  string         `json:"category"`
	Severity  string         `json:"severity"`
	ClusterID string         `json:"clusterId,omitempty"`
	Namespace string         `json:"namespace,omitempty"`
	Summary   string         `json:"summary"`
	Payload   map[string]any `json:"payload,omitempty"`
}
