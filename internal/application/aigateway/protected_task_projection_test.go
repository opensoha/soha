package aigateway

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestProtectedDurableAdmissionRequiresExplicitStorageAndOwnedTask(t *testing.T) {
	key, err := keyring.NewKey("protected", "task115-protected-durable", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ enabled, keys, want bool }{{false, false, false}, {true, false, false}, {false, true, false}, {true, true, true}} {
		deps := ServiceDeps{ProtectedDurableStorage: test.enabled}
		if test.keys {
			deps.RelayConfig.CredentialEncryptionKeys = ring
		}
		s := NewWithDeps(deps)
		if s.protectedDurableStorage != test.want {
			t.Fatal("protected storage admitted without explicit active keyring")
		}
		ctx := WithNativeWorkbenchInvocation(t.Context())
		if err := s.authorizeProtectedWorkbenchInput(ctx, protectedPodExecTool); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
			t.Fatal("unowned Native protected call admitted", err)
		}
		ctx = withCapabilityExecution(ctx, domainworkflow.Run{ID: "owned", Scope: domainworkflow.ScopeCapabilityTask}, domainworkflow.NodeRun{NodeID: "step"})
		err := s.authorizeProtectedWorkbenchInput(ctx, protectedPodExecTool)
		if (err == nil) != test.want {
			t.Fatal("owned protected storage gate", err)
		}
	}
}

func TestProtectedCapabilityPlanAndTaskViewsHideExactArguments(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}}
	fixture := &podCapabilityFixture{receipt: podCapabilityReceipt()}
	s, provider, actor := newPodCapabilityGateway(t, repo, fixture)
	s.protectedDurableStorage = true
	input := domainai.CapabilityTaskInput{IdempotencyKey: "protected-task", AIClientID: "codex", SkillID: "k8s-pod-executor", Plan: domainai.CapabilityPlan{Goal: "Run the reviewed process", Steps: []domainai.CapabilityPlanStep{{ID: "exec", Call: sohaapi.CapabilityCall{ToolName: protectedPodExecTool, CapabilityVersion: "1", Input: podCapabilityInput()}}}}}
	input.Plan.VerificationSteps = []string{"assess"}
	input.Plan.Steps = append(input.Plan.Steps, domainai.CapabilityPlanStep{ID: "assess", Call: sohaapi.CapabilityCall{ToolName: "k8s.pods.exec.assess", CapabilityVersion: "1", Input: map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "podName": "api"}}, DependsOn: []string{"exec"}, Bindings: []domainai.CapabilityInputBinding{{StepID: "exec", OutputPath: "/receipt/id", InputPath: "/executionId"}}})
	validation, err := s.ValidateCapabilityPlan(t.Context(), actor, input)
	if err != nil || !validation.Valid {
		t.Fatal("encrypted explicit plan rejected", err, validation.Issues)
	}
	call := domainai.ToolInvocationRequest{ToolName: protectedPodExecTool, CapabilityVersion: "1", AIClientID: "codex", SkillID: "k8s-pod-executor", Input: podCapabilityInput()}
	intent := domainworkflow.CapabilityIntent{ActorID: actor.UserID, ActorTokenID: actor.AccessTokenID, Input: input, PlanVersion: 1, Digest: validation.Digest, Deadline: time.Now().Add(time.Hour)}
	result := podGatewayOutput(fixture.receipt)
	result.Output.Stdout = "PRIVATE_PROCESS_OUTPUT"
	invocation := domainai.ToolInvocationResult{ToolName: protectedPodExecTool, CapabilityVersion: "1", Result: "success", Output: result, Task: s.gatewayRegistry().TaskReference(provider.Tools()[0], result, result)}
	run := domainworkflow.Run{ID: "private-task", Scope: domainworkflow.ScopeCapabilityTask, Status: "completed", Metadata: map[string]any{"capabilityIntent": intent}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "exec", Status: "completed", PreparedCall: &call, Invocation: &invocation}}}
	visible, err := s.VisibleCapabilityTask(t.Context(), actor, run)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(visible)
	for _, marker := range []string{"PRIVATE_ARGV_MARKER", "PRIVATE_STDIN_MARKER", "PRIVATE_PROCESS_OUTPUT"} {
		if strings.Contains(string(raw), marker) {
			t.Fatal("private task projection leaked", marker)
		}
	}
	if visible.Nodes[0].Invocation == nil || visible.Nodes[0].Invocation.Task == nil || visible.Nodes[0].Invocation.Task.ID != fixture.receipt.ID {
		t.Fatal("protected projection lost original task receipt")
	}
	if !strings.Contains(string(raw), "PROTECTED sha256:") {
		t.Fatal("input binding digest missing")
	}
	if mustMapFieldAs[map[string]any](t, input.Plan.Steps[0].Call.Input, "execution")["stdinBase64"] == nil || result.Output.Stdout != "PRIVATE_PROCESS_OUTPUT" {
		t.Fatal("view modified execution state")
	}
}

