package operation

import "context"

type Entry struct {
	ID            string         `json:"id"`
	ActorID       string         `json:"actorId"`
	OperationType string         `json:"operationType"`
	TargetScope   map[string]any `json:"targetScope"`
	Result        string         `json:"result"`
	Summary       string         `json:"summary"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     string         `json:"createdAt"`
}

type Repository interface {
	List(context.Context, int) ([]Entry, error)
}
