package audit

import (
	"context"
	"time"
)

type Entry struct {
	ID            string         `json:"id"`
	ActorID       string         `json:"actorId"`
	ActorName     string         `json:"actorName"`
	Roles         []string       `json:"roles"`
	Teams         []string       `json:"teams"`
	ClusterID     string         `json:"clusterId,omitempty"`
	Namespace     string         `json:"namespace,omitempty"`
	ResourceKind  string         `json:"resourceKind,omitempty"`
	ResourceName  string         `json:"resourceName,omitempty"`
	Action        string         `json:"action"`
	Result        string         `json:"result"`
	Summary       string         `json:"summary"`
	RequestPath   string         `json:"requestPath,omitempty"`
	RequestMethod string         `json:"requestMethod,omitempty"`
	RequestID     string         `json:"requestId,omitempty"`
	SourceIP      string         `json:"sourceIp,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type Filter struct {
	Action string
	Result string
	Limit  int
}

type Repository interface {
	Create(context.Context, Entry) error
	List(context.Context, Filter) ([]Entry, error)
}

type Service interface {
	Record(context.Context, Entry) error
	List(context.Context, Filter) ([]Entry, error)
}
