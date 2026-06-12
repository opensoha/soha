package event

import "time"

type Envelope struct {
	ID         string         `json:"id"`
	Source     string         `json:"source"`
	Category   string         `json:"category"`
	Severity   string         `json:"severity"`
	ClusterID  string         `json:"clusterId,omitempty"`
	Namespace  string         `json:"namespace,omitempty"`
	Summary    string         `json:"summary"`
	Payload    map[string]any `json:"payload,omitempty"`
	OccurredAt time.Time      `json:"occurredAt,omitempty"`
}

type ConnectorEvent struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Source     string         `json:"source"`
	OccurredAt string         `json:"occurredAt"`
	Subject    string         `json:"subject,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type ConnectorEventIngestInput struct {
	ConnectorID   string
	Events        []ConnectorEvent
	RequestPath   string
	RequestMethod string
	RequestID     string
	SourceIP      string
	ActorID       string
	ActorName     string
	ActorRoles    []string
	ActorTeams    []string
	AuthKind      string
}
