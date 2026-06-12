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
	OperationType     string
	ActorID           string
	ClusterID         string
	Namespace         string
	ResourceKind      string
	ResourceName      string
	Result            string
	RequestID         string
	RequestPath       string
	RequestMethod     string
	SourceIP          string
	ApprovalRequestID string
	AgentRunID        string
	RootCauseRunID    string
	MetadataKey       string
	MetadataValue     string
	From              *time.Time
	To                *time.Time
	Limit             int
}

type Summary struct {
	Total                 int64      `json:"total"`
	FailureCount          int64      `json:"failureCount"`
	RetentionDays         int        `json:"retentionDays"`
	RetentionCutoff       *time.Time `json:"retentionCutoff,omitempty"`
	OldestEntryAt         *time.Time `json:"oldestEntryAt,omitempty"`
	NewestEntryAt         *time.Time `json:"newestEntryAt,omitempty"`
	ExpiredEntryCount     int64      `json:"expiredEntryCount"`
	ExportRecommended     bool       `json:"exportRecommended"`
	RecommendedNextAction string     `json:"recommendedNextAction,omitempty"`
}

type Export struct {
	Filename    string    `json:"filename"`
	Content     []byte    `json:"-"`
	ContentType string    `json:"contentType"`
	Count       int       `json:"count"`
	GeneratedAt time.Time `json:"generatedAt"`
}

type Repository interface {
	Create(context.Context, Entry) error
	List(context.Context, Filter) ([]Entry, error)
	Summary(context.Context, Filter, int) (Summary, error)
}
