package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type stubWorkflowAlertMutator struct{}

func (stubWorkflowAlertMutator) CreateWorkflowSilence(context.Context, domainidentity.Principal, domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	return domainalert.AlertSilence{ID: "silence-1"}, nil
}

func TestDAGNodeHandlerRegistry_ExecutesEveryRegisteredNodeType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := &recordingWorkflowArtifactStore{}
	registry := newDAGNodeHandlerRegistry(dagNodeHandlerDependencies{
		builds:     stubWorkflowBuildExecutor{},
		releases:   stubWorkflowReleaseExecutor{},
		resources:  stubWorkflowResourceExecutor{},
		alerts:     stubWorkflowAlertMutator{},
		taskStore:  store,
		httpClient: server.Client(),
	})
	tests := []struct {
		name       string
		nodeType   string
		config     map[string]any
		wantStatus string
	}{
		{name: "external", nodeType: "external", wantStatus: workflowStatusWaitingExecution},
		{name: "manual approval", nodeType: "manual_approval", wantStatus: workflowStatusWaitingApproval},
		{name: "release", nodeType: "release", wantStatus: "completed"},
		{name: "deploy update image", nodeType: "deploy_update_image", wantStatus: "completed"},
		{name: "build", nodeType: "build", wantStatus: "completed"},
		{name: "wait rollout", nodeType: "wait_rollout", wantStatus: "completed"},
		{name: "check http", nodeType: "check_http", config: map[string]any{"url": server.URL}, wantStatus: "completed"},
		{name: "smoke test", nodeType: "smoke_test", config: map[string]any{"url": server.URL}, wantStatus: "completed"},
		{name: "verify", nodeType: "verify", config: map[string]any{"url": server.URL}, wantStatus: "completed"},
		{name: "check", nodeType: "check", config: map[string]any{"url": server.URL}, wantStatus: "completed"},
		{name: "check kubernetes event", nodeType: "check_k8s_event", wantStatus: "completed"},
		{name: "rollback", nodeType: "rollback_to_previous", wantStatus: "completed"},
		{name: "restart", nodeType: "restart_workload", wantStatus: "completed"},
		{name: "scale", nodeType: "scale_workload", config: map[string]any{"replicas": 2}, wantStatus: "completed"},
		{name: "delete pod", nodeType: "delete_pod", config: map[string]any{"podName": "demo-1"}, wantStatus: "completed"},
		{name: "evict pod", nodeType: "evict_pod", config: map[string]any{"podName": "demo-1"}, wantStatus: "completed"},
		{name: "http callback", nodeType: "http_callback", config: map[string]any{"url": server.URL}, wantStatus: "completed"},
		{name: "create silence", nodeType: "create_silence", wantStatus: "completed"},
		{name: "notify", nodeType: "notify", config: map[string]any{"channel": "ops"}, wantStatus: "completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := dagWorkflowNode{ID: "node-1", Name: tt.name, Type: tt.nodeType, Config: tt.config, TimeoutSeconds: 5}
			input := newDAGNodeExecutionInput(
				domainidentity.Principal{UserName: "tester"},
				domainapp.App{ID: "app-1", DefaultBranch: "main", DefaultTag: "latest"},
				domainworkflow.Input{ApplicationID: "app-1", ClusterID: "cluster-1", Namespace: "default", DeploymentName: "demo"},
				domaincatalog.ApplicationEnvironment{ID: "binding-1"},
				node,
				domainworkflow.Run{ID: "run-1"},
				nil,
				nil,
				nil,
			)
			result := registry.handler(tt.nodeType).execute(context.Background(), input)
			if result.status != tt.wantStatus {
				t.Fatalf("handler status = %q, summary = %q, want %q", result.status, result.summary, tt.wantStatus)
			}
		})
	}
}

func TestDAGNodeHandlerRegistry_FailureContracts(t *testing.T) {
	registry := newDAGNodeHandlerRegistry(dagNodeHandlerDependencies{})
	tests := []struct {
		name        string
		nodeType    string
		config      map[string]any
		wantStatus  string
		wantSummary string
	}{
		{name: "external store missing", nodeType: "external", wantStatus: "failed", wantSummary: "execution task store is not configured"},
		{name: "release executor missing", nodeType: "release", wantStatus: "failed", wantSummary: "release executor is not configured"},
		{name: "build executor missing", nodeType: "build", wantStatus: "failed", wantSummary: "build executor is not configured"},
		{name: "resource executor missing", nodeType: "wait_rollout", wantStatus: "failed", wantSummary: "resource executor is not configured"},
		{name: "callback url missing", nodeType: "http_callback", wantStatus: "failed", wantSummary: "http callback requires url"},
		{name: "alert mutator missing", nodeType: "create_silence", wantStatus: "failed", wantSummary: "alert mutator is not configured"},
		{name: "unknown type", nodeType: "future_node", wantStatus: "skipped", wantSummary: "node type future_node is not executable yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := dagWorkflowNode{ID: "node-1", Name: tt.name, Type: tt.nodeType, Config: tt.config}
			input := newDAGNodeExecutionInput(domainidentity.Principal{}, domainapp.App{}, domainworkflow.Input{}, domaincatalog.ApplicationEnvironment{}, node, domainworkflow.Run{}, nil, nil, nil)
			result := registry.handler(tt.nodeType).execute(context.Background(), input)
			if result.status != tt.wantStatus || result.summary != tt.wantSummary {
				t.Fatalf("handler result = status %q summary %q, want status %q summary %q", result.status, result.summary, tt.wantStatus, tt.wantSummary)
			}
		})
	}
}

