package copilot

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestNativeModelCallsAreValidatedBeforeCheckpoint(t *testing.T) {
	const marker = "rejected-private-argv-marker"
	for _, scenario := range []string{"unavailable_second_call", "protected_plan"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, gateway, owner := nativeGovernedTaskFixture(t)
			calls := []appaigateway.WorkbenchToolCall{{ID: "valid", Name: "harness_request_task", Arguments: nativeTaskFixtureArguments(t)}}
			if scenario == "unavailable_second_call" {
				calls = append(calls, appaigateway.WorkbenchToolCall{ID: "unavailable", Name: "k8s_pods_exec", Arguments: json.RawMessage(`{"argv":["` + marker + `"]}`)})
			} else {
				plan := nativeTaskFixturePlan()
				plan.Steps[0].Call.ToolName = "k8s.pods.exec"
				plan.Steps[0].Call.CapabilityVersion = "1"
				plan.Steps[0].Call.Input = map[string]any{"argv": []string{marker}}
				arguments, err := json.Marshal(map[string]any{"plan": plan})
				if err != nil {
					t.Fatal(err)
				}
				calls = append(calls, appaigateway.WorkbenchToolCall{ID: "protected", Name: "harness_request_task", Arguments: arguments})
			}
			cp := initialNativeCheckpoint(store.lease.Run)
			before, _ := json.Marshal(cp)
			response := appaigateway.WorkbenchRelayResponse{Content: marker, Usage: map[string]float64{"totalTokens": 10}, ToolCalls: calls}
			principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
			if err := service.queueNativeModelToolCalls(t.Context(), principal, &cp, store.lease, response); err == nil {
				t.Fatal("unsupported batch was accepted")
			}
			after, _ := json.Marshal(cp)
			if string(after) != string(before) || owner.creates != 0 {
				t.Fatal("rejected batch changed the checkpoint or created a child task")
			}
			gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
				return response, nil
			}
			if err := service.processNativeRun(t.Context(), store, store.lease); err != nil {
				t.Fatal(err)
			}
			var stored []byte
			// Inspect actual durable projections; private fixture fields are not JSON state.
			for _, value := range []any{store.lease, store.events, store.final, store.modelCheckpoints} {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				stored = append(stored, data...)
			}
			if strings.Contains(string(stored), marker) || store.lease.Run.Status != "failed" || owner.creates != 0 || gateway.toolCalls != 0 {
				t.Fatal("rejected model arguments reached storage, events, or execution")
			}
		})
	}
}

func TestNativeModelTaskCheckpointUsesCanonicalPlan(t *testing.T) {
	service, store, _, owner := nativeGovernedTaskFixture(t)
	const marker = "ignored-caller-private-value"
	var input struct {
		Plan domainaigateway.CapabilityPlan `json:"plan"`
	}
	if err := json.Unmarshal(nativeTaskFixtureArguments(t), &input); err != nil {
		t.Fatal(err)
	}
	input.Plan.Steps[1].Call.Input["idempotencyKey"] = marker
	plan, err := json.Marshal(input.Plan)
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"plan":{"goal":"` + marker + `"},"plan":` + string(plan) + `}`)
	cp := initialNativeCheckpoint(store.lease.Run)
	usage := cp.Usage
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	response := appaigateway.WorkbenchRelayResponse{ToolCalls: []appaigateway.WorkbenchToolCall{{ID: "request", Name: "harness_request_task", Arguments: arguments}}}
	if err := service.queueNativeModelToolCalls(t.Context(), principal, &cp, store.lease, response); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), marker) || len(cp.Pending) != 1 || len(cp.Messages) != 1 || !reflect.DeepEqual(usage, cp.Usage) || owner.creates != 0 || len(cp.Tasks) != 0 {
		t.Fatal("preflight retained ignored input or consumed task state/budget")
	}
	if string(cp.Pending[0].Call.Arguments) != string(cp.Messages[0].ToolCalls[0].Arguments) {
		t.Fatal("checkpoint argument copies differ")
	}
	prepared, err := service.prepareNativeTask(t.Context(), principal, store.lease, &cp, cp.Pending[0].Call.Arguments)
	if err != nil || cp.Usage.Steps != usage.Steps+3 || !strings.HasPrefix(prepared.Input.IdempotencyKey, "native:") {
		t.Fatalf("canonical input cannot be executed with one budget reservation: %v", err)
	}
	response.ToolCalls[0].Arguments[0] = ' '
	if cp.Pending[0].Call.Arguments[0] != '{' {
		t.Fatal("checkpoint aliases caller-owned arguments")
	}
}
