package manifest

import (
	"errors"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestInitializationClaimRevalidatesIdentityModeAndApprovedPlan(t *testing.T) {
	s, repo, target, selectVersion := newMigrationSnapshotFixture()
	selectVersion(1)
	repo.deployment = domainmanifest.Deployment{}
	snap, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "first", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	approveSnapshotPreflight(repo, snap)
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snap)
	requireSnapshotTestNoError(t, err)
	fixture := &rolloutTaskFixture{snapshotRepository: repo, plan: domaindelivery.DeliveryPlan{ID: "first", ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", Status: "confirmed", RequiresApproval: true, ManifestSnapshots: []domainmanifest.DeliverySnapshot{snap}}}
	identity := &rootExecutionPrincipal{principal: testPrincipal()}
	s.repository, s.delivery, s.executionPrincipals = fixture, fixture, identity
	requireSnapshotTestNoError(t, s.ValidateRootCRExecutionTask(t.Context(), task))
	protectedProvider := task.ProviderKind
	if protectedProvider != domainmanifest.TaskProviderDirectInitialization {
		t.Fatalf("initialization queued for legacy worker: %s", protectedProvider)
	}
	task.ProviderKind = domainmanifest.TaskProviderDirect
	if !errors.Is(s.ValidateRootCRExecutionTask(t.Context(), task), apperrors.ErrConflict) {
		t.Fatal("initialization downgraded to legacy Direct provider")
	}
	task.ProviderKind = protectedProvider
	identity.err = apperrors.ErrUnauthorized
	if !errors.Is(s.ValidateRootCRExecutionTask(t.Context(), task), apperrors.ErrUnauthorized) {
		t.Fatal("revoked identity retained first publication")
	}
	identity.err = nil
	fixture.plan.RequiresApproval = false
	if !errors.Is(s.ValidateRootCRExecutionTask(t.Context(), task), apperrors.ErrConflict) {
		t.Fatal("first publication lost mandatory approval")
	}
	fixture.plan.RequiresApproval = true
	payload, err := decodeTaskPayload(task.Payload)
	requireSnapshotTestNoError(t, err)
	payload.InitializationPlanID = ""
	task.Payload = structMap(payload)
	if !errors.Is(s.ValidateRootCRExecutionTask(t.Context(), task), apperrors.ErrConflict) {
		t.Fatal("initialization downgraded to ordinary update")
	}
}

func TestFirstTemplateReleaseDistinguishesUnknownBaselineAndFreezesCreateOnly(t *testing.T) {
	for _, mode := range []string{"new", "desired-only", "previous-write", "existing-unknown", "missing-mode-proof"} {
		t.Run(mode, func(t *testing.T) {
			checkFirstTemplateRelease(t, mode)
		})
	}
}

func TestInitializationBaselineWaitsForHealthyOriginalUID(t *testing.T) {
	s, repo, target, selectVersion := newMigrationSnapshotFixture()
	selectVersion(4)
	repo.deployment = domainmanifest.Deployment{}
	snap, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "first", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	approveSnapshotPreflight(repo, snap)
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snap)
	requireSnapshotTestNoError(t, err)
	s.repository = &migrationStatusRepository{snapshotRepository: repo}
	p, err := decodeTaskPayload(task.Payload)
	requireSnapshotTestNoError(t, err)
	doc := p.Documents[0]
	inventory := []domainmanifest.ResourceInventory{{APIVersion: doc.APIVersion, Kind: doc.Kind, Namespace: doc.Namespace, Name: doc.Name, UID: "created", DesiredObjectDigest: doc.ContentDigest, Health: "progressing"}}
	task.Status = "completed"
	task.Result = structMap(domainmanifest.TaskResult{InitializationPlanID: "first", Inventory: inventory})
	requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, p))
	if repo.deployment.Status.AppliedTemplate != nil {
		t.Fatal("unhealthy apply fabricated baseline")
	}
	p.Action = domainmanifest.TaskActionObserve
	p.Inventory = repo.deployment.Status.Inventory
	replaced := append([]domainmanifest.ResourceInventory(nil), inventory...)
	replaced[0].UID = "replacement"
	replaced[0].Health = "healthy"
	task.Result = structMap(domainmanifest.TaskResult{InitializationPlanID: "first", Inventory: replaced})
	if !errors.Is(s.recordDeploymentTaskResult(t.Context(), task, p), apperrors.ErrConflict) || repo.deployment.Status.AppliedTemplate != nil {
		t.Fatal("same-name replacement confirmed baseline")
	}
	inventory[0].Health = "healthy"
	task.Result = structMap(domainmanifest.TaskResult{InitializationPlanID: "first", Inventory: inventory})
	requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, p))
	ref := repo.deployment.Status.AppliedTemplate
	if ref == nil || ref.TemplateVersion != 4 || ref.TemplateDigest != "digest4" {
		t.Fatalf("healthy original UID did not confirm baseline %+v", ref)
	}
}

