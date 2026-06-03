package alert

import (
	"context"
	"time"
)

type Instance struct {
	ID                 string            `json:"id"`
	Source             string            `json:"source"`
	Fingerprint        string            `json:"fingerprint"`
	Title              string            `json:"title"`
	Summary            string            `json:"summary"`
	Severity           string            `json:"severity"`
	Status             string            `json:"status"`
	ClusterID          string            `json:"clusterId,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Annotations        map[string]string `json:"annotations,omitempty"`
	Receiver           string            `json:"receiver,omitempty"`
	GeneratorURL       string            `json:"generatorUrl,omitempty"`
	StartsAt           time.Time         `json:"startsAt,omitempty"`
	EndsAt             time.Time         `json:"endsAt,omitempty"`
	LastSeenAt         time.Time         `json:"lastSeenAt"`
	OwnerTeam          string            `json:"ownerTeam,omitempty"`
	Assignee           string            `json:"assignee,omitempty"`
	AcknowledgedAt     time.Time         `json:"acknowledgedAt,omitempty"`
	AcknowledgedBy     string            `json:"acknowledgedBy,omitempty"`
	AcknowledgedByName string            `json:"acknowledgedByName,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
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

type AlertIntegration struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationType string         `json:"integrationType"`
	Description     string         `json:"description,omitempty"`
	Token           string         `json:"token,omitempty"`
	TokenPreview    string         `json:"tokenPreview,omitempty"`
	WebhookPath     string         `json:"webhookPath,omitempty"`
	LabelMapping    map[string]any `json:"labelMapping,omitempty"`
	DedupeConfig    map[string]any `json:"dedupeConfig,omitempty"`
	Enabled         bool           `json:"enabled"`
	Status          string         `json:"status"`
	LastError       string         `json:"lastError,omitempty"`
	LastReceivedAt  time.Time      `json:"lastReceivedAt,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type AlertIntegrationInput struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationType string         `json:"integrationType"`
	Description     string         `json:"description,omitempty"`
	Token           string         `json:"token,omitempty"`
	LabelMapping    map[string]any `json:"labelMapping,omitempty"`
	DedupeConfig    map[string]any `json:"dedupeConfig,omitempty"`
	Enabled         bool           `json:"enabled"`
}

type AlertIntegrationStatusInput struct {
	Status         string    `json:"status"`
	LastError      string    `json:"lastError,omitempty"`
	LastReceivedAt time.Time `json:"lastReceivedAt,omitempty"`
}

type AlertIntegrationTestInput struct {
	IntegrationType string         `json:"integrationType"`
	LabelMapping    map[string]any `json:"labelMapping,omitempty"`
	DedupeConfig    map[string]any `json:"dedupeConfig,omitempty"`
	Payload         map[string]any `json:"payload"`
}

type AlertIntegrationTestResult struct {
	IntegrationType string        `json:"integrationType"`
	Source          string        `json:"source"`
	AcceptedCount   int           `json:"acceptedCount"`
	Alerts          []IngestAlert `json:"alerts"`
	Summary         string        `json:"summary,omitempty"`
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
	ListRules(context.Context) ([]AlertRule, error)
	GetRule(context.Context, string) (AlertRule, error)
	CreateRule(context.Context, AlertRuleInput) (AlertRule, error)
	UpdateRule(context.Context, string, AlertRuleInput) (AlertRule, error)
	ListRuleRuns(context.Context, AlertRuleRunFilter) ([]AlertRuleRun, error)
	CreateRuleRun(context.Context, AlertRuleRunInput) (AlertRuleRun, error)
	ListEvents(context.Context, AlertEventFilter) ([]AlertEvent, error)
	GetEvent(context.Context, string) (AlertEvent, error)
	CreateEvent(context.Context, AlertEventInput) (AlertEvent, error)
	UpdateEvent(context.Context, string, AlertEventInput) (AlertEvent, error)
	ListNotificationPolicies(context.Context) ([]NotificationPolicy, error)
	CreateNotificationPolicy(context.Context, NotificationPolicyInput) (NotificationPolicy, error)
	UpdateNotificationPolicy(context.Context, string, NotificationPolicyInput) (NotificationPolicy, error)
	ListNotificationTemplates(context.Context) ([]NotificationTemplate, error)
	CreateNotificationTemplate(context.Context, NotificationTemplateInput) (NotificationTemplate, error)
	UpdateNotificationTemplate(context.Context, string, NotificationTemplateInput) (NotificationTemplate, error)
	ListHealingPolicies(context.Context) ([]HealingPolicy, error)
	GetHealingPolicy(context.Context, string) (HealingPolicy, error)
	CreateHealingPolicy(context.Context, HealingPolicyInput) (HealingPolicy, error)
	UpdateHealingPolicy(context.Context, string, HealingPolicyInput) (HealingPolicy, error)
	ListHealingRuns(context.Context, HealingRunFilter) ([]HealingRun, error)
	GetHealingRun(context.Context, string) (HealingRun, error)
	CreateHealingRun(context.Context, HealingRunInput) (HealingRun, error)
	UpdateHealingRun(context.Context, string, HealingRunInput) (HealingRun, error)
	ListOnCallSchedules(context.Context) ([]OnCallSchedule, error)
	CreateOnCallSchedule(context.Context, OnCallScheduleInput) (OnCallSchedule, error)
	UpdateOnCallSchedule(context.Context, string, OnCallScheduleInput) (OnCallSchedule, error)
	ListOnCallRotations(context.Context) ([]OnCallRotation, error)
	CreateOnCallRotation(context.Context, OnCallRotationInput) (OnCallRotation, error)
	UpdateOnCallRotation(context.Context, string, OnCallRotationInput) (OnCallRotation, error)
	ListOnCallEscalationPolicies(context.Context) ([]OnCallEscalationPolicy, error)
	CreateOnCallEscalationPolicy(context.Context, OnCallEscalationPolicyInput) (OnCallEscalationPolicy, error)
	UpdateOnCallEscalationPolicy(context.Context, string, OnCallEscalationPolicyInput) (OnCallEscalationPolicy, error)
	ListOnCallAssignmentRules(context.Context) ([]OnCallAssignmentRule, error)
	CreateOnCallAssignmentRule(context.Context, OnCallAssignmentRuleInput) (OnCallAssignmentRule, error)
	UpdateOnCallAssignmentRule(context.Context, string, OnCallAssignmentRuleInput) (OnCallAssignmentRule, error)
	ListAlertIntegrations(context.Context) ([]AlertIntegration, error)
	GetAlertIntegration(context.Context, string) (AlertIntegration, error)
	CreateAlertIntegration(context.Context, AlertIntegrationInput) (AlertIntegration, error)
	UpdateAlertIntegration(context.Context, string, AlertIntegrationInput) (AlertIntegration, error)
	UpdateAlertIntegrationStatus(context.Context, string, AlertIntegrationStatusInput) (AlertIntegration, error)
}

type AlertRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	RuleType             string            `json:"ruleType"`
	DatasourceSelector   map[string]any    `json:"datasourceSelector,omitempty"`
	QuerySpec            map[string]any    `json:"querySpec,omitempty"`
	ThresholdSpec        map[string]any    `json:"thresholdSpec,omitempty"`
	ForSeconds           int               `json:"forSeconds"`
	GroupBy              []string          `json:"groupBy,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	Annotations          map[string]string `json:"annotations,omitempty"`
	NotificationPolicyID string            `json:"notificationPolicyId,omitempty"`
	HealingPolicyIDs     []string          `json:"healingPolicyIds,omitempty"`
	Enabled              bool              `json:"enabled"`
	CreatedAt            time.Time         `json:"createdAt"`
	UpdatedAt            time.Time         `json:"updatedAt"`
}

