package alert

import (
	"context"
	"time"
)

type Instance struct {
	ID           string            `json:"id"`
	Source       string            `json:"source"`
	Fingerprint  string            `json:"fingerprint"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Severity     string            `json:"severity"`
	Status       string            `json:"status"`
	ClusterID    string            `json:"clusterId,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Receiver     string            `json:"receiver,omitempty"`
	GeneratorURL string            `json:"generatorUrl,omitempty"`
	StartsAt     time.Time         `json:"startsAt,omitempty"`
	EndsAt       time.Time         `json:"endsAt,omitempty"`
	LastSeenAt   time.Time         `json:"lastSeenAt"`
	OwnerTeam    string            `json:"ownerTeam,omitempty"`
	Assignee     string            `json:"assignee,omitempty"`
	AcknowledgedAt time.Time       `json:"acknowledgedAt,omitempty"`
	AcknowledgedBy string          `json:"acknowledgedBy,omitempty"`
	AcknowledgedByName string      `json:"acknowledgedByName,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type Summary struct {
	TotalCount     int       `json:"totalCount"`
	FiringCount    int       `json:"firingCount"`
	ResolvedCount  int       `json:"resolvedCount"`
	CriticalCount  int       `json:"criticalCount"`
	WarningCount   int       `json:"warningCount"`
	InfoCount      int       `json:"infoCount"`
	ChannelCount   int       `json:"channelCount"`
	LastReceivedAt time.Time `json:"lastReceivedAt,omitempty"`
}

type NotificationChannel struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ChannelType string         `json:"channelType"`
	Enabled     bool           `json:"enabled"`
	Config      map[string]any `json:"config,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type ChannelInput struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ChannelType string         `json:"channelType"`
	Enabled     bool           `json:"enabled"`
	Config      map[string]any `json:"config,omitempty"`
}

type AlertRoute struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Matchers   map[string]any `json:"matchers,omitempty"`
	ChannelIDs []string       `json:"channelIds,omitempty"`
	Enabled    bool           `json:"enabled"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type RouteInput struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Matchers   map[string]any `json:"matchers,omitempty"`
	ChannelIDs []string       `json:"channelIds,omitempty"`
	Enabled    bool           `json:"enabled"`
}

type AlertSilence struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Matchers  map[string]any `json:"matchers,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	StartsAt  time.Time      `json:"startsAt"`
	EndsAt    time.Time      `json:"endsAt"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type SilenceInput struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Matchers map[string]any `json:"matchers,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	StartsAt time.Time      `json:"startsAt"`
	EndsAt   time.Time      `json:"endsAt"`
	Enabled  bool           `json:"enabled"`
}

type DeliveryLog struct {
	ID        string         `json:"id"`
	AlertID   string         `json:"alertId"`
	ChannelID string         `json:"channelId,omitempty"`
	Status    string         `json:"status"`
	Summary   string         `json:"summary,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type DeliveryFilter struct {
	AlertID string
	Status  string
	Limit   int
}

type OwnershipInput struct {
	OwnerTeam string `json:"ownerTeam"`
	Assignee  string `json:"assignee"`
}

type IngestAlert struct {
	Fingerprint  string            `json:"fingerprint"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Severity     string            `json:"severity"`
	Status       string            `json:"status"`
	ClusterID    string            `json:"clusterId,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Receiver     string            `json:"receiver,omitempty"`
	GeneratorURL string            `json:"generatorUrl,omitempty"`
	StartsAt     time.Time         `json:"startsAt,omitempty"`
	EndsAt       time.Time         `json:"endsAt,omitempty"`
}

type IngestRequest struct {
	Source string        `json:"source"`
	Alerts []IngestAlert `json:"alerts"`
}

type Filter struct {
	Status    string
	ClusterID string
	Limit     int
}

type Repository interface {
	Upsert(context.Context, string, []IngestAlert) ([]Instance, error)
	List(context.Context, Filter) ([]Instance, error)
	Get(context.Context, string) (Instance, error)
	UpdateOwnership(context.Context, string, OwnershipInput) (Instance, error)
	Acknowledge(context.Context, string, string, string) (Instance, error)
	Summary(context.Context) (Summary, error)
	ListChannels(context.Context) ([]NotificationChannel, error)
	CreateChannel(context.Context, ChannelInput) (NotificationChannel, error)
	UpdateChannel(context.Context, string, ChannelInput) (NotificationChannel, error)
	ListRoutes(context.Context) ([]AlertRoute, error)
	CreateRoute(context.Context, RouteInput) (AlertRoute, error)
	UpdateRoute(context.Context, string, RouteInput) (AlertRoute, error)
	ListSilences(context.Context) ([]AlertSilence, error)
	CreateSilence(context.Context, SilenceInput) (AlertSilence, error)
	UpdateSilence(context.Context, string, SilenceInput) (AlertSilence, error)
	ListDeliveryLogs(context.Context, DeliveryFilter) ([]DeliveryLog, error)
	CreateDeliveryLog(context.Context, DeliveryLog) error
}
