package aieval

import (
	"context"
	"strings"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type gatewayInvokerStub struct {
	request  *appaigateway.WorkbenchRelayRequest
	response appaigateway.WorkbenchRelayResponse
	err      error
}

func (s gatewayInvokerStub) InvokeWorkbenchModel(_ context.Context, _ domainidentity.Principal, input appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
	if s.request != nil {
		*s.request = input
	}
	return s.response, s.err
}

func TestGatewayCandidateExecutorUsesStructuredBoundedOutput(t *testing.T) {
	executor, err := NewGatewayCandidateExecutor(gatewayInvokerStub{response: appaigateway.WorkbenchRelayResponse{Content: `{"retrievedSources":["doc:1"],"producedFacts":["ready"],"actions":[]}`, RequestID: "req-1"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Execute(t.Context(), ExecutionRequest{RunID: "run-1", Sample: DatasetSample{ID: "sample-1", Input: "check"}, CandidateRefs: map[string]string{"routeId": "route-1"}})
	if err != nil || result.Output.SampleID != "sample-1" || result.TraceRef != "gateway-request:req-1" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
func TestGatewayCandidateExecutorRejectsUnstructuredOutput(t *testing.T) {
	executor, _ := NewGatewayCandidateExecutor(gatewayInvokerStub{response: appaigateway.WorkbenchRelayResponse{Content: "not json"}})
	if _, err := executor.Execute(t.Context(), ExecutionRequest{Sample: DatasetSample{ID: "sample-1"}}); err == nil {
		t.Fatal("unstructured output accepted")
	}
}

func TestEvaluationCandidateAppliesPromptContextAndReportedUsage(t *testing.T) {
	var request appaigateway.WorkbenchRelayRequest
	executor, _ := NewGatewayCandidateExecutor(gatewayInvokerStub{request: &request, response: appaigateway.WorkbenchRelayResponse{Content: `{"retrievedSources":[],"producedFacts":[],"actions":[]}`, Usage: map[string]float64{"inputTokens": 13}}})
	result, err := executor.Execute(t.Context(), ExecutionRequest{Sample: DatasetSample{Input: "question"}, CandidateRefs: map[string]string{"systemPrompt": "Require evidence", "retrievalContext": "doc:approved"}})
	if err != nil || !strings.Contains(request.Messages[0].Content, "Require evidence") || !strings.Contains(request.Messages[1].Content, "doc:approved") || result.Usage["inputTokens"] != 13 {
		t.Fatalf("candidate inputs or usage not applied: %+v %+v %v", request, result, err)
	}
	if _, known := result.Usage["cost"]; known {
		t.Fatal("unknown monetary cost became zero")
	}
	if _, err := executor.Execute(t.Context(), ExecutionRequest{CandidateRefs: map[string]string{"systemPrompt": strings.Repeat("x", 513)}}); err == nil {
		t.Fatal("unbounded candidate prompt accepted")
	}
}
