package handlers

import (
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestCopilotCapabilityInterfacesRemainFocused(t *testing.T) {
	interfaces := map[string]reflect.Type{
		"sessions":          reflect.TypeOf((*CopilotSessionService)(nil)).Elem(),
		"messages":          reflect.TypeOf((*CopilotMessageService)(nil)).Elem(),
		"streams":           reflect.TypeOf((*CopilotStreamService)(nil)).Elem(),
		"workbench":         reflect.TypeOf((*CopilotWorkbenchService)(nil)).Elem(),
		"data sources":      reflect.TypeOf((*CopilotDataSourceService)(nil)).Elem(),
		"analysis profiles": reflect.TypeOf((*CopilotAnalysisProfileService)(nil)).Elem(),
		"automation":        reflect.TypeOf((*CopilotAutomationService)(nil)).Elem(),
		"root cause":        reflect.TypeOf((*CopilotRootCauseService)(nil)).Elem(),
		"agent runs":        reflect.TypeOf((*CopilotAgentRunService)(nil)).Elem(),
		"inspection tasks":  reflect.TypeOf((*CopilotInspectionTaskService)(nil)).Elem(),
		"inspection runs":   reflect.TypeOf((*CopilotInspectionRunService)(nil)).Elem(),
	}
	for name, contract := range interfaces {
		if methods := contract.NumMethod(); methods > 7 {
			t.Errorf("%s capability has %d methods, want at most 7", name, methods)
		}
	}
}

func TestNewCopilotHandlerWithServicesComposesRouteHandlers(t *testing.T) {
	handler := NewCopilotHandlerWithServices(CopilotServices{}, keyring.Ring{})
	if handler == nil {
		t.Fatal("NewCopilotHandlerWithServices() returned nil")
	}
	registered := []gin.HandlerFunc{
		handler.ListSessions,
		handler.ListMessages,
		handler.StreamMessage,
		handler.ListInsights,
		handler.ListDataSources,
		handler.ListAutomationPolicies,
		handler.ListRootCauseRuns,
		handler.ListAgentRuns,
		handler.ListInspectionTasks,
		handler.ListInspectionRuns,
	}
	for index, routeHandler := range registered {
		if routeHandler == nil {
			t.Fatalf("composed route handler %d is nil", index)
		}
	}
}
