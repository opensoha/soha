package dto

import "time"

type IngestAlertsRequest struct {
	Source string             `json:"source"`
	Alerts []IngestAlertInput `json:"alerts"`
}

type AlertIntegrationRequest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationType string         `json:"integrationType"`
	Description     string         `json:"description"`
	Token           string         `json:"token"`
	LabelMapping    map[string]any `json:"labelMapping"`
	DedupeConfig    map[string]any `json:"dedupeConfig"`
	Enabled         *bool          `json:"enabled"`
}

type AlertIntegrationTestRequest struct {
	IntegrationType string         `json:"integrationType"`
	LabelMapping    map[string]any `json:"labelMapping"`
	DedupeConfig    map[string]any `json:"dedupeConfig"`
	Payload         map[string]any `json:"payload"`
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

type AlertRuleRequest struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	RuleType             string            `json:"ruleType"`
	DatasourceSelector   map[string]any    `json:"datasourceSelector"`
	QuerySpec            map[string]any    `json:"querySpec"`
	ThresholdSpec        map[string]any    `json:"thresholdSpec"`
	ForSeconds           int               `json:"forSeconds"`
	GroupBy              []string          `json:"groupBy"`
	Labels               map[string]string `json:"labels"`
	Annotations          map[string]string `json:"annotations"`
	NotificationPolicyID string            `json:"notificationPolicyId"`
	HealingPolicyIDs     []string          `json:"healingPolicyIds"`
	Enabled              bool              `json:"enabled"`
}

type AlertEventRequest struct {
	ID                 string            `json:"id"`
	RuleID             string            `json:"ruleId"`
	SourceType         string            `json:"sourceType"`
	SourceSystem       string            `json:"sourceSystem"`
	Fingerprint        string            `json:"fingerprint"`
	Title              string            `json:"title"`
	Summary            string            `json:"summary"`
	Severity           string            `json:"severity"`
	Status             string            `json:"status"`
	ClusterID          string            `json:"clusterId"`
	Namespace          string            `json:"namespace"`
	Labels             map[string]string `json:"labels"`
	Annotations        map[string]string `json:"annotations"`
	Receiver           string            `json:"receiver"`
	GeneratorURL       string            `json:"generatorUrl"`
	CurrentState       string            `json:"currentState"`
	LastNotificationAt time.Time         `json:"lastNotificationAt"`
	StartsAt           time.Time         `json:"startsAt"`
	EndsAt             time.Time         `json:"endsAt"`
	LastSeenAt         time.Time         `json:"lastSeenAt"`
}

type NotificationPolicyRequest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Matchers        map[string]any `json:"matchers"`
	ProcessorChain  []string       `json:"processorChain"`
	ChannelRefs     []string       `json:"channelRefs"`
	OnCallRef       string         `json:"oncallRef"`
	SendResolved    bool           `json:"sendResolved"`
	CooldownSeconds int            `json:"cooldownSeconds"`
	Enabled         bool           `json:"enabled"`
}

type NotificationTemplateRequest struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	TemplateType  string         `json:"templateType"`
	ContentType   string         `json:"contentType"`
	BodyTemplate  string         `json:"bodyTemplate"`
	Headers       map[string]any `json:"headers"`
	QueryParams   map[string]any `json:"queryParams"`
	SamplePayload map[string]any `json:"samplePayload"`
	Enabled       bool           `json:"enabled"`
}

type HealingPolicyRequest struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	TriggerMode         string         `json:"triggerMode"`
	WorkflowTemplateID  string         `json:"workflowTemplateId"`
	ApprovalPolicyRef   string         `json:"approvalPolicyRef"`
	CooldownSeconds     int            `json:"cooldownSeconds"`
	ConcurrencyKey      string         `json:"concurrencyKey"`
	SafetyWindowSeconds int            `json:"safetyWindowSeconds"`
	Definition          map[string]any `json:"definition"`
	Enabled             bool           `json:"enabled"`
}

type HealingRunRequest struct {
	ID              string         `json:"id"`
	PolicyID        string         `json:"policyId"`
	EventID         string         `json:"eventId"`
	Status          string         `json:"status"`
	ApprovalStatus  string         `json:"approvalStatus"`
	ApprovalComment string         `json:"approvalComment"`
	RequestedBy     string         `json:"requestedBy"`
	ApprovedBy      string         `json:"approvedBy"`
	WorkflowRunID   string         `json:"workflowRunId"`
	Result          map[string]any `json:"result"`
	StartedAt       time.Time      `json:"startedAt"`
	CompletedAt     time.Time      `json:"completedAt"`
}

type OnCallScheduleRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TimeZone    string `json:"timeZone"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type OnCallRotationRequest struct {
	ID             string         `json:"id"`
	ScheduleID     string         `json:"scheduleId"`
	Name           string         `json:"name"`
	Participants   []string       `json:"participants"`
	RotationConfig map[string]any `json:"rotationConfig"`
	Enabled        bool           `json:"enabled"`
}

type OnCallEscalationPolicyRequest struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Steps   []map[string]any `json:"steps"`
	Enabled bool             `json:"enabled"`
}

type OnCallAssignmentRuleRequest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationID   string         `json:"integrationId"`
	IntegrationType string         `json:"integrationType"`
	BusinessLineID  string         `json:"businessLineId"`
	AlertCategory   string         `json:"alertCategory"`
	AlertName       string         `json:"alertName"`
	Severity        string         `json:"severity"`
	Service         string         `json:"service"`
	Role            string         `json:"role"`
	Matchers        map[string]any `json:"matchers"`
	TargetType      string         `json:"targetType"`
	TargetRef       string         `json:"targetRef"`
	RouteOrder      int            `json:"routeOrder"`
	GroupBy         []string       `json:"groupBy"`
	Priority        int            `json:"priority"`
	Enabled         bool           `json:"enabled"`
}
