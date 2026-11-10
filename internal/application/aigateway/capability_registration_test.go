package aigateway

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestCapabilityRegistrationRejectsDuplicateToolOwners(t *testing.T) {
	const duplicate = "docker.operations.get"
	builtin := BuiltinCapabilityProvider{}
	override := testCapabilityProvider{tools: []domain.ToolCapability{{Name: duplicate}}}
	for _, test := range []struct {
		name      string
		providers []CapabilityProvider
	}{
		{name: "builtin first", providers: []CapabilityProvider{builtin, override}},
		{name: "builtin last", providers: []CapabilityProvider{override, builtin}},
		{name: "same provider twice", providers: []CapabilityProvider{override, override}},
		{name: "repeated declaration", providers: []CapabilityProvider{testCapabilityProvider{tools: []domain.ToolCapability{{Name: duplicate}, {Name: duplicate}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{registry: newDefaultCapabilityRegistry()}
			err := service.SetCapabilityProviders(test.providers...)
			if !errors.Is(err, apperrors.ErrConflict) || !strings.Contains(err.Error(), duplicate) {
				t.Fatalf("duplicate registration returned %v", err)
			}
			if _, ok := service.gatewayRegistry().toolOwner(duplicate).(BuiltinCapabilityProvider); !ok {
				t.Fatal("failed replacement changed the registered owner")
			}
		})
	}
}

func TestCapabilityRegistrationRejectsPartialAddition(t *testing.T) {
	service := &Service{registry: newDefaultCapabilityRegistry()}
	valid := testCapabilityProvider{tools: []domain.ToolCapability{{Name: "custom.echo"}}}
	conflicting := testCapabilityProvider{tools: []domain.ToolCapability{{Name: "docker.operations.get"}}}
	if err := service.AddCapabilityProviders(valid, conflicting); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate registration returned %v", err)
	}
	if _, ok := service.toolByName("custom.echo"); ok {
		t.Fatal("failed addition published part of the batch")
	}
	if err := service.AddCapabilityProviders(valid); err != nil {
		t.Fatalf("valid tool from the rejected batch could not be registered: %v", err)
	}
}

func TestRegisteredCapabilityPlanKeysArePerOccurrenceAndRecoverable(t *testing.T) {
	service, input := capabilityPlanFixture()
	tools := service.gatewayTools()
	tools[0].RiskLevel = "write"
	tools[0].Execution = &domain.ToolExecutionContract{Mode: "sync", Idempotent: true, IdempotencyKeyField: "requestKey"}
	tools[0].InputSchema = gatewayObjectSchema([]string{"requestKey"}, map[string]any{"requestKey": map[string]any{"type": "string", "format": "uuid"}})
	input.Plan.Steps[0].Call.Input["requestKey"] = "88bc3d97-1c20-4df9-a422-a070a21b2ce1"
	if err := service.SetCapabilityProviders(testCapabilityProvider{tools: tools}); err != nil {
		t.Fatal(err)
	}
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
	if err := service.SetCapabilityProviders(testCapabilityProvider{tools: tools}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MaterializeRegisteredCapabilityPlan(ctx, principal, input); err == nil {
		t.Fatal("stale pinned capability accepted")
	}
}
