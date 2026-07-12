package workflow

import (
	"context"
	"net/http"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type dagNodeHandler interface {
	execute(context.Context, dagNodeExecutionInput) dagNodeHandlerResult
}

type dagNodeHandlerFunc func(context.Context, dagNodeExecutionInput) dagNodeHandlerResult

func (f dagNodeHandlerFunc) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	return f(ctx, input)
}

type dagNodeHandlerResult struct {
	status  string
	summary string
	outputs map[string]any
	events  []map[string]any
	err     error
}

func completedDAGNode(summary string) dagNodeHandlerResult {
	return dagNodeHandlerResult{status: "completed", summary: summary, outputs: map[string]any{}}
}

func skippedDAGNode(summary string) dagNodeHandlerResult {
	return dagNodeHandlerResult{status: "skipped", summary: summary, outputs: map[string]any{}}
}

func failedDAGNode(err error) dagNodeHandlerResult {
	result := dagNodeHandlerResult{status: "failed", outputs: map[string]any{}, err: err}
	if err != nil {
		result.summary = err.Error()
	}
	return result
}

type dagNodeExecutionInput struct {
	principal       domainidentity.Principal
	app             domainapp.App
	workflowInput   domainworkflow.Input
	binding         domaincatalog.ApplicationEnvironment
	node            dagWorkflowNode
	run             domainworkflow.Run
	resolvedInputs  map[string]any
	selectors       map[string]any
	selectedTargets []domaincatalog.ReleaseTarget
	artifactState   map[string]any
}

func newDAGNodeExecutionInput(
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	node dagWorkflowNode,
	run domainworkflow.Run,
	resolvedInputs map[string]any,
	selectors map[string]any,
	artifactState map[string]any,
) dagNodeExecutionInput {
	clonedSelectors := cloneRunMetadataForAsyncWorker(selectors)
	return dagNodeExecutionInput{
		principal:       principal,
		app:             app,
		workflowInput:   cloneDAGWorkflowInput(input),
		binding:         binding,
		node:            cloneDAGWorkflowNode(node),
		run:             cloneRunForAsyncWorker(run),
		resolvedInputs:  cloneRunMetadataForAsyncWorker(resolvedInputs),
		selectors:       clonedSelectors,
		selectedTargets: append([]domaincatalog.ReleaseTarget(nil), selectedDAGTargetsFromSelectors(clonedSelectors)...),
		artifactState:   cloneRunMetadataForAsyncWorker(artifactState),
	}
}

func cloneDAGWorkflowInput(input domainworkflow.Input) domainworkflow.Input {
	input.Variables = cloneRunMetadataForAsyncWorker(input.Variables)
	input.BuildArgs = cloneRunMetadataForAsyncWorker(input.BuildArgs)
	return input
}

func cloneDAGWorkflowNode(node dagWorkflowNode) dagWorkflowNode {
	node.Config = cloneRunMetadataForAsyncWorker(node.Config)
	node.Inputs = append([]string(nil), node.Inputs...)
	node.Outputs = append([]string(nil), node.Outputs...)
	node.ServiceSelector = cloneRunMetadataForAsyncWorker(node.ServiceSelector)
	node.EnvironmentSelector = cloneRunMetadataForAsyncWorker(node.EnvironmentSelector)
	node.TargetSelector = cloneRunMetadataForAsyncWorker(node.TargetSelector)
	node.InputMapping = cloneRunMetadataForAsyncWorker(node.InputMapping)
	node.ArtifactOutputs = cloneDAGMapSlice(node.ArtifactOutputs)
	node.ArtifactKinds = append([]string(nil), node.ArtifactKinds...)
	node.Observability = cloneRunMetadataForAsyncWorker(node.Observability)
	return node
}

func cloneDAGMapSlice(items []map[string]any) []map[string]any {
	cloned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		cloned = append(cloned, cloneRunMetadataForAsyncWorker(item))
	}
	return cloned
}

type dagNodeHandlerRegistry struct {
	handlers map[string]dagNodeHandler
	fallback dagNodeHandler
}

func newDAGNodeHandlerRegistry(deps dagNodeHandlerDependencies) *dagNodeHandlerRegistry {
	if deps.httpClient == nil {
		deps.httpClient = &http.Client{Timeout: defaultDAGHTTPTimeout}
	}
	registry := &dagNodeHandlerRegistry{
		handlers: make(map[string]dagNodeHandler),
		fallback: unsupportedDAGNodeHandler{},
	}
	registry.register([]string{"external"}, externalDAGNodeHandler{store: deps.taskStore})
	registry.register([]string{"manual_approval"}, manualApprovalDAGNodeHandler{})
	registry.register([]string{"deploy_update_image", "release"}, releaseDAGNodeHandler{releases: deps.releases})
	registry.register([]string{"build"}, buildDAGNodeHandler{builds: deps.builds})
	registry.register([]string{"wait_rollout"}, rolloutDAGNodeHandler{resources: deps.resources})
	registry.register([]string{"check_http", "smoke_test", "verify", "check"}, checkDAGNodeHandler{client: deps.httpClient, store: deps.taskStore})
	registry.register([]string{"check_k8s_event"}, k8sEventDAGNodeHandler{resources: deps.resources})
	registry.register([]string{"rollback_to_previous"}, rollbackDAGNodeHandler{resources: deps.resources})
	registry.register([]string{"restart_workload"}, restartDAGNodeHandler{resources: deps.resources})
	registry.register([]string{"scale_workload"}, scaleDAGNodeHandler{resources: deps.resources})
	registry.register([]string{"delete_pod", "evict_pod"}, deletePodDAGNodeHandler{resources: deps.resources})
	registry.register([]string{"http_callback"}, callbackDAGNodeHandler{client: deps.httpClient})
	registry.register([]string{"create_silence"}, silenceDAGNodeHandler{alerts: deps.alerts})
	registry.register([]string{"notify"}, notifyDAGNodeHandler{})
	return registry
}

func (r *dagNodeHandlerRegistry) register(nodeTypes []string, handler dagNodeHandler) {
	for _, nodeType := range nodeTypes {
		r.handlers[strings.TrimSpace(nodeType)] = handler
	}
}

func (r *dagNodeHandlerRegistry) handler(nodeType string) dagNodeHandler {
	if r != nil {
		if handler := r.handlers[strings.TrimSpace(nodeType)]; handler != nil {
			return handler
		}
		if r.fallback != nil {
			return r.fallback
		}
	}
	return unsupportedDAGNodeHandler{}
}

type dagNodeHandlerDependencies struct {
	builds     BuildExecutor
	releases   ReleaseExecutor
	resources  ResourceExecutor
	alerts     AlertMutator
	taskStore  ExecutionTaskStore
	httpClient *http.Client
}
