package governance

import "time"

const AlertSourceSystem = "soha-governance"

type AlertInput struct {
	ID            string
	Source        string
	EventID       string
	ActorID       string
	ActorName     string
	Action        string
	OperationType string
	Result        string
	Summary       string
	ClusterID     string
	Namespace     string
	ResourceKind  string
	ResourceName  string
	RequestPath   string
	RequestMethod string
	RequestID     string
	SourceIP      string
	Severity      string
	Labels        map[string]string
	Annotations   map[string]string
	CreatedAt     time.Time
}