func TestProtectedWorkbenchProjectionIsStableAndKeepsReceiptOnly(t *testing.T) {
	result := podGatewayOutput(podCapabilityReceipt())
	result.Output.Stdout = "PRIVATE_OUTPUT_MARKER"
	value, _ := capabilityJSON(result)
	for _, tool := range []string{protectedPodExecTool, "k8s.pods.exec.receipt"} {
		execution := domaincopilot.ToolExecution{ToolName: tool, Input: map[string]any{"input": podCapabilityInput()}, Output: mustValueAs[map[string]any](t, value)}
		first, err := ProtectedWorkbenchExecution(execution)
		if err != nil {
			t.Fatal(err)
		}
		second, err := ProtectedWorkbenchExecution(first)
		if err != nil || !reflect.DeepEqual(first, second) {
			t.Fatal("reprojection changed original digest", err)
		}
		raw, _ := json.Marshal(first)
		if strings.Contains(string(raw), "PRIVATE_OUTPUT_MARKER") || tool == protectedPodExecTool && strings.Contains(string(raw), "PRIVATE_ARGV_MARKER") {
			t.Fatal("Native observation retained private values")
		}
		if !strings.Contains(string(raw), podCapabilityReceipt().ID) {
			t.Fatal("receipt identity lost")
		}
	}
}

func TestOwnedTaskApprovalStoresOnlyProtectedProcessReceipt(t *testing.T) {
	repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}}
	fixture := &podCapabilityFixture{receipt: podCapabilityReceipt()}
	fixture.receipt.Output.Stdout = "PRIVATE_PROCESS_OUTPUT"
	s, _, actor := newPodCapabilityGateway(t, repo, fixture)
	s.protectedDurableStorage = true
	s.approvals = capabilityApprovalRepositoryFixture{repo}
	input := domainai.CapabilityTaskInput{AIClientID: "codex", SkillID: "k8s-pod-executor", Plan: domainai.CapabilityPlan{Goal: "Run reviewed process", Steps: []domainai.CapabilityPlanStep{{ID: "exec", Call: sohaapi.CapabilityCall{ToolName: protectedPodExecTool, CapabilityVersion: "1", Input: podCapabilityInput()}}}}}
	intent := domainworkflow.CapabilityIntent{ActorID: actor.UserID, ActorTokenID: actor.AccessTokenID, Input: input, Digest: "frozen", Deadline: time.Now().Add(time.Hour)}
	run := domainworkflow.Run{ID: "owned-private", Scope: domainworkflow.ScopeCapabilityTask, Status: "running", Version: 1, Metadata: map[string]any{"capabilityIntent": intent}}
	node, err := s.PrepareCapabilityNode(t.Context(), actor, run, domainworkflow.NodeRun{NodeID: "exec"})
	if err != nil {
		t.Fatal(err)
	}
	node.Status, node.DispatchAttempted = "waiting_approval", true
	run.NodeRuns = []domainworkflow.NodeRun{node}
	guard := &capabilityGuardFixture{run: run}
	s.SetCapabilityApprovalGuard(guard)
	held, err := s.InvokeTool(withCapabilityExecution(t.Context(), run, node), actor, *node.PreparedCall)
	if err != nil || !held.RequiresApproval || len(repo.approvalRequests) != 1 {
		t.Fatal("reviewed task approval not held", err)
	}
	bindNativeApprovalParent(t, s, guard)
	result, err := s.ApproveApprovalRequest(t.Context(), testPrincipal("admin"), repo.approvalRequests[0].ID, domainai.ApprovalDecisionInput{})
	if err != nil || result.Invocation == nil || result.Invocation.Task == nil || result.Invocation.Task.ID != fixture.receipt.ID || fixture.executions != 1 {
		t.Fatal("original approved process was not preserved", err)
	}
	for _, value := range []any{result, repo.approvalRequests} {
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), "PRIVATE_PROCESS_OUTPUT") || strings.Contains(string(raw), "PRIVATE_ARGV_MARKER") {
			t.Fatal("public or persisted task approval exposed private values")
		}
	}
	if fixture.input.Execution.Argv[2] != "PRIVATE_ARGV_MARKER" {
		t.Fatal("execution did not retain exact protected input")
	}
}

func TestPodProcessAssessmentNeverInfersUnknownSuccess(t *testing.T) {
	for _, scenario := range []string{"zero", "nonzero", "running", "unknown", "transport_lost", "missing_output", "missing_finish", "future_finish"} {
		t.Run(scenario, func(t *testing.T) {
			receipt := podCapabilityReceipt()
			want := "inconclusive"
			switch scenario {
			case "zero":
				want = "satisfied"
			case "nonzero":
				code := int64(42)
				receipt.Status = "failed"
				receipt.Output.ExitCode = &code
				want = "unsatisfied"
			case "running", "unknown", "transport_lost":
				receipt.Status = scenario
			case "missing_output":
				receipt.Output = nil
				receipt.OutputAvailable = false
			case "missing_finish":
				receipt.FinishedAt = nil
			case "future_finish":
				future := time.Now().Add(time.Hour)
				receipt.FinishedAt = &future
			}
			assessment := podExecutionAssessment(receipt)
			if string(assessment.Verdict) != want || len(assessment.Evidence) != 1 || assessment.Evidence[0].Resource.ID != receipt.ID {
				t.Fatalf("bad process proof %+v", assessment)
			}
			if assessment.Evidence[0].Incomplete && assessment.Verdict == "satisfied" {
				t.Fatal("incomplete source became success")
			}
		})
	}
}
