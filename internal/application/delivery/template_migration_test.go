package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type migrationHelmRepository struct {
	*batchRuntimeRepository
	history      []domaindelivery.ExecutionTask
	writeHistory bool
}

func (r *migrationHelmRepository) HasTargetWriteHistory(context.Context, string, string, []string, string, string) (bool, error) {
	return r.writeHistory, nil
}

func TestHelmFirstReleaseDoesNotInitializeExistingUnknownTarget(t *testing.T) {
	s, repo, _, _, _, _, _ := batchRuntimeCheck(t)
	history := &migrationHelmRepository{batchRuntimeRepository: repo}
	s.repository = history
	s.SetTemplateUpdates(&migrationReleaseOwner{versions: []domaincatalog.MigrationVersion{{Number: 1, Digest: "one"}, {Number: 2, Digest: "two", Policy: &sohaapi.TemplateMigrationPolicy{Sequential: true, Irreversible: true, Guide: "核对数据初始化前提"}}, {Number: 3, Digest: "three"}}})
	target := domaincatalog.ReleaseTarget{ID: "target", ClusterID: "cluster", Namespace: "team", Helm: &sohaapi.HelmDeliveryConfiguration{ReleaseName: "db"}}
	snapshot := sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "first", ApplicationID: "app", ApplicationEnvironmentID: "env", TargetID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace, ReleaseName: "db", Operation: sohaapi.HelmDeliverySnapshotOperationInstall, TemplateInputs: &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 3, TemplateDigest: "three"}}
	assessment, err := s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), snapshot, target)
	if err != nil || assessment == nil || !assessment.Initialization || !assessment.RequiresApproval || assessment.Blocked || len(assessment.Steps) != 1 {
		t.Fatalf("new Helm initialization %+v %v", assessment, err)
	}
	snapshot.ExpectedRevision = 1
	if _, err := s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), snapshot, target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("existing native deployment initialized %v", err)
	}
	snapshot.ExpectedRevision = 0
	history.writeHistory = true
	if _, err := s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), snapshot, target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("failed historical write initialized %v", err)
	}
}

func (r *migrationHelmRepository) ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	return r.history, nil
}

type migrationReleaseOwner struct {
	adoptionOwner
	versions []domaincatalog.MigrationVersion
}

func (o *migrationReleaseOwner) AssessRelease(_ context.Context, _ domainidentity.Principal, _ sohaapi.TemplateUpdateKind, _ string, from, to int64) (*sohaapi.TemplateMigrationAssessment, error) {
	return domaincatalog.AssessMigration(o.versions, from, to), nil
}
func (o *migrationReleaseOwner) ReleaseReference(_ context.Context, _ domainidentity.Principal, id string, n int64) (*sohaapi.TemplateVersionReference, error) {
	for _, v := range o.versions {
		if v.Number == n {
			return &sohaapi.TemplateVersionReference{TemplateID: id, TemplateVersion: n, TemplateDigest: v.Digest}, nil
		}
	}
	return nil, apperrors.ErrNotFound
}

func TestHelmMigrationChecksNativeRevisionAndDoesNotFallBackPastInvalidReceipt(t *testing.T) {
	s, repo, _, _, _, _, _ := batchRuntimeCheck(t)
	history := &migrationHelmRepository{batchRuntimeRepository: repo}
	s.repository = history
	s.SetTemplateUpdates(&migrationReleaseOwner{versions: []domaincatalog.MigrationVersion{{Number: 1, Digest: "digest1"}, {Number: 2, Digest: "digest2", Policy: &sohaapi.TemplateMigrationPolicy{Sequential: true, Irreversible: true, Guide: "备份后相邻发布"}}, {Number: 3, Digest: "digest3"}}})
	target := domaincatalog.ReleaseTarget{ID: "target", ClusterID: "cluster", Namespace: "test", Helm: &sohaapi.HelmDeliveryConfiguration{ReleaseName: "app"}}
	snapshot := sohaapi.HelmDeliverySnapshot{DeliveryPlanID: "plan", TargetID: target.ID, ApplicationID: "app", ApplicationEnvironmentID: "env", ClusterID: target.ClusterID, Namespace: target.Namespace, ReleaseName: target.Helm.ReleaseName, ExpectedRevision: 1, RenderedDigest: "sha256:" + strings.Repeat("a", 64), TemplateInputs: &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 1, TemplateDigest: "digest1"}}
	payload := sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Apply, Snapshot: snapshot}
	completed := domaindelivery.ExecutionTask{TaskKind: "helm_apply", Status: "completed", Payload: map[string]any{"helm": payload}, Result: map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "deployed", Revision: 2, RenderedDigest: snapshot.RenderedDigest}}}
	history.history = []domaindelivery.ExecutionTask{completed}
	next := snapshot
	next.ExpectedRevision = 2
	next.TemplateInputs = &sohaapi.TemplateVersionReference{TemplateID: "template", TemplateVersion: 2, TemplateDigest: "digest2"}
	assessment, err := s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), next, target)
	if err != nil || assessment == nil || !assessment.RequiresApproval || assessment.FromVersion != 1 {
		t.Fatalf("legal adjacent release %#v %v", assessment, err)
	}
	next.ExpectedRevision = 3
	if _, err = s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), next, target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("native revision mismatch accepted %v", err)
	}
	next.ExpectedRevision = 2
	next.TemplateInputs.TemplateVersion = 3
	if _, err = s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), next, target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("jumped sequential release %v", err)
	}
	invalid := completed
	invalid.Result = map[string]any{"helm": sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "deployed", Revision: 9, RenderedDigest: snapshot.RenderedDigest}}
	history.history = []domaindelivery.ExecutionTask{invalid, completed}
	if ref, _, err := s.helmMigrationBaseline(t.Context(), domainidentity.Principal{}, "app", "env", target); err != nil || ref != nil {
		t.Fatalf("invalid newest receipt fell back to older baseline %#v %v", ref, err)
	}
	history.history = []domaindelivery.ExecutionTask{completed}
	next.TemplateInputs = nil
	next.RollbackRevision = 1
	if _, err = s.assessHelmMigration(t.Context(), deliveryActionPrincipal(), next, target); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unidentified historical rollback bypassed migration %v", err)
	}
}

