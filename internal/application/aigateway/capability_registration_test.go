package aigateway

import (
	"context"
	"reflect"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/aigateway"
)

func TestRegisteredCapabilityPlanKeysArePerOccurrenceAndRecoverable(t *testing.T) {
	service, input := capabilityPlanFixture()
	tools := service.gatewayTools()
	tools[0].RiskLevel = "write"
	tools[0].Execution = &domain.ToolExecutionContract{Mode: "sync", Idempotent: true, IdempotencyKeyField: "requestKey"}
	tools[0].InputSchema = gatewayObjectSchema([]string{"requestKey"}, map[string]any{"requestKey": map[string]any{"type": "string", "format": "uuid"}})
	input.Plan.Steps[0].Call.Input["requestKey"] = "88bc3d97-1c20-4df9-a422-a070a21b2ce1"
	service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
	ctx, principal := context.Background(), testPrincipal("developer")
	first, err := service.MaterializeRegisteredCapabilityPlan(ctx, principal, input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.MaterializeRegisteredCapabilityPlan(ctx, principal, input)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("receipt keys changed: %+v %v", replay, err)
	}
	if first.Plan.Steps[0].Call.Input["requestKey"] == input.Plan.Steps[0].Call.Input["requestKey"] {
		t.Fatal("registered literal reused across occurrences")
	}
	input.IdempotencyKey = "next-occurrence"
	next, err := service.MaterializeRegisteredCapabilityPlan(ctx, principal, input)
	if err != nil || next.Plan.Steps[0].Call.Input["requestKey"] == first.Plan.Steps[0].Call.Input["requestKey"] {
		t.Fatalf("different occurrences shared keys: %v", err)
	}
	if !reflect.DeepEqual(first.Plan.Steps[1], input.Plan.Steps[1]) {
		t.Fatal("materialization changed evidence bindings")
	}
	tools[0].Version = "2"
	service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
	if _, err := service.MaterializeRegisteredCapabilityPlan(ctx, principal, input); err == nil {
		t.Fatal("stale pinned capability accepted")
	}
}
