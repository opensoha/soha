package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
	"testing"

	appvm "github.com/opensoha/soha/internal/application/virtualization"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvm "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type virtualizationCapabilityFixture struct {
	VirtualizationCapabilityService
	input       appvm.CreateVMInput
	task        domainvm.Task
	writes      int
	recoveryErr error
}

func (f *virtualizationCapabilityFixture) CreateVM(_ context.Context, _ domainidentity.Principal, input appvm.CreateVMInput) (domainvm.Task, error) {
	f.input = input
	f.writes++
	return f.task, nil
}
func (f *virtualizationCapabilityFixture) FindVMCreation(_ context.Context, _ domainidentity.Principal, input appvm.CreateVMInput) (domainvm.Task, error) {
	f.input = input
	return f.task, f.recoveryErr
}

func TestVirtualizationCapabilitiesValidateRecoverAndRedact(t *testing.T) {
	fixture := &virtualizationCapabilityFixture{task: domainvm.Task{ID: "op-1", ConnectionID: "conn-1", TaskKind: "vm_create", Status: "queued", Payload: map[string]any{"cloudInit": "secret-bootstrap"}, Result: map[string]any{"error": "secret-provider-response"}}}
	registered, err := NewVirtualizationCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := registered.(*virtualizationCapabilityProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *virtualizationCapabilityProvider", registered)
	}
	registry := newCapabilityRegistry(provider, BuiltinCapabilityProvider{})
	tool, ok := registry.ToolByName("virtualization.vms.create.trigger")
	if !ok || tool.Version != "1" || tool.Execution.TaskKind != "virtualization.operation" {
		t.Fatalf("wrong registered capability: %+v", tool)
	}
	input := map[string]any{"connectionId": "conn-1", "name": "worker", "idempotencyKey": "worker-request-1", "requireCapacity": true, "cloudInit": "secret-bootstrap"}
	if err := validateCapabilityInput(tool, input); err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{"connectionId": "conn-1", "name": "worker", "idempotencyKey": "tiny"}
	if err := validateCapabilityInput(tool, bad); err == nil {
		t.Fatal("unbounded or missing receipt key accepted")
	}
	output, _, err := provider.InvokeTool(context.Background(), domainidentity.Principal{}, tool, input)
	if err != nil || fixture.input.IdempotencyKey != "worker-request-1" || !fixture.input.RequireCapacity {
		t.Fatalf("create input: %+v %v", fixture.input, err)
	}
	raw, _ := json.Marshal(output)
	if strings.Contains(string(raw), "secret-") || strings.Contains(string(raw), "payload") {
		t.Fatalf("sensitive task escaped: %s", raw)
	}
	ref := provider.TaskReference(tool, output, output)
	if ref == nil || ref.ID != "op-1" || ref.Terminal || ref.StatusCall.ToolName != "virtualization.operations.get" {
		t.Fatalf("task reference: %+v", ref)
	}
	if provider.TaskReference(tool, output, map[string]any{"id": "op-1", "status": "queued"}) != nil {
		t.Fatal("task reference restored redacted outcome")
	}
	verifyVirtualizationCapabilityRecovery(t, provider, fixture, tool, input)
}

func verifyVirtualizationCapabilityRecovery(t *testing.T, provider *virtualizationCapabilityProvider, fixture *virtualizationCapabilityFixture, tool domainai.ToolCapability, input map[string]any) {
	t.Helper()
	recovered, found, err := provider.RecoverTool(context.Background(), domainidentity.Principal{}, tool, input)
	if err != nil || !found || recovered == nil || fixture.writes != 1 {
		t.Fatalf("lost reply repeated creation: %v %v writes=%d", found, err, fixture.writes)
	}
	fixture.recoveryErr = apperrors.ErrNotFound
	if _, found, err := provider.RecoverTool(context.Background(), domainidentity.Principal{}, tool, input); found || err != nil {
		t.Fatalf("missing recovery: %v %v", found, err)
	}
	fixture.recoveryErr = apperrors.ErrConflict
	if _, found, err := provider.RecoverTool(context.Background(), domainidentity.Principal{}, tool, input); found || !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflicting recovery: %v %v", found, err)
	}
}

