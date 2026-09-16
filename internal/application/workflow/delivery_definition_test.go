package workflow

import (
	"encoding/json"
	"fmt"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"testing"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func deliveryTestTargets(count int) []domainworkflow.DeliveryTargetSnapshot {
	items := make([]domainworkflow.DeliveryTargetSnapshot, count)
	for i := range items {
		id := fmt.Sprintf("service%d", i+1)
		items[i] = domainworkflow.DeliveryTargetSnapshot{
			Target:      domainworkflow.DeliveryTargetInput{ID: id, ApplicationID: fmt.Sprintf("app%d", i/4), ServiceID: id, ApplicationEnvironmentID: "dev", Action: "build_deploy"},
			BuildNodeID: id + ":build",
		}
	}
	return items
}

func TestDeliveryDAGOrderDoesNotGrantSuccessfulDependency(t *testing.T) {
	targets := deliveryTestTargets(10)
	targets[4].Target.DependsOn = []string{targets[2].Target.ID}
	definition := domainworkflow.DeliveryWorkflowDefinition{Mode: domainworkflow.DeliveryModeSerial}
	dag, err := compileDeliveryDAG(definition, targets)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{"service2:health": "completed", "service3:build": "failed", "service3:health": "skipped", "service4:health": "completed"}
	if ready, skip := resolveDAGNodeReadiness(dag, nodeByID(dag, "service4:build"), incomingEdgesForNode(dag, "service4:build"), statuses); !ready || skip {
		t.Fatalf("independent successor blocked: %v %v", ready, skip)
	}
	if ready, skip := resolveDAGNodeReadiness(dag, nodeByID(dag, "service5:build"), incomingEdgesForNode(dag, "service5:build"), statuses); ready || !skip {
		t.Fatalf("failed strong dependency was ignored: %v %v", ready, skip)
	}
	statuses["service4:health"] = "running"
	if ready, skip := deliveryNodeReadiness(incomingEdgesForNode(dag, "service5:build"), statuses); ready || skip {
		t.Fatal("dependency skip bypassed an unfinished ordering predecessor")
	}
	statuses["service3:health"] = "canceling"
	if ready, skip := deliveryNodeReadiness(incomingEdgesForNode(dag, "service4:build"), statuses); ready || skip {
		t.Fatal("unconfirmed cancellation advanced ordering")
	}
	if len(dag.Nodes) != 40 {
		t.Fatalf("not one graph with 40 stages: %d", len(dag.Nodes))
	}
}

func TestDeliveryDAGBuildBarrierAndSharedArtifacts(t *testing.T) {
	targets := deliveryTestTargets(20)
	for i := range targets {
		targets[i].BuildNodeID = fmt.Sprintf("build%d", i%10)
		targets[i].Target.Group = i / 10
	}
	dag, err := compileDeliveryDAG(domainworkflow.DeliveryWorkflowDefinition{Mode: domainworkflow.DeliveryModeBuildAll}, targets)
	if err != nil {
		t.Fatal(err)
	}
	buildCount := 0
	for _, node := range dag.Nodes {
		if node.Stage == "build" {
			buildCount++
		}
	}
	if buildCount != 10 {
		t.Fatalf("20 targets should reuse 10 builds, got %d", buildCount)
	}
	statuses := map[string]string{}
	for i := 0; i < 10; i++ {
		statuses[fmt.Sprintf("build%d", i)] = "completed"
	}
	statuses["build2"] = "failed"
	if ready, skip := deliveryNodeReadiness(incomingEdgesForNode(dag, "builds:barrier"), statuses); !ready || skip {
		t.Fatal("build barrier should wait for terminal results")
	}
	statuses["builds:barrier"] = "completed"
	if ready, skip := deliveryNodeReadiness(incomingEdgesForNode(dag, "service3:plan"), statuses); ready || !skip {
		t.Fatal("a failed build became deployable")
	}
	for i := 1; i <= 10; i++ {
		statuses[fmt.Sprintf("service%d:health", i)] = "completed"
	}
	if ready, skip := deliveryNodeReadiness(incomingEdgesForNode(dag, "service13:plan"), statuses); ready || !skip {
		t.Fatal("shared build failure was ignored by a later consumer")
	}
	for i := 1; i <= 10; i++ {
		statuses[fmt.Sprintf("service%d:health", i)] = "completed"
	}
	statuses["service3:health"] = "running"
	if ready, skip := deliveryNodeReadiness(incomingEdgesForNode(dag, "service11:plan"), statuses); ready || skip {
		t.Fatal("next group passed unfinished health")
	}
}

func TestDeliveryDAGRejectsOrderDependencyCycles(t *testing.T) {
	for _, mode := range []string{domainworkflow.DeliveryModeSerial, domainworkflow.DeliveryModeBuildAll} {
		targets := deliveryTestTargets(2)
		targets[0].Target.DependsOn = []string{targets[1].Target.ID}
		targets[1].Target.Group = 1
		if _, err := compileDeliveryDAG(domainworkflow.DeliveryWorkflowDefinition{Mode: mode}, targets); err == nil {
			t.Fatalf("%s accepted conflicting order", mode)
		}
	}
	targets := deliveryTestTargets(2)
	targets[1].BuildNodeID = targets[0].BuildNodeID
	targets[1].Target.DependsOn = []string{targets[0].Target.ID}
	if _, err := compileDeliveryDAG(domainworkflow.DeliveryWorkflowDefinition{Mode: domainworkflow.DeliveryModeSerial}, targets); err != nil {
		t.Fatalf("shared build introduced a false cycle: %v", err)
	}
}

func TestDeliveryDefinitionDefaultsAndScopeValidation(t *testing.T) {
	input := domainworkflow.DeliveryWorkflowDefinition{Name: "release", Targets: []domainworkflow.DeliveryTargetInput{{ID: "web", ApplicationID: "a", ServiceID: "s", Action: "build"}}}
	definition, err := normalizeDeliveryDefinition(input)
	if err != nil || !definition.StopsOnFailure() || definition.Mode != domainworkflow.DeliveryModeSerial {
		t.Fatalf("defaults: %+v %v", definition, err)
	}
	definition.Targets[0].Action = "deploy"
	if _, err := normalizeDeliveryDefinition(definition); err == nil {
		t.Fatal("deployment accepted without environment or artifact")
	}
	input.Targets = append(input.Targets, input.Targets[0])
	if _, err := normalizeDeliveryDefinition(input); err == nil {
		t.Fatal("duplicate service target accepted")
	}
}

func TestHelmRollbackRequiresConfigurationActionWithoutArtifactOverride(t *testing.T) {
	input := domainworkflow.DeliveryTargetInput{ID: "helm", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "dev", Action: "config_update", HelmRevision: 1}
	if err := validateDeliveryTarget(input); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"build", "build_deploy", "deploy"} {
		invalid := input
		invalid.Action = action
		if err := validateDeliveryTarget(invalid); err == nil {
			t.Fatalf("rollback accepted action %s", action)
		}
	}
	input.ReleaseBundleID = "new-bundle"
	if err := validateDeliveryTarget(input); err == nil {
		t.Fatal("rollback accepted artifact override")
	}
}

