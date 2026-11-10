package copilot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

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
