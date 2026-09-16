package aigateway

import (
	"context"
	"errors"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func capabilityPlanFixture() (*Service, domainaigateway.CapabilityTaskInput) {
	service := contractTestService(&memoryGatewayRepository{}, nil)
	tools := []domainaigateway.ToolCapability{
		{Name: "test.lookup", Version: "1", RiskLevel: "read", Domain: "test", Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}, InputSchema: gatewayObjectSchema(nil, map[string]any{}), OutputSemantics: []domainaigateway.CapabilityValueSemantic{{Path: "/id", Kind: "docker.host"}}},
		{Name: "test.assess", Version: "1", RiskLevel: "read", Domain: "test", ProducesAssessment: true, Execution: &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true}, InputSchema: gatewayObjectSchema([]string{"hostId"}, map[string]any{"hostId": map[string]any{"type": "string"}}), InputSemantics: []domainaigateway.CapabilityValueSemantic{{Path: "/hostId", Kind: "docker.host"}}},
	}
	service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
	plan := domainaigateway.CapabilityPlan{Goal: "Assess a discovered Docker host", VerificationSteps: []string{"assess"}, Steps: []domainaigateway.CapabilityPlanStep{
		{ID: "lookup", Call: sohaapi.CapabilityCall{ToolName: "test.lookup", CapabilityVersion: "1", Input: map[string]any{}}},
		{ID: "assess", Call: sohaapi.CapabilityCall{ToolName: "test.assess", CapabilityVersion: "1", Input: map[string]any{}}, DependsOn: []string{"lookup"}, Bindings: []sohaapi.CapabilityInputBinding{{InputPath: "/hostId", StepID: "lookup", OutputPath: "/id"}}},
	}}
	return service, domainaigateway.CapabilityTaskInput{IdempotencyKey: "plan-1", Plan: plan}
}

func TestCapabilityPlanRejectsUnsafeComposition(t *testing.T) {
	cases := []string{"valid", "wrong-kind", "wrong-unit", "missing-dependency", "cycle", "duplicate", "stale-version", "secret", "overwritten-input", "no-assessment", "wrong-shape"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			service, input := capabilityPlanFixture()
			tools := service.gatewayTools()
			switch name {
			case "wrong-kind":
				tools[1].InputSemantics[0].Kind = "application.service"
			case "wrong-unit":
				tools[1].InputSemantics[0].Unit = "milliseconds"
			case "missing-dependency":
				input.Plan.Steps[1].DependsOn = nil
			case "cycle":
				input.Plan.Steps[0].DependsOn = []string{"assess"}
			case "duplicate":
				input.Plan.Steps[1].ID = "lookup"
			case "stale-version":
				input.Plan.Steps[0].Call.CapabilityVersion = "0"
			case "secret":
				input.Plan.Steps[0].Call.Input["password"] = "do-not-persist"
			case "overwritten-input":
				input.Plan.Steps[1].Call.Input["hostId"] = "literal"
			case "no-assessment":
				tools[1].ProducesAssessment = false
			case "wrong-shape":
				input.Plan.TimeoutSeconds = -1
			}
			service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
			result, err := service.ValidateCapabilityPlan(context.Background(), testPrincipal("developer"), input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid != (name == "valid") {
				t.Fatalf("unexpected validation: %+v", result)
			}
		})
	}
}

func TestCapabilityPlanValidatesLiteralsWhileBindingsArePending(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-type", "missing-literal", "unknown-field"} {
		t.Run(scenario, func(t *testing.T) {
			service, input := capabilityPlanFixture()
			tools := service.gatewayTools()
			tools[1].InputSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"hostId", "count"}, "properties": map[string]any{"hostId": map[string]any{"type": "string"}, "count": map[string]any{"type": "integer", "minimum": 1}}}
			input.Plan.Steps[1].Call.Input["count"] = 2
			switch scenario {
			case "wrong-type":
				input.Plan.Steps[1].Call.Input["count"] = "two"
			case "missing-literal":
				delete(input.Plan.Steps[1].Call.Input, "count")
			case "unknown-field":
				input.Plan.Steps[1].Call.Input["unknown"] = true
			}
			service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
			result, err := service.ValidateCapabilityPlan(context.Background(), testPrincipal("developer"), input)
			if err != nil || result.Valid != (scenario == "valid") {
				t.Fatalf("literal validation: %+v %v", result, err)
			}
		})
	}
}

