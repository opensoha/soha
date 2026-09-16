package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"

	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func TestDeliveryHistoryProjectionHidesForeignTargetsAndCounts(t *testing.T) {
	targets := deliveryTestTargets(3)
	targets[1].Target.DependsOn = []string{targets[0].Target.ID}
	batch := domainworkflow.DeliveryBatch{ID: "batch", RootRunID: "private-root-run", WorkflowID: "private-workflow", Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "secret-app release"}, Targets: targets, StopReason: "failure", StopSummary: "secret-app failed", Nodes: []domainworkflow.NodeRun{
		{NodeID: "service1:build", TargetID: "service1", Stage: "build", Status: "failed", Summary: "secret-app build failed"},
		{NodeID: "service2:build", TargetID: "service2", Stage: "build", Status: "completed", BuildRecordID: "visible-build"},
		{NodeID: "service2:health", TargetID: "service2", Stage: "health", Status: "completed"},
		{NodeID: "service3:health", TargetID: "service3", Stage: "health", Status: "completed"},
	}}
	projected := filterDeliveryBatch(batch, map[string]bool{"service2": true})
	if !projected.PartialView || projected.TargetCount != 1 || projected.ServiceCount != 1 || projected.BuildCount != 1 || projected.Status != "completed" {
		t.Fatalf("hidden target affected aggregate: %+v", projected)
	}
	body, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"service1", "service3", "secret-app", "private-workflow", "private-root-run"} {
		if strings.Contains(string(body), hidden) {
			t.Fatalf("history leaked %s: %s", hidden, body)
		}
	}
	if len(batch.Targets) != 3 || len(batch.Targets[1].Target.DependsOn) != 1 {
		t.Fatal("projection mutated persisted snapshot")
	}
	complete := filterDeliveryBatch(batch, map[string]bool{"service1": true, "service2": true, "service3": true})
	if complete.PartialView || complete.RootRunID != batch.RootRunID {
		t.Fatal("complete authorized history lost its run reference")
	}
}

func TestDeliveryBatchProjectionDoesNotExposeOrMutateInternalConfiguration(t *testing.T) {
	batch := domainworkflow.DeliveryBatch{Targets: []domainworkflow.DeliveryTargetSnapshot{{FrozenHelm: &sohaapi.HelmDeliverySnapshot{}, FrozenHelmCiphertext: "private ciphertext", FrozenBuild: &domainbuild.Prepared{Metadata: map[string]any{"commands": []string{"private command"}}}, FrozenManifest: &domainmanifest.DeliveryConfiguration{}, FrozenReleaseTarget: &domaincatalog.ReleaseTarget{Metadata: map[string]any{"private": "reference"}}}}}
	projected := projectDeliveryBatch(batch, domainworkflow.Run{Status: "running"})
	data, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "_frozen") || strings.Contains(string(data), "private") {
		t.Fatal("internal configuration leaked", string(data))
	}
	if batch.Targets[0].FrozenBuild == nil || batch.Targets[0].FrozenManifest == nil || batch.Targets[0].FrozenReleaseTarget == nil {
		t.Fatal("projection mutated persisted snapshot")
	}
}

func TestHelmBatchResourceKeysConflictWithManifestAndSameRelease(t *testing.T) {
	helm := domainworkflow.DeliveryTargetSnapshot{Target: domainworkflow.DeliveryTargetInput{ID: "helm"}, FrozenHelm: &sohaapi.HelmDeliverySnapshot{ClusterID: "cluster", Namespace: "test", ReleaseName: "app", Resources: []sohaapi.HelmDeliveryResource{{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "test", Name: "api"}}}}
	manifest := domainworkflow.DeliveryTargetSnapshot{Target: domainworkflow.DeliveryTargetInput{ID: "manifest"}, FrozenManifest: &domainmanifest.DeliveryConfiguration{ResourceKeys: []string{"cluster/apps/Deployment/test/api"}}}
	if err := validateDeliveryResourceTargets([]domainworkflow.DeliveryTargetSnapshot{helm, manifest}); err == nil {
		t.Fatal("Helm/Manifest resource overlap was accepted")
	}
	other := helm
	other.Target.ID = "other"
	copy := *helm.FrozenHelm
	copy.Resources = nil
	other.FrozenHelm = &copy
	if err := validateDeliveryResourceTargets([]domainworkflow.DeliveryTargetSnapshot{helm, other}); err == nil {
		t.Fatal("same native release with different resource keys was accepted")
	}
	copy.Namespace = "another"
	if err := validateDeliveryResourceTargets([]domainworkflow.DeliveryTargetSnapshot{helm, other}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryBatchCountsOnlyCreatedBuildRecords(t *testing.T) {
	batch := domainworkflow.DeliveryBatch{Targets: deliveryTestTargets(3)}
	run := domainworkflow.Run{NodeRuns: []domainworkflow.NodeRun{
		{NodeID: "service1:build", Stage: "build", Status: "running", BuildRecordID: "shared"},
		{NodeID: "service2:build", Stage: "build", Status: "completed", BuildRecordID: "shared"},
		{NodeID: "service3:build", Stage: "build", Status: "canceled"},
		{NodeID: "service1:deploy", Stage: "deploy", Status: "completed", BuildRecordID: "old-input"},
	}}
	if got := projectDeliveryBatch(batch, run).BuildCount; got != 1 {
		t.Fatalf("counted unstarted, duplicated or reused builds: %d", got)
	}
	run.NodeRuns[2].BuildRecordID = "failed-attempt"
	run.NodeRuns[2].Status = "failed"
	if got := projectDeliveryBatch(batch, run).BuildCount; got != 2 {
		t.Fatalf("lost a real failed build attempt: %d", got)
	}
}
