package handlers

import (
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMonitoringCapabilityInterfacesRemainFocused(t *testing.T) {
	interfaces := map[string]reflect.Type{
		"alerts":                 reflect.TypeOf((*AlertService)(nil)).Elem(),
		"channels":               reflect.TypeOf((*ChannelService)(nil)).Elem(),
		"routes":                 reflect.TypeOf((*AlertRouteService)(nil)).Elem(),
		"silences":               reflect.TypeOf((*SilenceService)(nil)).Elem(),
		"delivery logs":          reflect.TypeOf((*DeliveryLogService)(nil)).Elem(),
		"webhooks":               reflect.TypeOf((*WebhookService)(nil)).Elem(),
		"integrations":           reflect.TypeOf((*AlertIntegrationService)(nil)).Elem(),
		"rules":                  reflect.TypeOf((*AlertRuleService)(nil)).Elem(),
		"events":                 reflect.TypeOf((*AlertEventService)(nil)).Elem(),
		"healing runs":           reflect.TypeOf((*HealingRunService)(nil)).Elem(),
		"notification policies":  reflect.TypeOf((*NotificationPolicyService)(nil)).Elem(),
		"notification templates": reflect.TypeOf((*NotificationTemplateService)(nil)).Elem(),
		"healing policies":       reflect.TypeOf((*HealingPolicyService)(nil)).Elem(),
		"on-call schedules":      reflect.TypeOf((*OnCallScheduleService)(nil)).Elem(),
		"on-call rotations":      reflect.TypeOf((*OnCallRotationService)(nil)).Elem(),
		"on-call escalations":    reflect.TypeOf((*OnCallEscalationService)(nil)).Elem(),
		"on-call assignments":    reflect.TypeOf((*OnCallAssignmentService)(nil)).Elem(),
		"on-call runtime":        reflect.TypeOf((*OnCallRuntimeService)(nil)).Elem(),
	}
	for name, contract := range interfaces {
		if methods := contract.NumMethod(); methods > 7 {
			t.Errorf("%s capability has %d methods, want at most 7", name, methods)
		}
	}
}

func TestNewMonitoringHandlerComposesRouteHandlers(t *testing.T) {
	handler := NewMonitoringHandler(MonitoringDependencies{})
	if handler == nil {
		t.Fatal("NewMonitoringHandler() returned nil")
	}
	registered := []gin.HandlerFunc{
		handler.Summary,
		handler.ListRules,
		handler.ListEvents,
		handler.ListNotificationPolicies,
		handler.ListHealingRuns,
		handler.ListOnCallSchedules,
		handler.GetCurrentOnCall,
		handler.IngestIntegrationWebhook,
	}
	for index, routeHandler := range registered {
		if routeHandler == nil {
			t.Fatalf("composed route handler %d is nil", index)
		}
	}
}
