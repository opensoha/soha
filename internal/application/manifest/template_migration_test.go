package manifest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/infrastructure/manifestruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type migrationTemplates struct {
	values []domaincatalog.DeploymentTemplate
}

type sourceMigrationAdmission bool

func (required sourceMigrationAdmission) ServiceTemplateMigrationRequired(context.Context, string, string) (bool, error) {
	return bool(required), nil
}

func TestUnprotectedServiceSourceRetainsLegacyAutomaticPromotion(t *testing.T) {
	s, repo, _, _ := newSnapshotTestService()
	repo.base.item.ServiceID = "plain-service"
	s.templateMigrations = sourceMigrationAdmission(false)
	requireSnapshotTestNoError(t, s.promoteRevision(t.Context(), repo.base.item, 1, "system:manifest-sync"))
	if repo.setCount != 1 || len(repo.tasks) != 1 {
		t.Fatal("legacy automatic source behavior changed")
	}
	s.templateMigrations = sourceMigrationAdmission(true)
	if err := s.promoteRevision(t.Context(), repo.base.item, 1, "system:manifest-sync"); !errors.Is(err, apperrors.ErrConflict) || repo.setCount != 1 || len(repo.tasks) != 1 {
		t.Fatalf("protected source changed desired before admission %v", err)
	}
}

type migrationStatusRepository struct{ *snapshotRepository }

func (r *migrationStatusRepository) GetDeployment(context.Context, string) (domainmanifest.Deployment, error) {
	return r.deployment, nil
}
func (r *migrationStatusRepository) UpdateDeploymentStatus(_ context.Context, _ string, generation int64, status domainmanifest.DeploymentStatus) error {
	if generation != r.deployment.Generation {
		return apperrors.ErrConflict
	}
	r.deployment.Status = status
	return nil
}

func TestMigrationBaselineAdvancesOnlyOnMatchingSuccessfulApply(t *testing.T) {
	s, repo, target, selectVersion := newMigrationSnapshotFixture()
	selectVersion(2)
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "adjacent", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	approveSnapshotPreflight(repo, snapshot)
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	s.repository = &migrationStatusRepository{snapshotRepository: repo}
	payload, err := decodeTaskPayload(task.Payload)
	requireSnapshotTestNoError(t, err)
	task.Status = "failed"
	requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, payload))
	if repo.deployment.Status.AppliedTemplate.TemplateVersion != 1 {
		t.Fatal("failed task promoted migration baseline")
	}
	task.Status = "running"
	requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, payload))
	if repo.deployment.Status.AppliedTemplate.TemplateVersion != 1 {
		t.Fatal("running task promoted migration baseline")
	}
	task.Status = "completed"
	requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, payload))
	if repo.deployment.Status.AppliedTemplate.TemplateVersion != 2 || repo.deployment.Status.AppliedDigest != snapshot.RenderedDigest {
		t.Fatal("successful apply did not promote exact template")
	}
	payload.Generation--
	payload.RenderedDigest = "stale"
	requireSnapshotTestNoError(t, s.recordDeploymentTaskResult(t.Context(), task, payload))
	if repo.deployment.Status.AppliedTemplate.TemplateVersion != 2 || repo.deployment.Status.AppliedDigest != snapshot.RenderedDigest {
		t.Fatal("late callback replaced current baseline")
	}
}

func (r *migrationTemplates) ListDeploymentTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.DeploymentTemplate, error) {
	return r.values, nil
}
func (r *migrationTemplates) GetDeploymentTemplateVersion(_ context.Context, _ domainidentity.Principal, _ string, n int64) (domaincatalog.DeploymentTemplate, error) {
	for _, v := range r.values {
		if v.PublishedVersion == n {
			return v, nil
		}
	}
	return domaincatalog.DeploymentTemplate{}, apperrors.ErrNotFound
}

