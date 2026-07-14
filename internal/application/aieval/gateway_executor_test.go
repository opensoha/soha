package aieval

import (
	"context"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type gatewayInvokerStub struct {
	response appaigateway.WorkbenchRelayResponse
	err      error
}

func (s gatewayInvokerStub) InvokeWorkbenchModel(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
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
