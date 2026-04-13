package dto

import "time"

type IngestAlertsRequest struct {
	Source string             `json:"source"`
	Alerts []IngestAlertInput `json:"alerts"`
}

type IngestAlertInput struct {
	Fingerprint  string            `json:"fingerprint"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Severity     string            `json:"severity"`
	Status       string            `json:"status"`
	ClusterID    string            `json:"clusterId"`
	Namespace    string            `json:"namespace"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	Receiver     string            `json:"receiver"`
	GeneratorURL string            `json:"generatorUrl"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
}

type NotificationChannelRequest struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ChannelType string         `json:"channelType"`
	Enabled     bool           `json:"enabled"`
	Config      map[string]any `json:"config"`
}

type AlertRouteRequest struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Matchers   map[string]any `json:"matchers"`
	ChannelIDs []string       `json:"channelIds"`
	Enabled    bool           `json:"enabled"`
}

type AlertSilenceRequest struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Matchers map[string]any `json:"matchers"`
	Reason   string         `json:"reason"`
	StartsAt time.Time      `json:"startsAt"`
	EndsAt   time.Time      `json:"endsAt"`
	Enabled  bool           `json:"enabled"`
}

type AlertOwnershipRequest struct {
	OwnerTeam string `json:"ownerTeam"`
	Assignee  string `json:"assignee"`
}
