package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var deliveryTargetIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func normalizeDeliveryDefinition(input domainworkflow.DeliveryWorkflowDefinition) (domainworkflow.DeliveryWorkflowDefinition, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 160 || len(input.Targets) == 0 || len(input.Targets) > 200 {
		return input, fmt.Errorf("%w: a delivery name and 1–200 targets are required", apperrors.ErrInvalidArgument)
	}
	input.Mode = firstNonEmpty(input.Mode, domainworkflow.DeliveryModeSerial)
	if input.Mode != domainworkflow.DeliveryModeSerial && input.Mode != domainworkflow.DeliveryModeBuildAll {
		return input, fmt.Errorf("%w: unsupported delivery mode", apperrors.ErrInvalidArgument)
	}
	if input.MaxConcurrency == 0 {
		input.MaxConcurrency = defaultDAGNodeConcurrency
	}
	if input.MaxConcurrency < 1 || input.MaxConcurrency > 32 {
		return input, fmt.Errorf("%w: maxConcurrency must be between 1 and 32", apperrors.ErrInvalidArgument)
	}
	stop := input.StopsOnFailure()
	input.StopOnFailure = &stop
	if (input.WorkflowTemplateID == "") != (input.WorkflowTemplateVersion == 0) || input.WorkflowTemplateVersion < 0 {
		return input, fmt.Errorf("%w: workflow template requires a published version", apperrors.ErrInvalidArgument)
	}
	if err := validateDeliveryTargets(input.Targets); err != nil {
		return input, err
	}
	return input, nil
}

func validateDeliveryTargets(targets []domainworkflow.DeliveryTargetInput) error {
	ids, identities := map[string]bool{}, map[string]bool{}
	for _, target := range targets {
		if !deliveryTargetIDPattern.MatchString(target.ID) || ids[target.ID] {
			return fmt.Errorf("%w: invalid or duplicate target id %q", apperrors.ErrInvalidArgument, target.ID)
		}
		ids[target.ID] = true
		if err := validateDeliveryTarget(target); err != nil {
			return err
		}
		identity := target.ApplicationID + "\x00" + target.ServiceID + "\x00" + target.ApplicationEnvironmentID
		if identities[identity] {
			return fmt.Errorf("%w: target %s repeats the same service environment", apperrors.ErrInvalidArgument, target.ID)
		}
		identities[identity] = true
	}
	for _, target := range targets {
		seen := map[string]bool{}
		for _, dep := range target.DependsOn {
			if dep == target.ID || !ids[dep] || seen[dep] {
				return fmt.Errorf("%w: target %s has invalid dependency %s", apperrors.ErrInvalidArgument, target.ID, dep)
			}
			seen[dep] = true
		}
	}
	return nil
}

