package copilot

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestNativePrivateExecutionCannotReachEventsRunOrModelObservation(t *testing.T) {
	private := "PRIVATE_NATIVE_EVENT_MARKER"
	execution := domaincopilot.ToolExecution{ID: "call-1", ToolName: "k8s.pods.exec", Status: "success", Input: map[string]any{"input": map[string]any{"execution": map[string]any{"argv": []string{private}}}}, Output: map[string]any{"receipt": map[string]any{"id": "original-receipt", "status": "succeeded"}, "outputState": "text", "output": map[string]any{"stdout": private}}}
	event := domaincopilot.WorkbenchStreamEvent{ID: "event-1", Type: "tool.completed", Sequence: 7, ToolCall: nativeWorkbenchCall(execution), OutputDelta: private, Metadata: map[string]any{"raw": private}}
	visibleEvent := nativePublicEvent(event)
	visibleAgain := nativePublicEvent(visibleEvent)
	if !reflect.DeepEqual(visibleEvent, visibleAgain) {
		t.Fatal("event reprojection changed digest")
	}
	run := domaincopilot.AgentRun{ID: "run", ProviderKind: "native", ToolExecutions: []domaincopilot.ToolExecution{execution}, Output: map[string]any{"workbenchEvents": []domaincopilot.WorkbenchStreamEvent{event}, "phase": "tools"}}
	visible := nativePublicRun(run)
	for _, value := range []any{visibleEvent, visible} {
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), private) || !strings.Contains(string(raw), "original-receipt") {
			t.Fatal("private observation leaked or receipt identity lost", string(raw))
		}
	}
	if event.OutputDelta != private || execution.Output["output"] == nil {
		t.Fatal("public projection mutated private execution state")
	}
	store := &nativeLoopStore{lease: domaincopilot.NativeLease{Run: domaincopilot.AgentRun{ID: "run", Output: map[string]any{}}}}
	cp := nativeCheckpoint{Pending: []nativeCall{{Call: appaigateway.WorkbenchToolCall{ID: "call-1"}}}}
	_, err := completeNativeTool(t.Context(), store, store.lease, &cp, "call-1", execution, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cp.Messages)
	if strings.Contains(string(raw), private) {
		t.Fatal("private process output reached the next model request")
	}
}

type nativeProtectedStreamFixture struct {
	*nativeLoopGateway
	private string
}

func (g *nativeProtectedStreamFixture) InvokeWorkbenchModelStream(_ context.Context, _ domainidentity.Principal, _ appaigateway.WorkbenchRelayRequest, emit func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error) {
	for _, part := range []string{"Completed ", g.private[:8], g.private[8:]} {
		if !emit(appaigateway.WorkbenchRelayStreamDelta{ContentDelta: part}) {
			return appaigateway.WorkbenchRelayResponse{}, context.Canceled
		}
	}
	return appaigateway.WorkbenchRelayResponse{Content: "Completed " + g.private, Usage: map[string]float64{"inputTokens": 8, "outputTokens": 2, "totalTokens": 10}}, nil
}

func TestNativePrivateModelTextIsBufferedAndRedactedAcrossDeltaBoundaries(t *testing.T) {
	s, store, gateway, _ := nativeGovernedTaskFixture(t)
	private := "PRIVATE_SPLIT_STREAM_MARKER"
	tool := domainai.ToolCapability{Name: "k8s.pods.exec", Version: "1", RiskLevel: "high", InputSchema: map[string]any{"type": "object"}, Execution: &domainai.ToolExecutionContract{Mode: "sync", Idempotent: true, Checks: []domainai.CapabilityCheckReference{{ToolName: "test.assess", CapabilityVersion: "1", Purpose: "verification"}}}}
	gateway.catalog = append(gateway.catalog, tool)
	actor := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	_, catalog, err := s.freezeNativeCatalog(t.Context(), actor, "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	store.lease.Run.Input["_sohaNativeCatalog"] = catalog
	store.lease.Run.ToolBindings = appendNativeTaskBindings(nil, catalog.Tasks)
	s.runtime.Stream = &nativeProtectedStreamFixture{gateway, private}
	cp := initialNativeCheckpoint(store.lease.Run)
	cp.Tasks = []nativeTaskCheckpoint{{ID: "verified-task", State: "completed", VerifiedThrough: time.Now(), Input: domainai.CapabilityTaskInput{Plan: domainai.CapabilityPlan{Steps: []domainai.CapabilityPlanStep{{Call: sohaapi.CapabilityCall{ToolName: "k8s.pods.exec", Input: map[string]any{"execution": map[string]any{"argv": []string{private}}}}}}}}}}
	_, err = s.nativeModelStep(t.Context(), store, store.lease, &cp, actor)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"events": store.events, "run": store.lease.Run, "messages": store.final})
	if strings.Contains(string(raw), private) || strings.Contains(string(raw), "PRIVATE_") || len(store.final) != 1 || !strings.Contains(store.final[0].Content, "[PROTECTED]") {
		t.Fatal("protected model stream or final message leaked", string(raw))
	}
}

func TestNativeProtectedYAMLNeverDeclassifiesModelFreeTextFragments(t *testing.T) {
	input := map[string]any{"content": "apiVersion: v1\nkind: Secret\nstringData:\n  credential: PRIVATE_SCALAR\n"}
	call := sohaapi.CapabilityCall{ToolName: "k8s.resources.conditional.update", Input: input}
	plan := domainai.CapabilityTaskInput{Plan: domainai.CapabilityPlan{Steps: []domainai.CapabilityPlanStep{{Call: call}}}}
	raw, _ := json.Marshal(map[string]any{"plan": plan.Plan})
	for _, cp := range []nativeCheckpoint{
		{Tasks: []nativeTaskCheckpoint{{Input: plan}}},
		{Pending: []nativeCall{{Call: appaigateway.WorkbenchToolCall{Arguments: raw}}}},
	} {
		for _, text := range []string{"PRIVATE_SCALAR", "Escaped fragment PRIVATE_" + "SCALAR", "Encoded UFJJVkFURV9TQ0FMQVI=", "A model paraphrase of the protected value"} {
			visible := nativePrivateText(cp, text)
			if !strings.HasPrefix(visible, "[PROTECTED]") || strings.Contains(visible, text) {
				t.Fatal("unstructured protected YAML model text was declassified", visible)
			}
		}
	}
	if nativePrivateText(nativeCheckpoint{}, "ordinary chat") != "ordinary chat" || nativePrivateText(nativeCheckpoint{Tasks: []nativeTaskCheckpoint{{Input: plan}}}, "") != "" {
		t.Fatal("ordinary or empty text changed")
	}
}
