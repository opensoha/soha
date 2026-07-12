package workflow

import (
	"fmt"
	"strings"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func recordDAGFailurePolicy(run *domainworkflow.Run, nodeID, policy, summary string) {
	ensureDAGExecutionMetadata(run)
	policies := metadataMap(run.Metadata["failurePolicies"])
	effect := "workflow_failed"
	switch policy {
	case "continue":
		effect = "workflow_continues"
	case "rollback":
		effect = "failure_branch_can_rollback"
	case "notify":
		effect = "failure_branch_can_notify"
	case "stop":
		effect = "stop_successors"
	}
	policies[nodeID] = map[string]any{
		"policy":  policy,
		"effect":  effect,
		"summary": summary,
	}
	run.Metadata["failurePolicies"] = policies
	appendDAGNodeEvent(run, nodeID, "failure_policy_applied", "failed", summary, map[string]any{"failurePolicy": policy, "effect": effect})
}

func nodeByID(definition dagWorkflowDefinition, nodeID string) dagWorkflowNode {
	for _, node := range definition.Nodes {
		if node.ID == nodeID {
			return node
		}
	}
	return dagWorkflowNode{ID: nodeID}
}

func effectiveDAGFailurePolicy(definition dagWorkflowDefinition, node dagWorkflowNode) string {
	if node.ContinueOnFailure {
		return "continue"
	}
	policy := strings.ToLower(strings.TrimSpace(node.FailurePolicy))
	switch policy {
	case "continue", "rollback", "notify", "stop":
		return policy
	default:
		return "stop"
	}
}

func shouldStopDAGNodeAfterFailure(definition dagWorkflowDefinition, nodeID string, stoppedSources map[string]struct{}) bool {
	if len(stoppedSources) == 0 {
		return false
	}
	if _, failed := stoppedSources[nodeID]; failed {
		return false
	}
	return !hasFailureBranchPath(definition, nodeID, stoppedSources, map[string]bool{})
}

func dagStopFailureSourcesFromNodeRuns(definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) map[string]struct{} {
	out := make(map[string]struct{})
	for _, node := range definition.Nodes {
		entry := nodeRuns[node.ID]
		if entry.Status != "failed" {
			continue
		}
		if effectiveDAGFailurePolicy(definition, node) == "stop" {
			out[node.ID] = struct{}{}
		}
	}
	return out
}

func hasFailureBranchPath(definition dagWorkflowDefinition, nodeID string, stoppedSources map[string]struct{}, seen map[string]bool) bool {
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	for _, edge := range definition.Edges {
		if edge.Target != nodeID || !dagEdgeAllowsFailureBranch(edge.Condition) {
			continue
		}
		if _, ok := stoppedSources[edge.Source]; ok {
			return true
		}
		if hasFailureBranchPath(definition, edge.Source, stoppedSources, seen) {
			return true
		}
	}
	return false
}

func dagEdgeAllowsFailureBranch(condition string) bool {
	switch strings.TrimSpace(condition) {
	case "failure", "always":
		return true
	default:
		return false
	}
}

func dagFailureCountsAsWorkflowFailure(definition dagWorkflowDefinition, node dagWorkflowNode) bool {
	return effectiveDAGFailurePolicy(definition, node) != "continue"
}

func isValidationDAGNode(nodeType string) bool {
	switch strings.TrimSpace(nodeType) {
	case "check_http", "check_k8s_event", "smoke_test", "verify", "check":
		return true
	default:
		return false
	}
}

func isRollbackDAGNode(nodeType string) bool {
	switch strings.TrimSpace(nodeType) {
	case "rollback_to_previous":
		return true
	default:
		return false
	}
}

func resolveDAGNodeReadiness(definition dagWorkflowDefinition, node dagWorkflowNode, incoming []dagWorkflowEdge, statuses map[string]string) (ready bool, skipped bool) {
	if len(incoming) == 0 {
		return true, false
	}
	allPredicatesResolved := true
	anySatisfied := false
	for _, edge := range incoming {
		predStatus := statuses[edge.Source]
		if predStatus == "" || predStatus == "pending" || predStatus == "running" || predStatus == workflowStatusWaitingApproval || predStatus == workflowStatusWaitingExecution {
			allPredicatesResolved = false
			continue
		}
		if dagEdgeSatisfied(definition, edge, predStatus) {
			anySatisfied = true
		}
	}
	if allPredicatesResolved && !anySatisfied {
		return false, true
	}
	return allPredicatesResolved && anySatisfied, false
}

func dagEdgeSatisfied(definition dagWorkflowDefinition, edge dagWorkflowEdge, status string) bool {
	switch strings.TrimSpace(edge.Condition) {
	case "failure":
		return status == "failed"
	case "always":
		return status == "completed" || status == "failed" || status == "skipped"
	default:
		if status == "completed" {
			return true
		}
		return status == "failed" && effectiveDAGFailurePolicy(definition, nodeByID(definition, edge.Source)) == "continue"
	}
}

func initializeNodeRuns(definition dagWorkflowDefinition) map[string]dagNodeRun {
	items := make(map[string]dagNodeRun, len(definition.Nodes))
	for _, node := range definition.Nodes {
		items[node.ID] = dagNodeRun{
			NodeID: node.ID,
			Name:   node.Name,
			Type:   node.Type,
			Status: "pending",
		}
	}
	return items
}

func restoreNodeRuns(definition dagWorkflowDefinition, existing []domainworkflow.NodeRun) map[string]dagNodeRun {
	items := initializeNodeRuns(definition)
	for _, item := range existing {
		if _, ok := items[item.NodeID]; !ok {
			continue
		}
		items[item.NodeID] = item
	}
	return items
}

func collectNodeStatuses(items map[string]dagNodeRun) map[string]string {
	statuses := make(map[string]string, len(items))
	for nodeID, item := range items {
		if item.Status == "" || item.Status == "pending" {
			continue
		}
		statuses[nodeID] = item.Status
	}
	return statuses
}

func hasWaitingApproval(items map[string]dagNodeRun) bool {
	for _, item := range items {
		if item.Status == workflowStatusWaitingApproval {
			return true
		}
	}
	return false
}

func hasWaitingExecution(items map[string]dagNodeRun) bool {
	for _, item := range items {
		if item.Status == workflowStatusWaitingExecution {
			return true
		}
	}
	return false
}

func buildStepsFromNodeRuns(definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) []domainworkflow.Step {
	steps := make([]domainworkflow.Step, 0, len(definition.Nodes))
	for _, node := range definition.Nodes {
		entry := nodeRuns[node.ID]
		status := entry.Status
		if status == "" {
			status = "pending"
		}
		steps = append(steps, domainworkflow.Step{
			Name:    node.Name,
			Status:  status,
			Summary: entry.Summary,
		})
	}
	return steps
}

func mapNodeRunsToSlice(definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) []dagNodeRun {
	items := make([]dagNodeRun, 0, len(definition.Nodes))
	for _, node := range definition.Nodes {
		items = append(items, nodeRuns[node.ID])
	}
	return items
}

func workflowStatusFromExecutionTask(status string) (string, bool) {
	switch strings.TrimSpace(status) {
	case "completed":
		return "completed", true
	case "failed", "canceled", "callback_timeout":
		return "failed", true
	default:
		return "", false
	}
}

func executionTaskWorkflowSummary(task domaindelivery.ExecutionTask) string {
	status := strings.TrimSpace(task.Status)
	if status == "" {
		status = "completed"
	}
	for _, key := range []string{"error", "message", "summary"} {
		if text := mapString(task.Result, key); text != "" {
			return text
		}
	}
	return fmt.Sprintf("execution task %s %s", task.ID, status)
}

func workflowSystemPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "system",
		UserName: "Soha Workflow",
		Roles:    []string{"admin"},
	}
}

func syncRunNodeState(run domainworkflow.Run, definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun) domainworkflow.Run {
	run.Steps = buildStepsFromNodeRuns(definition, nodeRuns)
	run.NodeRuns = mapNodeRunsToSlice(definition, nodeRuns)
	run.Metadata = withNodeRunsMetadata(run.Metadata, definition, nodeRuns)
	return run
}

func cloneRunForAsyncWorker(run domainworkflow.Run) domainworkflow.Run {
	run.Steps = append([]domainworkflow.Step(nil), run.Steps...)
	run.NodeRuns = append([]domainworkflow.NodeRun(nil), run.NodeRuns...)
	run.Metadata = cloneRunMetadataForAsyncWorker(run.Metadata)
	return run
}