func newMigrationSnapshotFixture() (*DeclarativeService, *snapshotRepository, domaincatalog.ReleaseTarget, func(int64)) {
	s, repo, _, target := newSnapshotTestService()
	templates := &migrationTemplates{}
	for n := int64(1); n <= 4; n++ {
		templates.values = append(templates.values, domaincatalog.DeploymentTemplate{ID: "template", PublishedVersion: n, ContentDigest: fmt.Sprintf("digest%d", n), DeploymentTemplateSpec: domaincatalog.DeploymentTemplateSpec{Enabled: true, Source: domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: []domainmanifest.File{{Path: "config.yaml", Content: fmt.Sprintf("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: payments\ndata:\n  version: '%d'\n", n)}}}, ParameterSchema: domaincatalog.ParameterSchema{Type: "object"}}})
	}
	templates.values[1].MigrationPolicy = &sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "相邻发布并核验"}
	templates.values[2].MigrationPolicy = &sohaapi.TemplateMigrationPolicy{Irreversible: true, Guide: "只允许前向修复"}
	apps := &snapshotApplications{service: domainapp.Service{ID: "api", ApplicationID: "payments", Key: "api", Version: 1, DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: "template", Version: 1, ManifestPackageID: "package", Parameters: map[string]any{}}}}
	s.base.applications, s.renderer = apps, manifestruntime.NewRenderer()
	s.SetDeploymentTemplateReader(templates)
	repo.base.item.ServiceID = apps.service.ID
	repo.base.item.Files = templates.values[0].Source.Files
	repo.base.revisions[0].Files = repo.base.item.Files
	repo.deployment = domainmanifest.Deployment{ID: "deployment", PackageID: repo.base.item.ID, BindingID: repo.binding.ID, Generation: 1, Status: domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhaseConverged, AppliedTemplate: &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 1, TemplateDigest: "digest1"}}}
	selectVersion := func(n int64) {
		repo.binding.TemplateSelection = &domainmanifest.TemplateSelection{TemplateID: "template", Version: n, Parameters: map[string]any{}, BaseFilesDigest: TemplateFilesDigest(repo.base.item.Files), AdoptionID: "adoption"}
	}
	return s, repo, target, selectVersion
}

func TestManifestMigrationGuardsActualApplyAndStalePlan(t *testing.T) {
	s, repo, target, selectVersion := newMigrationSnapshotFixture()
	selectVersion(4)
	if _, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "skip", domainmanifest.DeliveryArtifacts{}); !errors.Is(err, apperrors.ErrConflict) || len(repo.tasks) != 0 {
		t.Fatalf("skipped migration queued preflight %v", err)
	}
	selectVersion(2)
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "adjacent", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	if snapshot.Migration == nil || !snapshot.Migration.RequiresApproval || snapshot.Migration.FromVersion != 1 {
		t.Fatalf("migration not frozen %#v", snapshot.Migration)
	}
	approveSnapshotPreflight(repo, snapshot)
	repo.deployment.Status.AppliedTemplate.TemplateVersion = 2
	repo.deployment.Status.AppliedTemplate.TemplateDigest = "digest2"
	if _, _, err = s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot); !errors.Is(err, apperrors.ErrConflict) || repo.setCount != 0 {
		t.Fatalf("stale approval applied %v", err)
	}
	repo.deployment.Status.AppliedTemplate.TemplateVersion = 1
	repo.deployment.Status.AppliedTemplate.TemplateDigest = "digest1"
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	fixture := &rolloutTaskFixture{snapshotRepository: repo, plan: domaindelivery.DeliveryPlan{ID: snapshot.DeliveryPlanID, ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", Status: "confirmed", RequiresApproval: true, ManifestSnapshots: []domainmanifest.DeliverySnapshot{snapshot}}}
	s.repository, s.delivery = fixture, fixture
	identities := &rootExecutionPrincipal{principal: testPrincipal()}
	s.executionPrincipals = identities
	requireSnapshotTestNoError(t, s.ValidateRootCRExecutionTask(t.Context(), task))
	changed := task
	payload, err := decodeTaskPayload(changed.Payload)
	requireSnapshotTestNoError(t, err)
	payload.Documents = append([]domainmanifest.RenderedDocument(nil), payload.Documents...)
	payload.Documents[0].Content = "unapproved content"
	changed.Payload = structMap(payload)
	if err = s.ValidateRootCRExecutionTask(t.Context(), changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed payload passed claim %v", err)
	}
	identities.err = apperrors.ErrUnauthorized
	if err = s.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("revoked identity passed claim %v", err)
	}
	identities.err = nil
	fixture.planErr = apperrors.ErrAccessDenied
	if err = s.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked Plan access passed claim %v", err)
	}
	fixture.planErr = nil
	// Selecting the desired v2 snapshot must not promote the last successful v1 identity.
	if repo.deployment.Status.AppliedTemplate != nil && repo.deployment.Status.AppliedTemplate.TemplateVersion != 1 {
		t.Fatal("desired selection fabricated successful migration")
	}
	verifyMigrationRollbackAndLegacyGuards(t, s, repo, target, selectVersion)
}