func TestDAGNodeExecutionPolicy_RetriesUntilSuccess(t *testing.T) {
	var attempts atomic.Int64
	handler := dagNodeHandlerFunc(func(context.Context, dagNodeExecutionInput) dagNodeHandlerResult {
		if attempts.Add(1) < 3 {
			return failedDAGNode(errors.New("temporary failure"))
		}
		return completedDAGNode("done")
	})
	result := (dagNodeExecutionPolicy{timeout: time.Second, maxAttempts: 3}).execute(context.Background(), handler, dagNodeExecutionInput{})
	if result.status != "completed" || result.summary != "done" || attempts.Load() != 3 {
		t.Fatalf("result = %#v, attempts = %d", result, attempts.Load())
	}
	if len(result.events) != 2 {
		t.Fatalf("retry events = %#v, want 2", result.events)
	}
}

func TestDAGNodeExecutionPolicy_ExhaustsRetryBudget(t *testing.T) {
	var attempts atomic.Int64
	handler := dagNodeHandlerFunc(func(context.Context, dagNodeExecutionInput) dagNodeHandlerResult {
		attempts.Add(1)
		return failedDAGNode(errors.New("still unavailable"))
	})
	result := (dagNodeExecutionPolicy{timeout: time.Second, maxAttempts: 2}).execute(context.Background(), handler, dagNodeExecutionInput{})
	if result.status != "failed" || result.summary != "still unavailable" || attempts.Load() != 2 {
		t.Fatalf("result = %#v, attempts = %d", result, attempts.Load())
	}
	if len(result.events) != 1 {
		t.Fatalf("retry events = %#v, want 1", result.events)
	}
}

func TestDAGNodePolicy_ReadsTimeoutAndRetryConfig(t *testing.T) {
	policy := dagNodePolicy(dagWorkflowNode{TimeoutSeconds: 9, Config: map[string]any{
		"timeoutSeconds": 2,
		"retry": map[string]any{
			"maxAttempts":       3,
			"delayMilliseconds": 25,
		},
	}})
	if policy.timeout != 2*time.Second || policy.maxAttempts != 3 || policy.retryDelay != 25*time.Millisecond {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestDAGNodeExecutionPolicy_TimeoutAndCancellation(t *testing.T) {
	blocking := dagNodeHandlerFunc(func(ctx context.Context, _ dagNodeExecutionInput) dagNodeHandlerResult {
		<-ctx.Done()
		return failedDAGNode(ctx.Err())
	})
	t.Run("timeout", func(t *testing.T) {
		result := (dagNodeExecutionPolicy{timeout: 20 * time.Millisecond, maxAttempts: 1}).execute(context.Background(), blocking, dagNodeExecutionInput{})
		if result.status != "failed" || !strings.Contains(result.summary, "node timed out after") {
			t.Fatalf("timeout result = %#v", result)
		}
	})
	t.Run("parent cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := (dagNodeExecutionPolicy{timeout: time.Second, maxAttempts: 3}).execute(ctx, blocking, dagNodeExecutionInput{})
		if result.status != "failed" || result.summary != context.Canceled.Error() {
			t.Fatalf("cancellation result = %#v", result)
		}
	})
}

func TestNewDAGNodeExecutionInput_ClonesMutableInput(t *testing.T) {
	config := map[string]any{"channel": "ops"}
	artifacts := map[string]any{"image": "repo/demo:v1"}
	node := dagWorkflowNode{Type: "notify", Config: config}
	input := newDAGNodeExecutionInput(domainidentity.Principal{}, domainapp.App{}, domainworkflow.Input{}, domaincatalog.ApplicationEnvironment{}, node, domainworkflow.Run{}, nil, nil, artifacts)
	config["channel"] = "changed"
	artifacts["image"] = "changed"
	if input.node.Config["channel"] != "ops" || input.artifactState["image"] != "repo/demo:v1" {
		t.Fatalf("execution input changed with caller maps: node=%#v artifacts=%#v", input.node.Config, input.artifactState)
	}
}
