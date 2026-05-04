package operation

import (
	"context"
	"time"
)

type Entry struct {
	ID            string         `json:"id"`
	ActorID       string         `json:"actorId"`
	ActorName     string         `json:"actorName,omitempty"`
	OperationType string         `json:"operationType"`
	TargetScope   map[string]any `json:"targetScope"`
	Result        string         `json:"result"`
	Summary       string         `json:"summary"`
	RequestPath   string         `json:"requestPath,omitempty"`
	RequestMethod string         `json:"requestMethod,omitempty"`
	RequestID     string         `json:"requestId,omitempty"`
	SourceIP      string         `json:"sourceIp,omitempty"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type Filter struct {
	OperationType string
	Result        string
	Limit         int
}

type Repository interface {
	Create(context.Context, Entry) error
	List(context.Context, Filter) ([]Entry, error)
}
