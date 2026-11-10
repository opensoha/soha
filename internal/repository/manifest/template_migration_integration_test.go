package manifest_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	manifestrepo "github.com/opensoha/soha/internal/repository/manifest"
	"gorm.io/gorm"
)

func verifyMigrationBaseline(t *testing.T, ctx context.Context, tx *gorm.DB, repo *manifestrepo.Repository, deployment domainmanifest.Deployment) {
	t.Helper()
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "0121_template_migration_policy.sql"))
	requireManifestRepositoryNoError(t, err)
	_, backfill, ok := strings.Cut(string(migration), "UPDATE manifest_deployment_status")
	if !ok {
		t.Fatal("migration backfill missing")
	}
	for _, scenario := range []string{"verified", "desired-only", "stale-generation", "failed", "digest-mismatch", "malformed-identity"} {
		t.Run(scenario, func(t *testing.T) {
			requireManifestRepositoryNoError(t, tx.SavePoint("migration_baseline").Error)
			defer func() { requireManifestRepositoryNoError(t, tx.RollbackTo("migration_baseline").Error) }()
			snapshot := *deployment.Spec.DeliverySnapshot
			snapshot.TemplateInputs = &domainmanifest.ServiceTemplateInputs{TemplateID: "template", TemplateVersion: 1, TemplateDigest: "sha256:published"}
			if scenario == "malformed-identity" {
				snapshot.TemplateInputs.TemplateVersion = 0
			}
			encoded, err := json.Marshal(snapshot)
			requireManifestRepositoryNoError(t, err)
			requireManifestRepositoryNoError(t, tx.Exec(`UPDATE manifest_deployments SET delivery_snapshot=?::jsonb WHERE id=?`, string(encoded), deployment.ID).Error)
			status := domainmanifest.DeploymentStatus{Phase: "converged", AppliedDigest: snapshot.RenderedDigest, ObservedGeneration: deployment.Generation, Conditions: []domainmanifest.Condition{{Type: "Synced", Status: "true", Reason: "TaskCompleted", ObservedGeneration: deployment.Generation, LastTransitionAt: time.Now().UTC(), EvidenceRefs: []string{"verified-apply"}}}}
			switch scenario {
			case "desired-only":
				status.Conditions = nil
			case "stale-generation":
				status.Conditions[0].ObservedGeneration--
			case "failed":
				status.Conditions[0].Status = "false"
			case "digest-mismatch":
				status.AppliedDigest = "old-render"
			}
			requireManifestRepositoryNoError(t, repo.UpdateDeploymentStatus(ctx, deployment.ID, deployment.Generation, status))
			requireManifestRepositoryNoError(t, tx.Exec("UPDATE manifest_deployment_status"+backfill).Error)
			loaded, err := repo.GetDeployment(ctx, deployment.ID)
			requireManifestRepositoryNoError(t, err)
			if (loaded.Status.AppliedTemplate != nil) != (scenario == "verified") {
				t.Fatalf("fabricated or missing successful baseline %#v", loaded.Status.AppliedTemplate)
			}
			if scenario == "verified" {
				expected := &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 1, TemplateDigest: "sha256:published"}
				if *loaded.Status.AppliedTemplate != *expected {
					t.Fatal("identity lost")
				}
				loaded.Spec.DeliverySnapshot.TemplateInputs.TemplateVersion = 2
				loaded.Spec.DeliverySnapshot.RenderedDigest = "new-render"
				loaded.Spec.DesiredDigest = "new-render"
				next, err := repo.SetDesiredRevision(ctx, loaded, loaded.Generation)
				requireManifestRepositoryNoError(t, err)
				if next.Status.AppliedTemplate == nil || *next.Status.AppliedTemplate != *expected {
					t.Fatal("desired selection replaced last successful identity")
				}
				if err = repo.UpdateDeploymentStatus(ctx, deployment.ID, deployment.Generation, status); !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("late callback overwrote baseline %v", err)
				}
			}
		})
	}
}
