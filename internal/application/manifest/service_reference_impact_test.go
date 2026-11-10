package manifest

import (
	"context"
	"errors"
	di "github.com/opensoha/soha/internal/domain/identity"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
	"testing"
)

func (r *snapshotRepository) ValidateDeliveryPlanServiceReferenceImpact(context.Context, di.Principal, string) error {
	return r.planError
}

type staleReferenceImpactReader struct {
	*snapshotRepository
	err   error
	calls int
}

func (r *staleReferenceImpactReader) ValidateDeliveryPlanServiceReferenceImpact(context.Context, di.Principal, string) error {
	r.calls++
	return r.err
}

func TestManifestPreflightRejectsStalePrivateServiceReferenceImpact(t *testing.T) {
	for _, kind := range []string{"preflight", "apply"} {
		for _, failure := range []error{nil, apperrors.ErrConflict, apperrors.ErrAccessDenied} {
			t.Run(kind+"/"+errorLabel(failure), func(t *testing.T) {
				service, repo, task := publicationClaimFixture(t)
				if kind == "preflight" {
					task = repo.tasks[repo.deployment.Spec.DeliverySnapshot.PreflightTaskID]
				}
				reader := &staleReferenceImpactReader{snapshotRepository: repo, err: failure}
				service.delivery = reader
				before := repo.setCount
				taskCount := len(repo.tasks)
				original := task
				err := service.ValidateRootCRExecutionTask(t.Context(), task)
				if failure == nil && err != nil || failure != nil && !errors.Is(err, failure) {
					t.Fatalf("private evidence validation mismatch: %v", err)
				}
				if reader.calls != 1 || before != repo.setCount || len(repo.tasks) != taskCount || !reflect.DeepEqual(task, original) {
					t.Fatal("validation skipped or changed execution")
				}
			})
		}
	}
}

func errorLabel(err error) string {
	if err == nil {
		return "current"
	}
	if errors.Is(err, apperrors.ErrAccessDenied) {
		return "revoked"
	}
	return "stale"
}

func TestPreparedManifestPreflightIsNotClaimableBeforePlanCommit(t *testing.T) {
	service, repo, _, target := newSnapshotTestService()
	ctx, _ := dm.WithPreparedOperations(t.Context())
	snapshot, err := service.CreateDeliverySnapshot(ctx, testPrincipal(), "payments", "payments-dev", target, 1, "prepared-plan", dm.DeliveryArtifacts{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PreflightTaskID == "" || len(repo.tasks) != 0 || len(repo.operations) != 0 {
		t.Fatal("preflight persisted while Plan is only preparing")
	}
	// A worker polling this exact reserved ID sees no task and cannot poison its status.
	if _, exists := repo.tasks[snapshot.PreflightTaskID]; exists {
		t.Fatal("worker can claim premature preflight")
	}
	if err := dm.FlushPreparedOperations(ctx); err != nil {
		t.Fatal(err)
	}
	task, exists := repo.tasks[snapshot.PreflightTaskID]
	if !exists || task.Status != "queued" || repo.operations[task.ID].DeliveryContext == nil {
		t.Fatal("committed preflight lost its protected context")
	}
	service.executionPrincipals = &rootExecutionPrincipal{principal: testPrincipal()}
	if err := service.ValidateRootCRExecutionTask(t.Context(), task); err != nil {
		t.Fatalf("persisted preflight failed guard: %v", err)
	}
}

func TestPreparedManifestPreflightRechecksDatabaseScopeBeforePublishing(t *testing.T) {
	service, repo, _, target := newSnapshotTestService()
	ctx, _ := dm.WithPreparedOperations(t.Context())
	if _, err := service.CreateDeliverySnapshot(ctx, testPrincipal(), "payments", "payments-dev", target, 1, "changed-preparation", dm.DeliveryArtifacts{}); err != nil {
		t.Fatal(err)
	}
	repo.scopeRevision = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := dm.FlushPreparedOperations(ctx); !errors.Is(err, apperrors.ErrConflict) || len(repo.tasks) != 0 || len(repo.operations) != 0 {
		t.Fatalf("stale staged scope became executable: %v", err)
	}
}