type AlertRuleInput struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	RuleType             string            `json:"ruleType"`
	DatasourceSelector   map[string]any    `json:"datasourceSelector,omitempty"`
	QuerySpec            map[string]any    `json:"querySpec,omitempty"`
	ThresholdSpec        map[string]any    `json:"thresholdSpec,omitempty"`
	ForSeconds           int               `json:"forSeconds"`
	GroupBy              []string          `json:"groupBy,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	Annotations          map[string]string `json:"annotations,omitempty"`
	NotificationPolicyID string            `json:"notificationPolicyId,omitempty"`
	HealingPolicyIDs     []string          `json:"healingPolicyIds,omitempty"`
	Enabled              bool              `json:"enabled"`
}

type AlertEvent struct {
	ID                 string            `json:"id"`
	RuleID             string            `json:"ruleId,omitempty"`
	SourceType         string            `json:"sourceType"`
	SourceSystem       string            `json:"sourceSystem,omitempty"`
	Fingerprint        string            `json:"fingerprint"`
	Title              string            `json:"title"`
	Summary            string            `json:"summary"`
	Severity           string            `json:"severity"`
	Status             string            `json:"status"`
	ClusterID          string            `json:"clusterId,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Annotations        map[string]string `json:"annotations,omitempty"`
	Receiver           string            `json:"receiver,omitempty"`
	GeneratorURL       string            `json:"generatorUrl,omitempty"`
	CurrentState       string            `json:"currentState,omitempty"`
	LastNotificationAt time.Time         `json:"lastNotificationAt,omitempty"`
	StartsAt           time.Time         `json:"startsAt,omitempty"`
	EndsAt             time.Time         `json:"endsAt,omitempty"`
	LastSeenAt         time.Time         `json:"lastSeenAt"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type AlertEventInput struct {
	ID                 string            `json:"id"`
	RuleID             string            `json:"ruleId,omitempty"`
	SourceType         string            `json:"sourceType"`
	SourceSystem       string            `json:"sourceSystem,omitempty"`
	Fingerprint        string            `json:"fingerprint"`
	Title              string            `json:"title"`
	Summary            string            `json:"summary"`
	Severity           string            `json:"severity"`
	Status             string            `json:"status"`
	ClusterID          string            `json:"clusterId,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Annotations        map[string]string `json:"annotations,omitempty"`
	Receiver           string            `json:"receiver,omitempty"`
	GeneratorURL       string            `json:"generatorUrl,omitempty"`
	CurrentState       string            `json:"currentState,omitempty"`
	LastNotificationAt time.Time         `json:"lastNotificationAt,omitempty"`
	StartsAt           time.Time         `json:"startsAt,omitempty"`
	EndsAt             time.Time         `json:"endsAt,omitempty"`
	LastSeenAt         time.Time         `json:"lastSeenAt"`
}

