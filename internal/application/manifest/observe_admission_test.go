package manifest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Model the two independent write-impact checks in the original admission path.
func (r *staleReferenceImpactReader) GetConfirmedDeliveryPlan(context.Context, di.Principal, string) (dd.DeliveryPlan, error) {
	return dd.DeliveryPlan{}, r.err
}

func observationClaimFixture(t *testing.T) (*DeclarativeService, *snapshotRepository, dd.ExecutionTask) {
	t.Helper()
	s, repo, apply := publicationClaimFixture(t)
	repo.deployment.Status.LastExecutionTaskID = apply.ID
	requireSnapshotTestNoError(t, s.queueAutomaticAction(t.Context(), repo.deployment, dm.TaskActionObserve, time.Now()))
	for _, task := range repo.tasks {
		if task.TaskKind == taskKind(dm.TaskActionObserve) {
			return s, repo, task
		}
	}
	t.Fatal("observation was not queued")
	return nil, nil, dd.ExecutionTask{}
}

func TestManifestObservationIgnoresOnlyPublicationImpact(t *testing.T) {
	for _, scenario := range []string{"valid", "read-only-role", "permission-denied", "revoked", "subject", "missing-context", "other-operation", "payload", "force", "binding", "target-deleted", "target-disabled", "position", "policy", "unapproved", "runtime", "preflight-context", "documents", "generation", "snapshot", "preflight"} {
		t.Run(scenario, func(t *testing.T) {
			s, repo, task := observationClaimFixture(t)
			reader := &staleReferenceImpactReader{snapshotRepository: repo, err: apperrors.ErrConflict}
			s.delivery = reader
			mutatePublicationClaim(t, scenario, s, repo, &task)
			switch scenario {
			case "read-only-role", "permission-denied":
				principal := testPrincipal()
				principal.Roles = []string{"editor"}
				if scenario == "permission-denied" {
					principal.Roles = []string{"no-access"}
				}
				s.executionPrincipals = &rootExecutionPrincipal{principal: principal}
			case "documents":
				payload, err := decodeTaskPayload(task.Payload)
				requireSnapshotTestNoError(t, err)
				payload.Documents[0].Content = "tampered"
				task.Payload = structMap(payload)
			case "generation":
				repo.deployment.Generation++
			case "snapshot":
				snapshot := *repo.deployment.Spec.DeliverySnapshot
				snapshot.RenderedDigest = "tampered"
				repo.deployment.Spec.DeliverySnapshot = &snapshot
			case "preflight":
				id := repo.deployment.Spec.DeliverySnapshot.PreflightTaskID
				preflight := repo.tasks[id]
				preflight.Status = "failed"
				repo.tasks[id] = preflight
			}
			before := repo.setCount
			count := len(repo.tasks)
			original := task
			err := s.ValidateRootCRExecutionTask(t.Context(), task)
			if scenario == "valid" || scenario == "read-only-role" {
				if err != nil {
					t.Fatalf("read-only observation blocked by stale write impact: %v", err)
				}
			} else {
				want := apperrors.ErrConflict
				if scenario == "permission-denied" {
					want = apperrors.ErrAccessDenied
				}
				if scenario == "revoked" || scenario == "subject" {
					want = apperrors.ErrUnauthorized
				}
				if !errors.Is(err, want) {
					t.Fatalf("observation guard %s: got %v, want %v", scenario, err, want)
				}
			}
			if reader.calls != 0 || before != repo.setCount || count != len(repo.tasks) || !reflect.DeepEqual(original, task) {
				t.Fatal("observation checked write impact or mutated execution")
			}
		})
	}
}

type legacyObservationPlanReader struct {
	DeliveryTaskReader
	deliveryContextReader
}

func (r legacyObservationPlanReader) ValidateDeliveryPlanServiceReferenceImpact(context.Context, di.Principal, string) error {
	return nil
}

func TestManifestObservationRequiresDedicatedPlanReader(t *testing.T) {
	s, repo, task := observationClaimFixture(t)
	s.delivery = legacyObservationPlanReader{DeliveryTaskReader: repo, deliveryContextReader: repo}
	if err := s.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("missing observation reader did not fail closed: %v", err)
	}
}
