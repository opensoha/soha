package delivery

import (
	"context"
	"maps"
	"strings"
	"testing"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func TestBatchSharedBuildPromotesWithOriginalProvenanceAndEnvironmentApproval(t *testing.T) {
	service, repo, builder, manifest, catalog, run, batch := batchRuntimeCheck(t)
	dev := catalog.bindings[0]
	dev.ID = "dev"
	catalog.bindings = append(catalog.bindings, dev)
	repo.bundle.ApplicationEnvironmentID = "dev"
	batchRuntimeStep(t, service, &run, batch, 0, "waiting_execution")
	repo.status(run.NodeRuns[0].ExecutionTaskID, "completed")
	batchRuntimeStep(t, service, &run, batch, 0, "completed")
	ctx := domainworkflow.WithNodeExecution(context.Background(), run, run.NodeRuns[1])
	var bundleID string
	for range 2 {
		var err error
		bundleID, err = service.batchTargetBundle(ctx, deliveryActionPrincipal(), batch.Targets[0].Target, repo.bundle.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	promoted := repo.promotions[bundleID]
	if len(repo.promotions) != 1 || bundleID == repo.bundle.ID || promoted.ApplicationEnvironmentID != "env" || repo.bundle.ApplicationEnvironmentID != "dev" || promoted.ArtifactDigest != repo.bundle.ArtifactDigest || promoted.Metadata["sourceBuildTaskId"] != run.NodeRuns[0].ExecutionTaskID || promoted.Metadata["sourceReleaseBundleId"] != repo.bundle.ID {
		t.Fatalf("promotion lost immutable build or scope: %+v", promoted)
	}
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_execution")
	repo.status(run.NodeRuns[1].ExecutionTaskID, "completed")
	batchRuntimeStep(t, service, &run, batch, 1, "waiting_approval")
	if repo.plan.ReleaseBundleID != promoted.ID || !repo.plan.RequiresApproval || len(manifest.applied) != 0 {
		t.Fatal("target environment approval was bypassed")
	}
	if _, err := service.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
		t.Fatal(err)
	}
	batchRuntimeStep(t, service, &run, batch, 1, "completed")
	batchRuntimeStep(t, service, &run, batch, 2, "waiting_execution")
	if builder.created != 1 || len(manifest.applied) != 1 {
		t.Fatal("promotion rebuilt the image or failed to deploy")
	}
	verifyPromotionProvenance(t, service, repo, promoted)
}

func verifyPromotionProvenance(t *testing.T, service *Service, repo *batchRuntimeRepository, promoted domaindelivery.ReleaseBundle) {
	t.Helper()
	for _, mismatch := range []string{"application", "digest", "build-task", "source", "cycle"} {
		t.Run(mismatch, func(t *testing.T) {
			bad := promoted
			bad.Metadata = maps.Clone(promoted.Metadata)
			switch mismatch {
			case "application":
				bad.ApplicationID = "other"
			case "digest":
				bad.ArtifactDigest = "sha256:" + strings.Repeat("b", 64)
			case "build-task":
				bad.Metadata["sourceBuildTaskId"] = "other"
			case "source":
				bad.Metadata["sourceReleaseBundleId"] = "missing"
			case "cycle":
				bad.Metadata["sourceReleaseBundleId"] = promoted.ID
			}
			repo.promotions[promoted.ID] = bad
			defer func() { repo.promotions[promoted.ID] = promoted }()
			if _, err := service.manifestArtifacts(context.Background(), deliveryActionPrincipal(), "app", "svc", promoted.ID); err == nil {
				t.Fatal("accepted altered promotion provenance")
			}
		})
	}
}
