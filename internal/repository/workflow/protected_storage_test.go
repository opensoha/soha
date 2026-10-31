package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/protectedpayload"
)

func privateWorkflowTest(t *testing.T) (*Repository, domainworkflow.Run) {
	t.Helper()
	key, err := keyring.NewKey("storage-test", "task115-storage-test-key", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"content": "PRIVATE_YAML_MARKER", "integer": json.Number("9007199254740993")}
	call := domainai.ToolInvocationRequest{ToolName: "k8s.resources.conditional.update", Input: input}
	intent := domainworkflow.CapabilityIntent{ActorID: "actor-1", Digest: "digest-1", PlanVersion: 1, Deadline: time.Now().Add(time.Hour), Input: domainai.CapabilityTaskInput{IdempotencyKey: "original-key", Plan: domainai.CapabilityPlan{Goal: "Private task", Steps: []domainai.CapabilityPlanStep{{ID: "step-1", Call: sohaapi.CapabilityCall{ToolName: call.ToolName, Input: input}}}}}}
	run := domainworkflow.Run{ID: "task-1", Scope: domainworkflow.ScopeCapabilityTask, Metadata: map[string]any{"capabilityActorId": "actor-1", "capabilityInitialDigest": "digest-1", "capabilityIdempotencyKey": "original-key", "capabilityIntent": intent}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "step-1", PreparedCall: &call}}}
	run.Metadata["capabilityRevisions"] = []domainworkflow.CapabilityRevision{{Intent: intent, Nodes: run.NodeRuns, Version: 1}}
	return New(nil, keys), run
}

func TestPrivateWorkflowStorageSealsIntentPreparedCallAndRevisions(t *testing.T) {
	repo, run := privateWorkflowTest(t)
	raw, err := repo.marshalWorkflowMetadata(run)
	if err != nil || strings.Contains(string(raw), "PRIVATE_YAML_MARKER") || strings.Contains(string(raw), "9007199254740993") {
		t.Fatal("private state leaked", err)
	}
	var stored map[string]any
	if json.Unmarshal(raw, &stored) != nil {
		t.Fatal("invalid storage")
	}
	loaded, err := repo.restoreWorkflowStorage(domainworkflow.Run{ID: run.ID, Scope: run.Scope, Metadata: stored}, nil)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := domainworkflow.CapabilityIntentFrom(loaded)
	if err != nil {
		t.Fatal(err)
	}
	integer, ok := intent.Input.Plan.Steps[0].Call.Input["integer"].(json.Number)
	if !ok || integer.String() != "9007199254740993" {
		t.Fatal("intent precision changed", err)
	}
	preparedInteger, ok := loaded.NodeRuns[0].PreparedCall.Input["integer"].(json.Number)
	if !ok || loaded.NodeRuns[0].PreparedCall.Input["content"] != "PRIVATE_YAML_MARKER" || preparedInteger.String() != "9007199254740993" {
		t.Fatal("prepared call changed")
	}
	revisions, err := domainworkflow.CapabilityRevisionsFrom(loaded)
	if err != nil || len(revisions) != 1 || revisions[0].Nodes[0].PreparedCall.Input["content"] != "PRIVATE_YAML_MARKER" {
		t.Fatal("revision lost", err)
	}
	for _, id := range []string{"other-task", ""} {
		if _, err := repo.restoreWorkflowStorage(domainworkflow.Run{ID: id, Scope: run.Scope, Metadata: stored}, nil); err == nil {
			t.Fatal("state transplanted")
		}
	}
	if _, err := New(nil).restoreWorkflowStorage(domainworkflow.Run{ID: run.ID, Scope: run.Scope, Metadata: stored}, nil); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestPrivateWorkflowStorageRejectsUnsealedProtectedCalls(t *testing.T) {
	repo, run := privateWorkflowTest(t)
	if _, err := New(nil).marshalWorkflowMetadata(run); err == nil {
		t.Fatal("protected input stored without key")
	}
	if _, err := repo.restoreWorkflowStorage(run, nil); err == nil {
		t.Fatal("unsealed protected plan accepted")
	}
	raw, err := repo.marshalWorkflowMetadata(run)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	stored["capabilityActorId"] = "other-actor"
	if _, err := repo.restoreWorkflowStorage(domainworkflow.Run{ID: run.ID, Scope: run.Scope, Metadata: stored}, nil); err == nil {
		t.Fatal("actor index changed")
	}
	if !protectedpayload.ContainsNamedCall([]byte(`{"call":{"name":"k8s.pods.exec","arguments":{}}}`), "k8s.pods.exec") || protectedpayload.ContainsNamedCall([]byte(`{"message":"k8s.pods.exec"}`), "k8s.pods.exec") {
		t.Fatal("call detection treated message text as a call")
	}
}