func verifyMigrationRollbackAndLegacyGuards(t *testing.T, s *DeclarativeService, repo *snapshotRepository, target domaincatalog.ReleaseTarget, selectVersion func(int64)) {
	t.Helper()
	var err error
	repo.deployment.Status = domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhaseConverged, AppliedTemplate: &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 4, TemplateDigest: "digest4"}}
	selectVersion(2)
	if _, err = s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "rollback", domainmanifest.DeliveryArtifacts{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("irreversible rollback accepted %v", err)
	}
	repo.deployment.Status.AppliedTemplate = nil
	if _, err = s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "unknown", domainmanifest.DeliveryArtifacts{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unknown baseline accepted %v", err)
	}
	if err = s.requireLegacyMigration(t.Context(), testPrincipal(), repo.base.item.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy apply bypassed application Plan %v", err)
	}
	before := repo.setCount
	if err = s.promoteRevision(t.Context(), repo.base.item, 1, "system:manifest-sync"); !errors.Is(err, apperrors.ErrConflict) || repo.setCount != before {
		t.Fatalf("source auto-promotion changed desired outside Plan %v", err)
	}
	if err = s.PromoteRevision(t.Context(), testPrincipal(), repo.base.item, 1); !errors.Is(err, apperrors.ErrConflict) || repo.setCount != before {
		t.Fatalf("manual promotion bypassed migration Plan %v", err)
	}
	repo.deployment.Spec.DeliverySnapshot = nil
	s.templateMigrations = sourceMigrationAdmission(true)
	if _, err = s.Rollback(t.Context(), testPrincipal(), repo.deployment.ID, domainmanifest.RollbackInput{ExpectedGeneration: repo.deployment.Generation, TargetRevision: 1}); !errors.Is(err, apperrors.ErrConflict) || repo.setCount != before {
		t.Fatalf("legacy rollback changed desired before migration admission %v", err)
	}
}

func TestUntemplatedQueuedPlanRevalidatesIdentityAndLaterTemplateBinding(t *testing.T) {
	s, repo, _, target := newSnapshotTestService()
	repo.base.item.ServiceID = "api"
	repo.base.revisions[0].Files = []domainmanifest.File{{Path: "config.yaml", Content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\n  namespace: payments\n"}}
	apps := &snapshotApplications{service: domainapp.Service{ID: "api", ApplicationID: "payments", Version: 1}}
	s.base.applications = apps
	snapshot, err := s.CreateDeliverySnapshot(t.Context(), testPrincipal(), "payments", "payments-dev", target, 1, "untemplated", domainmanifest.DeliveryArtifacts{})
	requireSnapshotTestNoError(t, err)
	if snapshot.TemplateInputs != nil || snapshot.Migration != nil {
		t.Fatal("fixture must freeze an untemplated Plan")
	}
	approveSnapshotPreflight(repo, snapshot)
	_, task, err := s.ApplyDeliverySnapshot(t.Context(), testPrincipal(), snapshot)
	requireSnapshotTestNoError(t, err)
	fixture := &rolloutTaskFixture{snapshotRepository: repo, plan: domaindelivery.DeliveryPlan{ID: snapshot.DeliveryPlanID, ApplicationID: "payments", ApplicationEnvironmentID: "payments-dev", Status: "confirmed", ManifestSnapshots: []domainmanifest.DeliverySnapshot{snapshot}}}
	identities := &rootExecutionPrincipal{principal: testPrincipal()}
	s.repository, s.delivery, s.executionPrincipals = fixture, fixture, identities
	requireSnapshotTestNoError(t, s.ValidateRootCRExecutionTask(t.Context(), task))
	identities.err = apperrors.ErrUnauthorized
	if err = s.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("untemplated Plan ignored revoked identity: %v", err)
	}
	identities.err = nil
	fixture.planErr = apperrors.ErrAccessDenied
	if err = s.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("untemplated Plan ignored revoked Plan access: %v", err)
	}
	fixture.planErr = nil
	s.renderer = manifestruntime.NewRenderer()
	apps.service.DeploymentTemplate = &domaincatalog.DeploymentTemplateBinding{TemplateID: "template", Version: 1, ManifestPackageID: repo.base.item.ID}
	s.SetDeploymentTemplateReader(&migrationTemplates{values: []domaincatalog.DeploymentTemplate{{ID: "template", PublishedVersion: 1, ContentDigest: "digest1", DeploymentTemplateSpec: domaincatalog.DeploymentTemplateSpec{Enabled: true, MigrationPolicy: &sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "先核对实际基线"}, Source: domaincatalog.DeploymentTemplateSource{Renderer: "raw_yaml", Files: repo.base.revisions[0].Files}, ParameterSchema: domaincatalog.ParameterSchema{Type: "object"}}}}})
	if err = s.ValidateRootCRExecutionTask(t.Context(), task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("queued untemplated Plan bypassed later migration binding: %v", err)
	}
}
