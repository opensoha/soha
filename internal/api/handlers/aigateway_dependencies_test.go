package handlers

import (
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAIGatewayCapabilityInterfacesRemainFocused(t *testing.T) {
	interfaces := map[string]reflect.Type{
		"capabilities":        reflect.TypeOf((*AIGatewayCapabilityService)(nil)).Elem(),
		"personal tokens":     reflect.TypeOf((*AIGatewayPersonalTokenService)(nil)).Elem(),
		"service accounts":    reflect.TypeOf((*AIGatewayServiceAccountService)(nil)).Elem(),
		"clients":             reflect.TypeOf((*AIGatewayClientService)(nil)).Elem(),
		"tool grants":         reflect.TypeOf((*AIGatewayToolGrantService)(nil)).Elem(),
		"access policies":     reflect.TypeOf((*AIGatewayAccessPolicyService)(nil)).Elem(),
		"governance":          reflect.TypeOf((*AIGatewayGovernanceService)(nil)).Elem(),
		"audit":               reflect.TypeOf((*AIGatewayAuditService)(nil)).Elem(),
		"approvals":           reflect.TypeOf((*AIGatewayApprovalService)(nil)).Elem(),
		"upstreams":           reflect.TypeOf((*AIGatewayUpstreamService)(nil)).Elem(),
		"model routes":        reflect.TypeOf((*AIGatewayModelRouteService)(nil)).Elem(),
		"relay observability": reflect.TypeOf((*AIGatewayRelayObservabilityService)(nil)).Elem(),
		"relay":               reflect.TypeOf((*AIGatewayRelayService)(nil)).Elem(),
	}
	for name, contract := range interfaces {
		if methods := contract.NumMethod(); methods > 7 {
			t.Errorf("%s capability has %d methods, want at most 7", name, methods)
		}
	}
}

func TestNewAIGatewayHandlerWithServicesComposesRouteHandlers(t *testing.T) {
	handler := NewAIGatewayHandlerWithServices(AIGatewayServices{})
	if handler == nil {
		t.Fatal("NewAIGatewayHandlerWithServices() returned nil")
	}
	registered := []gin.HandlerFunc{
		handler.Capabilities,
		handler.ListPersonalAccessTokens,
		handler.ListServiceAccounts,
		handler.ListAIClients,
		handler.ListApprovalRequests,
		handler.ListLLMUpstreams,
		handler.ListLLMCallLogs,
		handler.RelayOpenAIModels,
	}
	for index, routeHandler := range registered {
		if routeHandler == nil {
			t.Fatalf("composed route handler %d is nil", index)
		}
	}
}