func TestInitializationCannotCreateImportedRootOrController(t *testing.T) {
	for _, mode := range []string{"root", "gitops", "rollout", "force"} {
		t.Run(mode, func(t *testing.T) {
			p := domainmanifest.TaskPayload{InitializationPlanID: "first", Action: "apply", Documents: []domainmanifest.RenderedDocument{{APIVersion: "v1", Kind: "ConfigMap"}}}
			switch mode {
			case "root":
				p.RootResource = &sohaapi.ManifestRootResource{}
			case "gitops":
				p.GitOpsDocuments = p.Documents
			case "rollout":
				p.Documents[0].APIVersion = "argoproj.io/v1alpha1"
				p.Documents[0].Kind = "Rollout"
			case "force":
				p.ForceConflicts = true
			}
			if validateInitializationPayload(p) == nil {
				t.Fatal("unsupported first publication fell through to update")
			}
		})
	}
}

func checkFirstTemplateRelease(t *testing.T, mode string) {
	t.Helper()

	s, repo, target, selectVersion := newMigrationSnapshotFixture()
	selectVersion(4)
	repo.deployment = domainmanifest.Deployment{}
	switch mode {
	case "desired-only":
		repo.deployment = domainmanifest.Deployment{ID: "desired", Generation: 1, Status: domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhasePending}}
	case "previous-write":
		repo.history = true
	case "existing-unknown":
		repo.deployment = domainmanifest.Deployment{ID: "old", Status: domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhaseConverged}}
	}
	snap, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "first", domainmanifest.DeliveryArtifacts{})
	if mode == "previous-write" || mode == "existing-unknown" {
		if !errors.Is(err, apperrors.ErrConflict) || len(repo.tasks) != 0 {
			t.Fatalf("old target treated as new %v", err)
		}
		return
	}
	requireSnapshotTestNoError(t, err)
	if snap.Migration == nil || !snap.Migration.Initialization || !snap.Migration.RequiresApproval || snap.Migration.FromVersion != 0 || snap.Migration.ToVersion != 4 || snap.Migration.Blocked || len(snap.Migration.Steps) != 2 {
		t.Fatalf("first release not distinct: %+v", snap.Migration)
	}
	task := repo.tasks[snap.PreflightTaskID]
	p, err := decodeTaskPayload(task.Payload)
	requireSnapshotTestNoError(t, err)
	if p.InitializationPlanID != "first" || p.ForceConflicts {
		t.Fatal("lost create-only intent")
	}
	approveSnapshotPreflight(repo, snap)
	if mode == "missing-mode-proof" {
		task = repo.tasks[snap.PreflightTaskID]
		task.Result = structMap(domainmanifest.TaskResult{Preflight: &domainmanifest.PreflightResult{Ready: true, RenderedDigest: snap.RenderedDigest}})
		repo.tasks[task.ID] = task
		if err := s.ValidateDeliverySnapshot(t.Context(), testPrincipal(), snap); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("ordinary preflight accepted %v", err)
		}
		return
	}
	_, apply, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snap)
	requireSnapshotTestNoError(t, err)
	p, err = decodeTaskPayload(apply.Payload)
	requireSnapshotTestNoError(t, err)
	if p.InitializationPlanID != "first" {
		t.Fatal("apply dropped initialization")
	}
}
