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
	ActorID           string
	ActorName         string
	ClusterID         string
	Namespace         string
	ResourceKind      string
	ResourceName      string
	Action            string
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

type Service interface {
	Record(context.Context, Entry) error
	List(context.Context, Filter) ([]Entry, error)
}
