package manifest

import (
	"errors"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
	"testing"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func publicationClaimFixture(t *testing.T) (*DeclarativeService, *snapshotRepository, domaindelivery.ExecutionTask) {
	t.Helper()
	s, repo, _, target := newSnapshotTestService()
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "publication", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	approveSnapshotPreflight(repo, snapshot)
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	s.executionPrincipals = &rootExecutionPrincipal{principal: testPrincipal()}
	return s, repo, task
}

func TestManifestPublicationClaimRestoresDurableControl(t *testing.T) {
	for _, scenario := range []string{"valid", "retry", "revoked", "permission", "subject", "missing-context", "other-operation", "action", "payload", "force", "binding", "target-deleted", "target-disabled", "position", "policy", "unapproved", "runtime", "preflight-context"} {
		t.Run(scenario, func(t *testing.T) {
			s, repo, task := publicationClaimFixture(t)
			mutatePublicationClaim(t, scenario, s, repo, &task)
			err := s.ValidateRootCRExecutionTask(t.Context(), task)
			if scenario == "valid" || scenario == "retry" {
				if err != nil {
					t.Fatalf("valid publication blocked: %v", err)
				}
				return
			}
			want := apperrors.ErrConflict
			switch scenario {
			case "revoked", "subject":
				want = apperrors.ErrUnauthorized
			case "permission":
				want = apperrors.ErrAccessDenied
			}
			if !errors.Is(err, want) {
				t.Fatalf("unsafe publication accepted: %s, %v", scenario, err)
			}
		})
	}
}

func mutatePublicationClaim(t *testing.T, scenario string, s *DeclarativeService, repo *snapshotRepository, task *domaindelivery.ExecutionTask) {
	t.Helper()
	switch scenario {
	case "retry":
		task.AttemptCount = 2
	case "revoked":
		s.executionPrincipals = &rootExecutionPrincipal{err: apperrors.ErrUnauthorized}
	case "permission":
		principal := task.SecretPrincipal
		principal.Roles = []string{"editor"}
		s.executionPrincipals = &rootExecutionPrincipal{principal: principal}
	case "subject":
		s.executionPrincipals = &rootExecutionPrincipal{principal: domainidentity.Principal{UserID: "another"}}
	case "missing-context":
		operation := repo.operations[task.ID]
		operation.DeliveryContext = nil
		repo.operations[task.ID] = operation
	case "other-operation":
		operation := repo.operations[task.ID]
		operation.ExecutionTaskID = "other"
		repo.operations[task.ID] = operation
	case "action":
		task.Payload["action"] = "repair"
	case "payload":
		task.Payload["renderedDigest"] = "different"
	case "force":
		task.Payload["forceConflicts"] = true
	case "binding":
		repo.binding.Version++
	case "target-deleted":
		repo.target.ID = "recreated"
	case "target-disabled":
		repo.target.Enabled = false
	case "position":
		repo.target.Namespace = "moved"
	case "policy":
		repo.scopeRevision = strings.Repeat("c", 64)
	case "unapproved":
		repo.planError = apperrors.ErrConflict
	case "runtime":
		task.ProviderKind = "ci_agent_runner"
	case "preflight-context":
		operation := repo.operations[task.ID]
		control := *operation.DeliveryContext
		control.ScopeRevision = strings.Repeat("c", 64)
		operation.DeliveryContext = &control
		repo.operations[task.ID] = operation
		repo.scopeRevision = control.ScopeRevision
	}
}

func TestManifestPublicationControlApprovalNeedsNoPublishPermission(t *testing.T) {
	s, repo, _, target := newSnapshotTestService()
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "approval", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	approver := domainidentity.Principal{UserID: "approver", Roles: []string{"custom-approver"}}
	if err := s.ValidateDeliveryControl(t.Context(), approver, snapshot); err != nil {
		t.Fatalf("read-only control validation required deployment permission: %v", err)
	}
	repo.scopeRevision = strings.Repeat("d", 64)
	if err := s.ValidateDeliveryControl(t.Context(), approver, snapshot); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale policy approved: %v", err)
	}
}

func TestManifestPublicationClaimPreflightAndObservationUseFreshIdentity(t *testing.T) {
	s, repo, task := publicationClaimFixture(t)
	snapshot := repo.deployment.Spec.DeliverySnapshot
	preflight := repo.tasks[snapshot.PreflightTaskID]
	if err := s.ValidateRootCRExecutionTask(t.Context(), preflight); err != nil {
		t.Fatalf("preflight required a confirming Plan: %v", err)
	}
	repo.deployment.Status.LastExecutionTaskID = task.ID
	repo.tasks[task.ID] = task
	before := len(repo.tasks)
	if err := s.queueAutomaticAction(t.Context(), repo.deployment, domainmanifest.TaskActionObserve, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(repo.tasks) != before+1 {
		t.Fatal("automatic observation did not retain a durable task")
	}
	for id, observation := range repo.tasks {
		if id == task.ID || id == snapshot.PreflightTaskID {
			continue
		}
		if observation.SecretPrincipal.UserID != task.SecretPrincipal.UserID || repo.operations[id].DeliveryContext == nil {
			t.Fatal("observation lost original identity or publication context")
		}
		if err := s.ValidateRootCRExecutionTask(t.Context(), observation); err != nil {
			t.Fatal(err)
		}
		s.executionPrincipals = &rootExecutionPrincipal{err: apperrors.ErrUnauthorized}
		if !errors.Is(s.ValidateRootCRExecutionTask(t.Context(), observation), apperrors.ErrUnauthorized) {
			t.Fatal("revoked observation retained access")
		}
	}
}

func TestLegacyRootObservationCannotMasqueradeAsCurrentApplicationPlan(t *testing.T) {
	for _, manager := range []string{"opensoha-delivery/v1", "opensoha-manifest/binding"} {
		t.Run(manager, func(t *testing.T) {
			service, _, task := publicationClaimFixture(t)
			task.ProviderKind = domainmanifest.TaskProviderDirect
			payload := domainmanifest.TaskPayload{Action: domainmanifest.TaskActionObserve, FieldManager: manager, RootResource: &sohaapi.ManifestRootResource{UID: "existing"}}
			err := service.validateLegacyManifestTask(t.Context(), task, payload)
			if manager == "opensoha-manifest/binding" && !errors.Is(err, apperrors.ErrConflict) || manager == "opensoha-delivery/v1" && err != nil {
				t.Fatalf("manager=%s err=%v", manager, err)
			}
		})
	}
}