func TestCapabilityBindingsCheckObservedScopeAndType(t *testing.T) {
	service, input := capabilityPlanFixture()
	tools := service.gatewayTools()
	tools[0].OutputSemantics[0].ScopePaths = map[string]string{"workspace": "/workspaceId"}
	tools[1].InputSemantics[0].ScopePaths = map[string]string{"workspace": "/workspaceId"}
	service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
	step := input.Plan.Steps[1]
	step.Call.Input["workspaceId"] = "workspace-1"
	completed := map[string]domainaigateway.ToolInvocationResult{"lookup": {Result: "success", Output: map[string]any{"id": "host-1", "workspaceId": "workspace-2"}}}
	if _, err := service.ResolveCapabilityStep(step, input.Plan, completed); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cross-scope binding allowed: %v", err)
	}
	completed["lookup"] = domainaigateway.ToolInvocationResult{Result: "success", Output: map[string]any{"id": "host-1", "workspaceId": "workspace-1"}}
	result, err := service.ResolveCapabilityStep(step, input.Plan, completed)
	if err != nil || result.Input["hostId"] != "host-1" {
		t.Fatalf("valid binding failed: %+v %v", result, err)
	}
	completed["lookup"] = domainaigateway.ToolInvocationResult{Result: "success", Output: map[string]any{"id": 42, "workspaceId": "workspace-1"}}
	if _, err := service.ResolveCapabilityStep(step, input.Plan, completed); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("untyped output reached execution: %v", err)
	}
}

func TestCapabilityDiscoveryCursorTracksVisibleCatalog(t *testing.T) {
	tools := []domainaigateway.ToolCapability{{Name: "c", Version: "1", Domain: "docker"}, {Name: "a", Version: "1", Domain: "docker"}, {Name: "b", Version: "1", Domain: "delivery"}}
	input := domainaigateway.ManifestRequest{ToolDomain: "docker", Limit: 1}
	first, revision, cursor, err := discoverCapabilities(tools, input)
	if err != nil || len(first) != 1 || first[0].Name != "a" || revision == "" || cursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	input.Cursor = cursor
	next, _, last, err := discoverCapabilities(tools, input)
	if err != nil || len(next) != 1 || next[0].Name != "c" || last != "" {
		t.Fatalf("next page: %+v %v", next, err)
	}
	tools[0].Version = "2"
	if _, _, _, err := discoverCapabilities(tools, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale catalog cursor accepted: %v", err)
	}
}

func TestCapabilityJSONPointersDoNotEvaluateExpressions(t *testing.T) {
	value := map[string]any{"a/b": map[string]any{"~key": []any{"value"}}}
	if got, ok := capabilityPointerGet(value, "/a~1b/~0key/0"); !ok || got != "value" {
		t.Fatalf("escaped pointer failed: %v", got)
	}
	for _, path := range []string{"$.a", "/a~2b", "/a~1b/~0key/-", "/a~1b/~0key/00"} {
		if _, ok := capabilityPointerGet(value, path); ok {
			t.Fatalf("invalid pointer accepted: %s", path)
		}
	}
}

func TestCapabilityPlanRejectsScalarBindingParentBeforePersistence(t *testing.T) {
	service, input := capabilityPlanFixture()
	tools := service.gatewayTools()
	tools[1].InputSemantics[0].Path = "/resource/id"
	service.SetCapabilityProviders(testCapabilityProvider{tools: tools})
	input.Plan.Steps[1].Call.Input["resource"] = "literal"
	input.Plan.Steps[1].Bindings[0].InputPath = "/resource/id"
	result, err := service.ValidateCapabilityPlan(context.Background(), testPrincipal("developer"), input)
	if err != nil || result.Valid {
		t.Fatalf("invalid scalar parent persisted: %+v %v", result, err)
	}
}

func TestCapabilityBindingRechecksPersistedDependencies(t *testing.T) {
	service, input := capabilityPlanFixture()
	step := input.Plan.Steps[1]
	step.DependsOn = nil
	_, err := service.ResolveCapabilityStep(step, input.Plan, map[string]domainaigateway.ToolInvocationResult{"lookup": {Result: "success", Output: map[string]any{"id": "host"}}})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("missing dependency accepted after restore: %v", err)
	}
}

func TestCapabilitySemanticScopesExtendPolicyWithoutDomainBranches(t *testing.T) {
	tool := domainaigateway.ToolCapability{InputSemantics: []domainaigateway.CapabilityValueSemantic{{Path: "/assetId", Kind: "newdomain.asset", ScopePaths: map[string]string{"newDomainPool": "/poolId"}}}}
	scope := capabilityGatewayScope(tool, map[string]any{"assetId": "asset", "poolId": "pool-a"})
	if !gatewayResourceScopeMatches(map[string]any{"newDomainPool": "pool-a"}, scope) || gatewayResourceScopeMatches(map[string]any{"newDomainPool": "pool-b"}, scope) {
		t.Fatal("registered scope was ignored")
	}
	if gatewayResourceScopeMatches(map[string]any{"clusterId": "cluster", "undeclaredDimension": "restricted"}, map[string]string{"clusterId": "cluster"}) {
		t.Fatal("unknown constraint silently dropped")
	}
}