func validateDeliveryTarget(target domainworkflow.DeliveryTargetInput) error {
	if target.HelmRevision < 0 || target.HelmRevision > 0 && (target.Action != "config_update" || target.ReleaseBundleID != "") {
		return fmt.Errorf("%w: Helm rollback requires config_update without an artifact override", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(target.ApplicationID) == "" || strings.TrimSpace(target.ServiceID) == "" || target.Group < 0 || target.Group > 199 {
		return fmt.Errorf("%w: target %s requires application, service and a valid group", apperrors.ErrInvalidArgument, target.ID)
	}
	if err := validateDeliveryTargetAction(target); err != nil {
		return err
	}

	if target.Action != "build" && strings.TrimSpace(target.ApplicationEnvironmentID) == "" {
		return fmt.Errorf("%w: deployment target %s requires an application environment binding", apperrors.ErrInvalidArgument, target.ID)
	}
	if target.Action == "deploy" || target.Action == "config_update" {
		if len(target.RepositoryRefs) > 0 || len(target.BuildArgs) > 0 {
			return fmt.Errorf("%w: target %s does not build and cannot override build inputs", apperrors.ErrInvalidArgument, target.ID)
		}
	}
	return nil
}

func validateDeliveryTargetAction(target domainworkflow.DeliveryTargetInput) error {
	switch target.Action {
	case "build", "build_deploy":
		if target.ReleaseBundleID != "" {
			return fmt.Errorf("%w: build target %s cannot select an existing artifact", apperrors.ErrInvalidArgument, target.ID)
		}
	case "deploy":
		if target.ReleaseBundleID == "" {
			return fmt.Errorf("%w: deploy target %s requires an existing artifact", apperrors.ErrInvalidArgument, target.ID)
		}
	case "config_update":
	default:
		return fmt.Errorf("%w: target %s has an unsupported action", apperrors.ErrInvalidArgument, target.ID)
	}
	return nil
}

// compileDeliveryDAG uses the same node graph as application workflows. Batch
// edges are conjunctive: ordering waits for termination, business edges require
// success, independently of the batch's failure policy.
func compileDeliveryDAG(definition domainworkflow.DeliveryWorkflowDefinition, targets []domainworkflow.DeliveryTargetSnapshot) (dagWorkflowDefinition, error) {
	dag := dagWorkflowDefinition{SchemaVersion: 3, Mode: domainworkflow.ScopeDeliveryBatch}
	first, last := map[string]string{}, map[string]string{}
	builds := map[string]bool{}
	policy := "continue"
	if definition.StopsOnFailure() {
		policy = "stop"
	}
	for _, snapshot := range targets {
		target := snapshot.Target
		if target.Action == "build" || target.Action == "build_deploy" {
			buildID := snapshot.BuildNodeID
			if buildID == "" {
				return dag, fmt.Errorf("%w: target %s has no frozen build identity", apperrors.ErrInvalidArgument, target.ID)
			}
			if !builds[buildID] {
				dag.Nodes = append(dag.Nodes, deliveryStageNode(buildID, target.ID, "build", policy))
				builds[buildID] = true
			}
			first[target.ID], last[target.ID] = buildID, buildID
		}
		if target.Action == "build" {
			id := target.ID + ":done"
			dag.Nodes = append(dag.Nodes, deliveryStageNode(id, target.ID, "barrier", policy))
			deliveryDAGEdge(&dag, last[target.ID], id, "success")
			last[target.ID] = id
		}
		if target.Action != "build" {
			for _, stage := range []string{"plan", "deploy", "health"} {
				id := target.ID + ":" + stage
				dag.Nodes = append(dag.Nodes, deliveryStageNode(id, target.ID, stage, policy))
				if previous := last[target.ID]; previous != "" {
					deliveryDAGEdge(&dag, previous, id, "success")
				} else {
					first[target.ID] = id
				}
				last[target.ID] = id
			}
		}
	}
	if definition.Mode == domainworkflow.DeliveryModeBuildAll {
		compileDeliveryBuildBarrier(&dag, targets, builds, last, policy)
	} else {
		compileDeliverySerialOrder(&dag, targets, first, last)
	}
	for _, snapshot := range targets {
		target := snapshot.Target
		start := first[target.ID]
		if snapshot.BuildNodeID != "" && nodeByID(dag, snapshot.BuildNodeID).TargetID != target.ID {
			start = target.ID + ":plan"
			if target.Action == "build" {
				start = target.ID + ":done"
			}
		}
		if definition.Mode == domainworkflow.DeliveryModeBuildAll && target.Action != "build" {
			start = target.ID + ":plan"
		}
		for _, dependency := range target.DependsOn {
			deliveryDAGEdge(&dag, last[dependency], start, "success")
		}
	}
	return dag, validateDeliveryDAG(dag)
}

func deliveryStageNode(id, targetID, stage, policy string) dagWorkflowNode {
	return dagWorkflowNode{ID: id, Name: id, Type: "delivery_stage", TargetID: targetID, Stage: stage, FailurePolicy: policy, TimeoutSeconds: 300}
}

func deliveryDAGEdge(dag *dagWorkflowDefinition, source, target, condition string) {
	if source == target {
		return
	}
	dag.Edges = append(dag.Edges, dagWorkflowEdge{ID: source + ":" + target + ":" + condition, Source: source, Target: target, Condition: condition})
}

func compileDeliverySerialOrder(dag *dagWorkflowDefinition, targets []domainworkflow.DeliveryTargetSnapshot, first, last map[string]string) {
	seenBuilds := map[string]bool{}
	for i, snapshot := range targets {
		start := first[snapshot.Target.ID]
		if snapshot.BuildNodeID != "" && seenBuilds[snapshot.BuildNodeID] {
			start = snapshot.Target.ID + ":plan"
			if snapshot.Target.Action == "build" {
				start = snapshot.Target.ID + ":done"
			}
		}
		seenBuilds[snapshot.BuildNodeID] = true
		if i > 0 {
			deliveryDAGEdge(dag, last[targets[i-1].Target.ID], start, "settled")
		}
	}
}

func compileDeliveryBuildBarrier(dag *dagWorkflowDefinition, targets []domainworkflow.DeliveryTargetSnapshot, builds map[string]bool, last map[string]string, policy string) {
	const barrier = "builds:barrier"
	dag.Nodes = append(dag.Nodes, deliveryStageNode(barrier, "", "barrier", policy))
	buildIDs := make([]string, 0, len(builds))
	for buildID := range builds {
		buildIDs = append(buildIDs, buildID)
	}
	sort.Strings(buildIDs)
	for _, buildID := range buildIDs {
		deliveryDAGEdge(dag, buildID, barrier, "settled")
	}
	groups := map[int][]string{}
	for _, snapshot := range targets {
		if snapshot.Target.Action != "build" {
			groups[snapshot.Target.Group] = append(groups[snapshot.Target.Group], snapshot.Target.ID)
		}
	}
	groupIDs := make([]int, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Ints(groupIDs)
	for i, groupID := range groupIDs {
		for _, targetID := range groups[groupID] {
			deliveryDAGEdge(dag, barrier, targetID+":plan", "success")
			if i == 0 {
				continue
			}
			for _, previous := range groups[groupIDs[i-1]] {
				deliveryDAGEdge(dag, last[previous], targetID+":plan", "settled")
			}
		}
	}
}

func validateDeliveryDAG(dag dagWorkflowDefinition) error {
	ids, _, err := indexDAGNodes(dag.Nodes)
	if err != nil {
		return err
	}
	if err := validateDAGEdges(dag.Edges, ids); err != nil {
		return err
	}
	indegree, children := map[string]int{}, map[string][]string{}
	for _, edge := range dag.Edges {
		indegree[edge.Target]++
		children[edge.Source] = append(children[edge.Source], edge.Target)
	}
	queue := []string{}
	for _, node := range dag.Nodes {
		if indegree[node.ID] == 0 {
			queue = append(queue, node.ID)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, child := range children[queue[i]] {
			indegree[child]--
			if indegree[child] == 0 {
				queue = append(queue, child)
			}
		}
	}
	if len(queue) != len(dag.Nodes) {
		return fmt.Errorf("%w: delivery dependencies conflict with the selected order or contain a cycle", apperrors.ErrInvalidArgument)
	}
	return nil
}

func deliveryNodeReadiness(incoming []dagWorkflowEdge, statuses map[string]string) (ready, skipped bool) {
	ready = true
	failedDependency := false
	for _, edge := range incoming {
		status := statuses[edge.Source]
		if !deliveryNodeTerminal(status) {
			ready = false
			continue
		}
		if edge.Condition != "settled" && status != "completed" {
			failedDependency = true
		}
	}
	if !ready {
		return false, false
	}
	if failedDependency {
		return false, true
	}
	return ready, false
}

func deliveryNodeTerminal(status string) bool {
	switch status {
	case "completed", "failed", "skipped", "canceled":
		return true
	default:
		return false
	}
}
