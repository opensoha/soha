package aieval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type GatewayModelInvoker interface {
	InvokeWorkbenchModel(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error)
}

type GatewayCandidateExecutor struct{ invoker GatewayModelInvoker }

func NewGatewayCandidateExecutor(invoker GatewayModelInvoker) (*GatewayCandidateExecutor, error) {
	if invoker == nil {
		return nil, fmt.Errorf("evaluation gateway invoker is required")
	}
	return &GatewayCandidateExecutor{invoker: invoker}, nil
}

func (e *GatewayCandidateExecutor) Execute(ctx context.Context, request ExecutionRequest) (ExecutionResult, error) {
	startedAt := time.Now()
	response, err := e.invoker.InvokeWorkbenchModel(ctx, request.Principal, appaigateway.WorkbenchRelayRequest{
		PublicModel: request.CandidateRefs["publicModel"],
		RouteID:     request.CandidateRefs["routeId"],
		Endpoint:    request.CandidateRefs["endpoint"],
		AgentRunID:  request.RunID,
		Mode:        "evaluation",
		Metadata:    map[string]any{"evaluationSampleId": request.Sample.ID, "executorProfileId": request.Profile.ID},
		Messages: []appaigateway.WorkbenchRelayMessage{
			{Role: "system", Content: "Return exactly one JSON object with string-array fields retrievedSources, producedFacts, and actions. Do not include markdown."},
			{Role: "user", Content: request.Sample.Input},
		},
	})
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("invoke evaluation candidate through gateway: %w", err)
	}
	var output struct {
		RetrievedSources []string `json:"retrievedSources"`
		ProducedFacts    []string `json:"producedFacts"`
		Actions          []string `json:"actions"`
	}
	decoder := json.NewDecoder(strings.NewReader(response.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return ExecutionResult{}, fmt.Errorf("decode evaluation candidate output: %w", err)
	}
	if len(output.RetrievedSources) > 256 || len(output.ProducedFacts) > 256 || len(output.Actions) > 128 {
		return ExecutionResult{}, fmt.Errorf("evaluation candidate output exceeds limits")
	}
	return ExecutionResult{
		Output:   SampleOutput{SampleID: request.Sample.ID, RetrievedSources: output.RetrievedSources, ProducedFacts: output.ProducedFacts, Actions: output.Actions},
		TraceRef: "gateway-request:" + response.RequestID,
		Usage:    map[string]float64{"gatewayCalls": 1},
		Latency:  time.Since(startedAt), CompletedAt: time.Now().UTC(),
	}, nil
}