func TestDeliveryNodeScopeSurvivesStoredJSON(t *testing.T) {
	node := deliveryStageNode("web:plan", "web", "plan", "stop")
	run := syncRunNodeState(domainworkflow.Run{Metadata: map[string]any{}}, dagWorkflowDefinition{Mode: domainworkflow.ScopeDeliveryBatch, Nodes: []dagWorkflowNode{node}}, initializeNodeRuns(dagWorkflowDefinition{Nodes: []dagWorkflowNode{node}}))
	run.Metadata["mode"] = domainworkflow.ScopeDeliveryBatch
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var restored domainworkflow.Run
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.NodeRuns[0].TargetID != "web" || restored.NodeRuns[0].Stage != "plan" {
		t.Fatalf("node lost target: %+v", restored.NodeRuns)
	}
}

func TestDeliveryRejectsOverlappingResourceTargets(t *testing.T) {
	a := domainworkflow.DeliveryTargetSnapshot{Target: domainworkflow.DeliveryTargetInput{ID: "api-dev"}, FrozenManifest: &domainmanifest.DeliveryConfiguration{ResourceKeys: []string{"cluster/apps/Deployment/dev/api"}}}
	b := a
	b.Target.ID = "worker-dev"
	if err := validateDeliveryResourceTargets([]domainworkflow.DeliveryTargetSnapshot{a, b}); err == nil {
		t.Fatal("overlapping resources accepted")
	}
	b.FrozenManifest = &domainmanifest.DeliveryConfiguration{ResourceKeys: []string{"cluster/apps/Deployment/prod/api"}}
	if err := validateDeliveryResourceTargets([]domainworkflow.DeliveryTargetSnapshot{a, b}); err != nil {
		t.Fatal(err)
	}
}
