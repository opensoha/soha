package workflow

import (
	"context"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type dagNodeDispatcher struct {
	planner   dagPlanner
	registry  *dagNodeHandlerRegistry
	artifacts ArtifactStore
}

func newDAGNodeDispatcher(service *Service) *dagNodeDispatcher {
	if service == nil {
		return &dagNodeDispatcher{registry: newDAGNodeHandlerRegistry(dagNodeHandlerDependencies{})}
	}
	return &dagNodeDispatcher{
		planner: dagPlanner{apps: service.apps},
		registry: newDAGNodeHandlerRegistry(dagNodeHandlerDependencies{
			builds:     service.builds,
			releases:   service.releases,
			resources:  service.resources,
			alerts:     service.alerts,
			taskStore:  service.taskStore,
			httpClient: service.httpClient,
		}),
		artifacts: service.artifacts,
	}
}

func (d *dagNodeDispatcher) execute(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	node dagWorkflowNode,
	run domainworkflow.Run,
	artifactState map[string]any,
) dagExecutionResult {
	if ctx == nil {
		ctx = context.Background()
	}
	resolvedInputs := d.planner.resolveInputs(node, app, input, binding, artifactState)
	resolvedSelectors, selectorErr := d.planner.resolveSelectors(ctx, app, input, binding, node)
	events := []map[string]any{
		{"type": "node_inputs_resolved", "inputs": metadataDAGInputState(resolvedInputs)},
	}
	if len(resolvedSelectors) > 0 {
		events = append(events, map[string]any{"type": "node_selectors_resolved", "selectors": resolvedSelectors})
	}
	if selectorErr != nil {
		events = append(events, map[string]any{"type": "node_selector_resolution_failed", "error": selectorErr.Error()})
		return newDAGExecutionResult(node, "failed", selectorErr.Error(), resolvedInputs, nil, nil, resolvedSelectors, events)
	}

	handlerInput := newDAGNodeExecutionInput(principal, app, input, binding, node, run, resolvedInputs, resolvedSelectors, artifactState)
	result := dagNodePolicy(node).execute(ctx, d.registry.handler(node.Type), handlerInput)
	status := result.status
	summary := result.summary
	outputs := result.outputs
	if outputs == nil {
		outputs = map[string]any{}
	}
	events = append(events, result.events...)

	if status == "failed" && node.ContinueOnFailure {
		summary = "continued after failure: " + summary
		status = "completed"
	}
	if status == "failed" && effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node) == "continue" {
		events = append(events, map[string]any{"type": "node_failure_continued", "originalStatus": "failed", "summary": summary})
	}

	artifactOutputs := map[string]any{}
	if len(node.ArtifactOutputs) > 0 {
		resolvedArtifacts, err := materializeDAGArtifactOutputs(node, outputs, artifactState, status)
		if err != nil {
			status = "failed"
			summary = err.Error()
			outputs = map[string]any{}
			events = append(events, map[string]any{"type": "artifact_outputs_missing", "error": summary})
		} else {
			artifactOutputs = resolvedArtifacts
		}
		for key, value := range artifactOutputs {
			outputs[key] = value
		}
		if len(artifactOutputs) > 0 {
			events = append(events, map[string]any{"type": "artifact_outputs_recorded", "artifacts": metadataDAGArtifactState(artifactOutputs)})
			if references := persistDAGArtifactOutputs(ctx, d.artifacts, app, binding, node, run, artifactOutputs, status); len(references) > 0 {
				outputs["artifactReferences"] = references
				events = append(events, map[string]any{"type": "artifact_outputs_persisted", "artifacts": references})
			}
		}
	}
	for _, outputName := range node.Outputs {
		outputName = strings.TrimSpace(outputName)
		if outputName == "" {
			continue
		}
		if _, ok := outputs[outputName]; !ok {
			if value, exists := artifactState[outputName]; exists {
				outputs[outputName] = value
			}
		}
	}
	events = append(events, map[string]any{"type": "node_finished", "status": status, "summary": summary})
	return newDAGExecutionResult(node, status, summary, resolvedInputs, outputs, artifactOutputs, resolvedSelectors, events)
}

func newDAGExecutionResult(node dagWorkflowNode, status, summary string, inputs, outputs, artifacts, selectors map[string]any, events []map[string]any) dagExecutionResult {
	if outputs == nil {
		outputs = map[string]any{}
	}
	if artifacts == nil {
		artifacts = map[string]any{}
	}
	return dagExecutionResult{
		nodeID:    node.ID,
		status:    status,
		summary:   summary,
		inputs:    inputs,
		outputs:   outputs,
		artifacts: artifacts,
		selectors: selectors,
		events:    events,
		step: domainworkflow.Step{
			Name:    node.Name,
			Status:  status,
			Summary: summary,
		},
	}
}

func persistDAGArtifactOutputs(ctx context.Context, store ArtifactStore, app domainapp.App, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode, run domainworkflow.Run, artifacts map[string]any, status string) map[string]any {
	if store == nil || len(artifacts) == 0 || run.ID == "" {
		return nil
	}
	references := map[string]any{}
	seen := map[string]struct{}{}
	for key, raw := range artifacts {
		recordMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(workflowMetadataString(recordMap, "name"), key)
		kind := workflowMetadataString(recordMap, "kind")
		if name == "" || kind == "" {
			continue
		}
		dedupeKey := kind + ":" + name
		if _, exists := seen[dedupeKey]; exists {
			continue
		}
		seen[dedupeKey] = struct{}{}
		stored, err := store.UpsertExecutionArtifact(ctx, dagArtifactStoreItem(app, binding, node, run, recordMap, status))
		if err != nil {
			continue
		}
		references[name] = map[string]any{
			"id":             stored.ID,
			"kind":           stored.Kind,
			"name":           stored.Name,
			"ref":            stored.Ref,
			"digest":         stored.Digest,
			"path":           stored.Path,
			"status":         stored.Status,
			"workflowRunId":  stored.WorkflowRunID,
			"workflowNodeId": stored.WorkflowNodeID,
		}
	}
	if len(references) == 0 {
		return nil
	}
	return references
}