func TestMigrationApprovalRequiresExplicitCommentBeforeAdoption(t *testing.T) {
	s, repo, builder, manifest, _, run, batch := batchRuntimeCheck(t)
	migration := &sohaapi.TemplateMigrationAssessment{FromVersion: 1, ToVersion: 2, RequiresApproval: true, Steps: []sohaapi.TemplateMigrationStep{{Version: 2, ContentDigest: "v2", Policy: sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "备份后逐版迁移"}}}}
	owner := &adoptionOwner{frozen: domainupdate.FrozenCandidate{PreviewID: "preview", TemplateDigest: "source", Candidate: sohaapi.TemplateUpdateCandidate{ID: "candidate", Digest: "frozen", Eligible: true, RequiresApproval: true, Cumulative: sohaapi.TemplateUpdateDiff{Migration: migration}, Target: sohaapi.TemplateUpdateTarget{ID: "binding", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", ReleaseTargetID: "target", TemplateID: "template", FromVersion: 1}, ToVersion: 2}}}
	s.SetTemplateUpdates(owner)
	target := domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", ReleaseTargetID: "target", Action: "config_update", TemplateCandidateID: "candidate"}
	snapshot, err := s.freezeTemplateAdoption(domainupdate.WithPreviewBatch(context.Background(), "preview"), deliveryActionPrincipal(), target)
	if err != nil {
		t.Fatal(err)
	}
	batch.Targets = []domainworkflow.DeliveryTargetSnapshot{snapshot}
	batchRuntimeStep(t, s, &run, batch, 1, "waiting_approval")
	if !planHasMigration(repo.plan) {
		t.Fatal("migration declaration missing from ordinary Plan")
	}
	for _, comment := range []string{"", "  \n "} {
		if _, err = s.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve", Comment: comment}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("blank migration approval accepted: %v", err)
		}
	}
	if _, err = s.DecideDeliveryPlanApproval(context.Background(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve", Comment: "已核实备份与 v2 校验方案"}); err != nil {
		t.Fatal(err)
	}
	batchRuntimeStep(t, s, &run, batch, 1, "completed")
	batchRuntimeStep(t, s, &run, batch, 2, "completed")
	if owner.applied != 1 || builder.created != 0 || manifest.created != 0 || len(repo.tasks) != 0 {
		t.Fatal("adoption executed a migration or deployment")
	}
}

func TestMigrationSnapshotsForceExistingPlanApproval(t *testing.T) {
	for _, helm := range []bool{false, true} {
		input := domaindelivery.DeliveryPlanInput{Impact: map[string]any{}}
		migration := &sohaapi.TemplateMigrationAssessment{FromVersion: 2, ToVersion: 3, RequiresApproval: true}
		if helm {
			input.HelmSnapshots = []sohaapi.HelmDeliverySnapshot{{Migration: migration}}
		} else {
			input.ManifestSnapshots = []domainmanifest.DeliverySnapshot{{Migration: migration}}
		}
		freezePlanMigration(&input)
		if !input.RequiresApproval || input.RiskLevel != "high" || input.Impact["templateMigrations"] == nil {
			t.Fatal("migration did not force existing approval")
		}
	}
}

func TestOrdinaryApprovalOutsideMigrationBoundaryKeepsOptionalComment(t *testing.T) {
	s, repo, _, _, _, run, batch := batchRuntimeCheck(t)
	migration := &sohaapi.TemplateMigrationAssessment{FromVersion: 3, ToVersion: 4, Steps: []sohaapi.TemplateMigrationStep{}}
	owner := &adoptionOwner{frozen: domainupdate.FrozenCandidate{PreviewID: "preview", TemplateDigest: "source", Candidate: sohaapi.TemplateUpdateCandidate{ID: "candidate", Digest: "frozen", Eligible: true, RequiresApproval: true, Cumulative: sohaapi.TemplateUpdateDiff{Migration: migration}, Target: sohaapi.TemplateUpdateTarget{ID: "binding", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", ReleaseTargetID: "target", TemplateID: "template", FromVersion: 3}, ToVersion: 4}}}
	s.SetTemplateUpdates(owner)
	target := domainworkflow.DeliveryTargetInput{ID: "web", ApplicationID: "app", ServiceID: "svc", ApplicationEnvironmentID: "env", ReleaseTargetID: "target", Action: "config_update", TemplateCandidateID: "candidate"}
	snapshot, err := s.freezeTemplateAdoption(domainupdate.WithPreviewBatch(context.Background(), "preview"), deliveryActionPrincipal(), target)
	if err != nil {
		t.Fatal(err)
	}
	batch.Targets = []domainworkflow.DeliveryTargetSnapshot{snapshot}
	batchRuntimeStep(t, s, &run, batch, 1, "waiting_approval")
	if planHasMigration(repo.plan) {
		t.Fatal("historical declaration outside this interval forced a migration comment")
	}
	if _, err = s.DecideDeliveryPlanApproval(t.Context(), deliveryActionPrincipal(), repo.plan.ID, domaindelivery.DeliveryPlanApprovalInput{Action: "approve"}); err != nil {
		t.Fatalf("ordinary environment approval no longer accepts an optional comment: %v", err)
	}
}