type AlertEventFilter struct {
	Status    string
	RuleID    string
	ClusterID string
	Limit     int
}

type NotificationPolicy struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Matchers        map[string]any `json:"matchers,omitempty"`
	ProcessorChain  []string       `json:"processorChain,omitempty"`
	ChannelRefs     []string       `json:"channelRefs,omitempty"`
	OnCallRef       string         `json:"oncallRef,omitempty"`
	SendResolved    bool           `json:"sendResolved"`
	CooldownSeconds int            `json:"cooldownSeconds"`
	Enabled         bool           `json:"enabled"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type NotificationPolicyInput struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Matchers        map[string]any `json:"matchers,omitempty"`
	ProcessorChain  []string       `json:"processorChain,omitempty"`
	ChannelRefs     []string       `json:"channelRefs,omitempty"`
	OnCallRef       string         `json:"oncallRef,omitempty"`
	SendResolved    bool           `json:"sendResolved"`
	CooldownSeconds int            `json:"cooldownSeconds"`
	Enabled         bool           `json:"enabled"`
}

type NotificationTemplate struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	TemplateType  string         `json:"templateType"`
	ContentType   string         `json:"contentType"`
	BodyTemplate  string         `json:"bodyTemplate,omitempty"`
	Headers       map[string]any `json:"headers,omitempty"`
	QueryParams   map[string]any `json:"queryParams,omitempty"`
	SamplePayload map[string]any `json:"samplePayload,omitempty"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type NotificationTemplateInput struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	TemplateType  string         `json:"templateType"`
	ContentType   string         `json:"contentType"`
	BodyTemplate  string         `json:"bodyTemplate,omitempty"`
	Headers       map[string]any `json:"headers,omitempty"`
	QueryParams   map[string]any `json:"queryParams,omitempty"`
	SamplePayload map[string]any `json:"samplePayload,omitempty"`
	Enabled       bool           `json:"enabled"`
}

