package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
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
	stored["capabilityActorId"] = "actor-1"
	protected, ok := stored[privateWorkflowMetadata].(map[string]any)
	if !ok {
		t.Fatalf("unexpected protected workflow metadata type %T", stored[privateWorkflowMetadata])
	}
	protected["ciphertext"] = "tampered"
	if _, err := repo.restoreWorkflowStorage(domainworkflow.Run{ID: run.ID, Scope: run.Scope, Metadata: stored}, nil); !errors.Is(err, protectedpayload.ErrUnavailable) {
		t.Fatalf("tampered state returned %v", err)
	}
}

func TestPrivateWorkflowStorageRejectsEachPlaintextCallSlot(t *testing.T) {
	cases := []struct {
		name     string
		metadata string
	}{
		{"current plan", `{"capabilityIntent":{"input":{"plan":{"steps":[{"call":{"toolName":"k8s.pods.exec"}}]}}}}`},
		{"archived plan", `{"capabilityRevisions":[{"intent":{"input":{"plan":{"steps":[{"call":{"toolName":"k8s.resources.conditional.update"}}]}}}}]}`},
		{"prepared call", `{"nodeRuns":[{"preparedCall":{"toolName":"k8s.pods.exec"}}]}`},
		{"control call", `{"nodeRuns":[{"controlCall":{"toolName":"k8s.pods.exec"}}]}`},
		{"archived prepared call", `{"capabilityRevisions":[{"nodes":[{"preparedCall":{"toolName":"k8s.pods.exec"}}]}]}`},
		{"archived control call", `{"capabilityRevisions":[{"nodes":[{"controlCall":{"toolName":"k8s.pods.exec"}}]}]}`},
		{"invocation", `{"nodeRuns":[{"invocation":{"toolName":"k8s.pods.exec"}}]}`},
		{"task status call", `{"nodeRuns":[{"invocation":{"task":{"statusCall":{"toolName":"k8s.pods.exec"}}}}]}`},
		{"task cancel call", `{"nodeRuns":[{"invocation":{"task":{"cancelCall":{"toolName":"k8s.pods.exec"}}}}]}`},
		{"assessment reference", `{"nodeRuns":[{"invocation":{"assessment":{"evidence":[{"reference":{"toolName":"k8s.pods.exec"}}]}}}]}`},
		{"unrecognized revision shape", `{"capabilityRevisions":{"intent":{"input":{"plan":{"steps":[{"call":{"toolName":"k8s.pods.exec"}}]}}}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyed, run := privateWorkflowTest(t)
			run.NodeRuns, run.Metadata = nil, nil
			if err := json.Unmarshal([]byte(tc.metadata), &run.Metadata); err != nil {
				t.Fatal(err)
			}
			if _, err := New(nil).marshalWorkflowMetadata(run); !errors.Is(err, protectedpayload.ErrUnavailable) {
				t.Fatalf("plaintext write returned %v", err)
			}
			if _, err := keyed.restoreWorkflowStorage(run, nil); !errors.Is(err, protectedpayload.ErrUnavailable) {
				t.Fatalf("legacy plaintext read returned %v", err)
			}
		})
	}
}

func TestPrivateWorkflowStorageRoundTripsOrdinaryBusinessNames(t *testing.T) {
	_, run := privateWorkflowTest(t)
	input := map[string]any{"name": "k8s.pods.exec", "nested": map[string]any{"toolName": "k8s.resources.conditional.update"}}
	const reader = "k8s.resources.identity.read"
	call := domainai.ToolInvocationRequest{ToolName: reader, Input: input}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		t.Fatal(err)
	}
	intent.Input.Plan.Steps[0].Call = sohaapi.CapabilityCall{ToolName: reader, Input: input}
	run.Metadata["capabilityIntent"] = intent
	run.NodeRuns = []domainworkflow.NodeRun{{NodeID: "step-1", Name: "k8s.pods.exec", PreparedCall: &call, Invocation: &domainai.ToolInvocationResult{ToolName: reader, Output: input}}}
	run.Metadata["capabilityRevisions"] = []domainworkflow.CapabilityRevision{{Intent: intent, Nodes: run.NodeRuns, Version: 1}}
	repo := New(nil)
	raw, err := repo.marshalWorkflowMetadata(run)
	if err != nil {
		t.Fatal(err)
	}
	run.Metadata = nil
	if err := json.Unmarshal(raw, &run.Metadata); err != nil {
		t.Fatal(err)
	}
	run.NodeRuns = extractWorkflowNodeRuns(run.Metadata)
	loaded, err := repo.restoreWorkflowStorage(run, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := domainworkflow.CapabilityIntentFrom(loaded)
	if err != nil || current.Input.Plan.Steps[0].Call.Input["name"] != input["name"] {
		t.Fatalf("current plan lost business input: %v", err)
	}
	revisions, err := domainworkflow.CapabilityRevisionsFrom(loaded)
	if err != nil || len(revisions) != 1 || revisions[0].Nodes[0].PreparedCall.Input["name"] != input["name"] {
		t.Fatalf("archived plan lost prepared input: %v", err)
	}
}