func TestVirtualizationTaskOutcomeRequiresProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		status, effect, vm, outcome string
		confirmed, terminal         bool
	}{
		{"canceling", "unknown", "", "unknown", false, false},
		{"canceled", "unknown", "", "unknown", false, true},
		{"canceled", "created", "vm-1", "canceled", true, true},
		{"completed", "created", "vm-1", "succeeded", false, true},
		{"completed", "", "vm-1", "unknown", false, true},
		{"failed", "unknown", "", "unknown", false, true},
		{"failed", "not_started", "", "failed", false, true},
	} {
		output := virtualizationTaskOutput(domainvm.Task{ID: "op", TaskKind: "vm_create", Status: tc.status, VMID: tc.vm, Result: map[string]any{"providerEffect": tc.effect, "cancellationConfirmed": tc.confirmed}})
		if output["outcome"] != tc.outcome || output["terminal"] != tc.terminal {
			t.Fatalf("%+v: %+v", tc, output)
		}
	}
}

func TestVirtualizationScopeCheckRejectsChangedTargets(t *testing.T) {
	tool := "virtualization.vms.create.trigger"
	ctx := context.WithValue(context.Background(), capabilityScopeContextKey{}, capabilityScopeContext{tool: tool, scopes: []map[string]string{{"virtualizationConnectionId": "conn", "namespace": "team-a", "connectionRevision": "v1"}}})
	ctx = withVirtualizationScopeCheck(ctx, tool)
	if err := domainvm.CheckScope(ctx, map[string]string{"virtualizationConnectionId": "conn", "namespace": "team-a", "connectionRevision": "v1"}); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []map[string]string{{"virtualizationConnectionId": "other"}, {"virtualizationConnectionId": "conn", "namespace": "team-b"}, {"virtualizationConnectionId": "conn", "connectionRevision": "v2"}} {
		if err := domainvm.CheckScope(ctx, scope); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("changed scope accepted: %v %v", scope, err)
		}
	}
}

var _ ToolRecoveryProvider = (*virtualizationCapabilityProvider)(nil)
var _ ToolTaskReferenceProvider = (*virtualizationCapabilityProvider)(nil)

func TestCapacityPlacementBindingKeepsOwningConnection(t *testing.T) {
	provider, err := NewVirtualizationCapabilityProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	service := contractTestService(&memoryGatewayRepository{}, nil)
	service.SetCapabilityProviders(provider)
	step := domainai.CapabilityPlanStep{ID: "create", Call: sohaapi.CapabilityCall{ToolName: "virtualization.vms.create.trigger", CapabilityVersion: "1", Input: map[string]any{"connectionId": "pool-a", "name": "worker", "idempotencyKey": "supply-worker-1", "requireCapacity": true}}, DependsOn: []string{"capacity"}, Bindings: []domainai.CapabilityInputBinding{{InputPath: "/node", StepID: "capacity", OutputPath: "/node"}, {InputPath: "/providerParams/storage", StepID: "capacity", OutputPath: "/storage"}}}
	plan := domainai.CapabilityPlan{Steps: []domainai.CapabilityPlanStep{{ID: "capacity", Call: sohaapi.CapabilityCall{ToolName: "virtualization.capacity.check", CapabilityVersion: "1"}}, step}}
	result := map[string]domainai.ToolInvocationResult{"capacity": {Result: "success", Output: map[string]any{"connectionId": "pool-a", "node": "node-a", "storage": "disk-a"}}}
	call, err := service.ResolveCapabilityStep(step, plan, result)
	if err != nil || call.Input["node"] != "node-a" {
		t.Fatalf("supported binding: %+v %v", call, err)
	}
	result["capacity"] = domainai.ToolInvocationResult{Result: "success", Output: map[string]any{"connectionId": "pool-b", "node": "node-a", "storage": "disk-a"}}
	if _, err := service.ResolveCapabilityStep(step, plan, result); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cross-pool placement binding accepted: %v", err)
	}
}