type HealingPolicy struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	TriggerMode         string         `json:"triggerMode"`
	WorkflowTemplateID  string         `json:"workflowTemplateId"`
	ApprovalPolicyRef   string         `json:"approvalPolicyRef,omitempty"`
	CooldownSeconds     int            `json:"cooldownSeconds"`
	ConcurrencyKey      string         `json:"concurrencyKey,omitempty"`
	SafetyWindowSeconds int            `json:"safetyWindowSeconds"`
	Definition          map[string]any `json:"definition,omitempty"`
	Enabled             bool           `json:"enabled"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type HealingPolicyInput struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	TriggerMode         string         `json:"triggerMode"`
	WorkflowTemplateID  string         `json:"workflowTemplateId"`
	ApprovalPolicyRef   string         `json:"approvalPolicyRef,omitempty"`
	CooldownSeconds     int            `json:"cooldownSeconds"`
	ConcurrencyKey      string         `json:"concurrencyKey,omitempty"`
	SafetyWindowSeconds int            `json:"safetyWindowSeconds"`
	Definition          map[string]any `json:"definition,omitempty"`
	Enabled             bool           `json:"enabled"`
}

type HealingRun struct {
	ID              string         `json:"id"`
	PolicyID        string         `json:"policyId"`
	EventID         string         `json:"eventId,omitempty"`
	Status          string         `json:"status"`
	ApprovalStatus  string         `json:"approvalStatus,omitempty"`
	ApprovalComment string         `json:"approvalComment,omitempty"`
	RequestedBy     string         `json:"requestedBy,omitempty"`
	ApprovedBy      string         `json:"approvedBy,omitempty"`
	WorkflowRunID   string         `json:"workflowRunId,omitempty"`
	WorkflowStatus  string         `json:"workflowStatus,omitempty"`
	WorkflowSummary string         `json:"workflowSummary,omitempty"`
	Result          map[string]any `json:"result,omitempty"`
	StartedAt       time.Time      `json:"startedAt,omitempty"`
	CompletedAt     time.Time      `json:"completedAt,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type HealingRunInput struct {
	ID              string         `json:"id"`
	PolicyID        string         `json:"policyId"`
	EventID         string         `json:"eventId,omitempty"`
	Status          string         `json:"status"`
	ApprovalStatus  string         `json:"approvalStatus,omitempty"`
	ApprovalComment string         `json:"approvalComment,omitempty"`
	RequestedBy     string         `json:"requestedBy,omitempty"`
	ApprovedBy      string         `json:"approvedBy,omitempty"`
	WorkflowRunID   string         `json:"workflowRunId,omitempty"`
	WorkflowStatus  string         `json:"workflowStatus,omitempty"`
	WorkflowSummary string         `json:"workflowSummary,omitempty"`
	Result          map[string]any `json:"result,omitempty"`
	StartedAt       time.Time      `json:"startedAt,omitempty"`
	CompletedAt     time.Time      `json:"completedAt,omitempty"`
}

type HealingRunFilter struct {
	PolicyID string
	EventID  string
	Status   string
	Limit    int
}

type OnCallSchedule struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TimeZone    string    `json:"timeZone,omitempty"`
	Description string    `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type OnCallScheduleInput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TimeZone    string `json:"timeZone,omitempty"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
}

type OnCallRotation struct {
	ID             string         `json:"id"`
	ScheduleID     string         `json:"scheduleId"`
	Name           string         `json:"name"`
	Participants   []string       `json:"participants,omitempty"`
	RotationConfig map[string]any `json:"rotationConfig,omitempty"`
	Enabled        bool           `json:"enabled"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type OnCallRotationInput struct {
	ID             string         `json:"id"`
	ScheduleID     string         `json:"scheduleId"`
	Name           string         `json:"name"`
	Participants   []string       `json:"participants,omitempty"`
	RotationConfig map[string]any `json:"rotationConfig,omitempty"`
	Enabled        bool           `json:"enabled"`
}

type OnCallEscalationPolicy struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Steps     []map[string]any `json:"steps,omitempty"`
	Enabled   bool             `json:"enabled"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

type OnCallEscalationPolicyInput struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Steps   []map[string]any `json:"steps,omitempty"`
	Enabled bool             `json:"enabled"`
}

