package monitoring

import (
	"context"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
)

type AlertReader interface {
	List(context.Context, domainalert.Filter) ([]domainalert.Instance, error)
	Get(context.Context, string) (domainalert.Instance, error)
	Summary(context.Context) (domainalert.Summary, error)
}

type AlertWriter interface {
	Upsert(context.Context, string, []domainalert.IngestAlert) ([]domainalert.Instance, error)
	UpdateOwnership(context.Context, string, domainalert.OwnershipInput) (domainalert.Instance, error)
	Acknowledge(context.Context, string, string, string) (domainalert.Instance, error)
}

type ChannelRepository interface {
	ListChannels(context.Context) ([]domainalert.NotificationChannel, error)
	CreateChannel(context.Context, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
	UpdateChannel(context.Context, string, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
}

type SilenceRepository interface {
	ListSilences(context.Context) ([]domainalert.AlertSilence, error)
	CreateSilence(context.Context, domainalert.SilenceInput) (domainalert.AlertSilence, error)
	UpdateSilence(context.Context, string, domainalert.SilenceInput) (domainalert.AlertSilence, error)
}

type DeliveryLogRepository interface {
	ListDeliveryLogs(context.Context, domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error)
	CreateDeliveryLog(context.Context, domainalert.DeliveryLog) error
}

type RuleRepository interface {
	ListRules(context.Context) ([]domainalert.AlertRule, error)
	GetRule(context.Context, string) (domainalert.AlertRule, error)
	CreateRule(context.Context, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	UpdateRule(context.Context, string, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
}

type RuleRunRepository interface {
	ListRuleRuns(context.Context, domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error)
	CreateRuleRun(context.Context, domainalert.AlertRuleRunInput) (domainalert.AlertRuleRun, error)
}

type AlertEventRepository interface {
	ListEvents(context.Context, domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error)
	GetEvent(context.Context, string) (domainalert.AlertEvent, error)
	CreateEvent(context.Context, domainalert.AlertEventInput) (domainalert.AlertEvent, error)
	UpdateEvent(context.Context, string, domainalert.AlertEventInput) (domainalert.AlertEvent, error)
}

type NotificationPolicyRepository interface {
	ListNotificationPolicies(context.Context) ([]domainalert.NotificationPolicy, error)
	CreateNotificationPolicy(context.Context, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	UpdateNotificationPolicy(context.Context, string, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
}

type NotificationTemplateRepository interface {
	ListNotificationTemplates(context.Context) ([]domainalert.NotificationTemplate, error)
	CreateNotificationTemplate(context.Context, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
	UpdateNotificationTemplate(context.Context, string, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
}

type HealingPolicyRepository interface {
	ListHealingPolicies(context.Context) ([]domainalert.HealingPolicy, error)
	GetHealingPolicy(context.Context, string) (domainalert.HealingPolicy, error)
	CreateHealingPolicy(context.Context, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
	UpdateHealingPolicy(context.Context, string, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
}

type HealingRunRepository interface {
	ListHealingRuns(context.Context, domainalert.HealingRunFilter) ([]domainalert.HealingRun, error)
	GetHealingRun(context.Context, string) (domainalert.HealingRun, error)
	CreateHealingRun(context.Context, domainalert.HealingRunInput) (domainalert.HealingRun, error)
	UpdateHealingRun(context.Context, string, domainalert.HealingRunInput) (domainalert.HealingRun, error)
}

type OnCallScheduleRepository interface {
	ListOnCallSchedules(context.Context) ([]domainalert.OnCallSchedule, error)
	CreateOnCallSchedule(context.Context, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
	UpdateOnCallSchedule(context.Context, string, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
}

type OnCallRotationRepository interface {
	ListOnCallRotations(context.Context) ([]domainalert.OnCallRotation, error)
	CreateOnCallRotation(context.Context, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
	UpdateOnCallRotation(context.Context, string, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
}

type OnCallEscalationRepository interface {
	ListOnCallEscalationPolicies(context.Context) ([]domainalert.OnCallEscalationPolicy, error)
	CreateOnCallEscalationPolicy(context.Context, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
	UpdateOnCallEscalationPolicy(context.Context, string, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
}

type OnCallAssignmentRepository interface {
	ListOnCallAssignmentRules(context.Context) ([]domainalert.OnCallAssignmentRule, error)
	CreateOnCallAssignmentRule(context.Context, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
	UpdateOnCallAssignmentRule(context.Context, string, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
}

type AlertIntegrationRepository interface {
	ListAlertIntegrations(context.Context) ([]domainalert.AlertIntegration, error)
	GetAlertIntegration(context.Context, string) (domainalert.AlertIntegration, error)
	CreateAlertIntegration(context.Context, domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error)
	UpdateAlertIntegration(context.Context, string, domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error)
	UpdateAlertIntegrationStatus(context.Context, string, domainalert.AlertIntegrationStatusInput) (domainalert.AlertIntegration, error)
}