type OnCallAssignmentRule struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationID   string         `json:"integrationId,omitempty"`
	IntegrationType string         `json:"integrationType,omitempty"`
	BusinessLineID  string         `json:"businessLineId,omitempty"`
	AlertCategory   string         `json:"alertCategory,omitempty"`
	AlertName       string         `json:"alertName,omitempty"`
	Severity        string         `json:"severity,omitempty"`
	Service         string         `json:"service,omitempty"`
	Role            string         `json:"role,omitempty"`
	Matchers        map[string]any `json:"matchers,omitempty"`
	TargetType      string         `json:"targetType"`
	TargetRef       string         `json:"targetRef"`
	RouteOrder      int            `json:"routeOrder"`
	GroupBy         []string       `json:"groupBy,omitempty"`
	Priority        int            `json:"priority"`
	Enabled         bool           `json:"enabled"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type OnCallAssignmentRuleInput struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	IntegrationID   string         `json:"integrationId,omitempty"`
	IntegrationType string         `json:"integrationType,omitempty"`
	BusinessLineID  string         `json:"businessLineId,omitempty"`
	AlertCategory   string         `json:"alertCategory,omitempty"`
	AlertName       string         `json:"alertName,omitempty"`
	Severity        string         `json:"severity,omitempty"`
	Service         string         `json:"service,omitempty"`
	Role            string         `json:"role,omitempty"`
	Matchers        map[string]any `json:"matchers,omitempty"`
	TargetType      string         `json:"targetType"`
	TargetRef       string         `json:"targetRef"`
	RouteOrder      int            `json:"routeOrder"`
	GroupBy         []string       `json:"groupBy,omitempty"`
	Priority        int            `json:"priority"`
	Enabled         bool           `json:"enabled"`
}

type OnCallResolveInput struct {
	AlertID         string            `json:"alertId,omitempty"`
	IntegrationID   string            `json:"integrationId,omitempty"`
	IntegrationType string            `json:"integrationType,omitempty"`
	BusinessLineID  string            `json:"businessLineId,omitempty"`
	AlertCategory   string            `json:"alertCategory,omitempty"`
	AlertName       string            `json:"alertName,omitempty"`
	Severity        string            `json:"severity,omitempty"`
	Service         string            `json:"service,omitempty"`
	Role            string            `json:"role,omitempty"`
	ClusterID       string            `json:"clusterId,omitempty"`
	Namespace       string            `json:"namespace,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
}

type OnCallTask struct {
	ID                 string            `json:"id"`
	EventID            string            `json:"eventId"`
	Title              string            `json:"title"`
	Summary            string            `json:"summary,omitempty"`
	Severity           string            `json:"severity"`
	Status             string            `json:"status"`
	IntegrationID      string            `json:"integrationId,omitempty"`
	IntegrationType    string            `json:"integrationType,omitempty"`
	ClusterID          string            `json:"clusterId,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Service            string            `json:"service,omitempty"`
	BusinessLineID     string            `json:"businessLineId,omitempty"`
	RouteID            string            `json:"routeId,omitempty"`
	RouteName          string            `json:"routeName,omitempty"`
	GroupKey           string            `json:"groupKey,omitempty"`
	GroupBy            []string          `json:"groupBy,omitempty"`
	TargetType         string            `json:"targetType,omitempty"`
	TargetRef          string            `json:"targetRef,omitempty"`
	CurrentParticipant string            `json:"currentParticipant,omitempty"`
	Participants       []string          `json:"participants,omitempty"`
	ResolutionStatus   string            `json:"resolutionStatus"`
	Labels             map[string]string `json:"labels,omitempty"`
	LastSeenAt         time.Time         `json:"lastSeenAt"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type RuleTestResult struct {
	RuleID              string           `json:"ruleId,omitempty"`
	RuleType            string           `json:"ruleType,omitempty"`
	Summary             string           `json:"summary,omitempty"`
	Matched             bool             `json:"matched"`
	Samples             []map[string]any `json:"samples,omitempty"`
	DataSources         []string         `json:"dataSources,omitempty"`
	NotificationPreview []map[string]any `json:"notificationPreview,omitempty"`
	ExecutedAt          time.Time        `json:"executedAt"`
}

type AlertRuleRun struct {
	ID         string         `json:"id"`
	RuleID     string         `json:"ruleId"`
	Status     string         `json:"status"`
	Summary    string         `json:"summary,omitempty"`
	Matched    bool           `json:"matched"`
	DurationMs int            `json:"durationMs"`
	Error      string         `json:"error,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type AlertRuleRunInput struct {
	ID         string         `json:"id"`
	RuleID     string         `json:"ruleId"`
	Status     string         `json:"status"`
	Summary    string         `json:"summary,omitempty"`
	Matched    bool           `json:"matched"`
	DurationMs int            `json:"durationMs"`
	Error      string         `json:"error,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
}

type AlertRuleRunFilter struct {
	RuleID string
	Limit  int
}
