package templateupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestBuildAndWorkflowAdoptionShareMigrationBoundaries(t *testing.T) {
	for _, kind := range []sohaapi.TemplateUpdateKind{"build", "workflow"} {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			for n := int64(1); n <= 3; n++ {
				var policy *sohaapi.TemplateMigrationPolicy
				if n == 2 {
					policy = &sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "先采用相邻版本并核对执行前提"}
				}
				f.catalog.build = append(f.catalog.build, domaincatalog.BuildTemplate{ID: "build", PublishedVersion: n, ContentDigest: fmt.Sprintf("b%d", n), Enabled: true, MigrationPolicy: policy})
				f.catalog.workflow = append(f.catalog.workflow, domaincatalog.WorkflowTemplate{ID: "workflow", PublishedVersion: n, ContentDigest: fmt.Sprintf("w%d", n), Enabled: true, MigrationPolicy: policy})
			}
			f.apps.items[0].BuildSources = []domainapp.BuildSource{{ID: "source", Type: domainapp.BuildSourceTypePlatformTemplate, Config: map[string]any{"buildTemplateId": "build", "buildTemplateVersion": 1}}}
			f.catalog.environments[0].WorkflowTemplateID = "workflow"
			f.catalog.environments[0].WorkflowTemplateVersion = 1
			page, err := f.s.ListTargets(t.Context(), f.p, domainupdate.Filter{Kind: kind, TemplateID: string(kind)})
			if err != nil || len(page.Items) == 0 {
				t.Fatalf("missing binding %v", err)
			}
			for _, n := range []int64{3, 2} {
				preview, err := f.s.CreatePreview(t.Context(), f.p, sohaapi.TemplateUpdatePreviewInput{Kind: kind, TemplateID: string(kind), ToVersion: n, Targets: []sohaapi.TemplateUpdateSelection{{TargetID: page.Items[0].ID}}})
				if err != nil {
					t.Fatal(err)
				}
				candidate := preview.Candidates[0]
				if n == 3 && (candidate.Eligible || !candidate.Cumulative.Migration.Blocked) {
					t.Fatal("template kind bypassed sequential declaration")
				}
				if n == 2 && (!candidate.Eligible || !candidate.RequiresApproval) {
					t.Fatalf("adjacent declaration was not approvable %#v", candidate)
				}
			}
		})
	}
}

func migrationFixture(t *testing.T) (*fixture, sohaapi.TemplateUpdateTarget) {
	t.Helper()
	f := newFixture(t)
	f.catalog.deployment[1].ParameterSchema.Required = nil
	f.catalog.deployment[1].MigrationPolicy = &sohaapi.TemplateMigrationPolicy{Sequential: true, Guide: "先备份，发布 v2 后校验数据"}
	f.catalog.deployment[2].MigrationPolicy = &sohaapi.TemplateMigrationPolicy{Irreversible: true, Guide: "仅允许前向修复"}
	f.batches.baseline = &sohaapi.TemplateVersionReference{TemplateID: "tpl", TemplateVersion: 1, TemplateDigest: "digest1"}
	return f, f.testTargets(t)[0]
}

func previewMigration(t *testing.T, f *fixture, target sohaapi.TemplateUpdateTarget, to int64) sohaapi.TemplateUpdatePreview {
	t.Helper()
	preview, err := f.s.CreatePreview(context.Background(), f.p, sohaapi.TemplateUpdatePreviewInput{Kind: "deployment", TemplateID: "tpl", ToVersion: to, Targets: []sohaapi.TemplateUpdateSelection{{TargetID: target.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestMigrationAdoptionUsesRuntimeBaselineAndImmutableBoundaries(t *testing.T) {
	f, target := migrationFixture(t)
	skipped := previewMigration(t, f, target, 4)
	if c := skipped.Candidates[0]; c.Eligible || c.Cumulative.Migration == nil || !c.Cumulative.Migration.Blocked || len(c.Cumulative.Migration.Steps) != 2 || !strings.Contains(c.Reason, "相邻") {
		t.Fatalf("unsafe skip %#v", c)
	}
	if _, err := f.s.CreateBatch(context.Background(), f.p, skipped.ID, sohaapi.TemplateUpdateBatchInput{PreviewDigest: skipped.Digest, CandidateIDs: []string{skipped.Candidates[0].ID}, WaveSize: 1, IdempotencyKey: "cannot-waive", AcknowledgeHistoryGaps: true}); err == nil {
		t.Fatal("history acknowledgement bypassed migration")
	}
	adjacent := previewMigration(t, f, target, 2)
	candidate := adjacent.Candidates[0]
	if !candidate.Eligible || !candidate.RequiresApproval || candidate.Cumulative.Migration.FromVersion != 1 || len(candidate.Cumulative.Migration.Steps) != 1 {
		t.Fatalf("adjacent migration %#v", candidate)
	}
	if _, err := f.s.Candidate(context.Background(), f.p, candidate.ID, true); err != nil {
		t.Fatal(err)
	}
	// A new runtime baseline invalidates the frozen approval input, even if the configuration did not change.
	f.batches.baseline.TemplateVersion = 2
	f.batches.baseline.TemplateDigest = "digest2"
	if _, err := f.s.Candidate(context.Background(), f.p, candidate.ID, true); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale runtime baseline: %v", err)
	}
	f.batches.baseline.TemplateVersion = 1
	f.batches.baseline.TemplateDigest = "digest1"
	if err := f.s.Apply(context.Background(), f.p, candidate.ID, "approved-plan", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	// Adopting v2 configuration is not proof that v2 has run successfully.
	afterAdoption := previewMigration(t, f, target, 4).Candidates[0]
	if afterAdoption.Target.FromVersion != 2 || afterAdoption.Cumulative.Migration.FromVersion != 1 || !afterAdoption.Cumulative.Migration.Blocked {
		t.Fatalf("adoption fabricated runtime evidence %#v", afterAdoption)
	}
	f.batches.baseline = nil
	unknown := previewMigration(t, f, target, 2).Candidates[0]
	if unknown.Eligible || !strings.Contains(unknown.Reason, "基线未知") {
		t.Fatalf("unknown runtime accepted: %#v", unknown)
	}
}

func TestAutomaticMigrationCannotSkipAndAdjacentStepRequiresApproval(t *testing.T) {
	for _, mode := range []sohaapi.TemplateUpdateMode{"automatic", "approval"} {
		t.Run(string(mode), func(t *testing.T) {
			f, target := migrationFixture(t)
			policy, err := f.s.SavePolicy(context.Background(), f.p, sohaapi.TemplateUpdatePolicyInput{Kind: "deployment", TemplateID: "tpl", TargetID: target.ID, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.s.ReconcilePolicies(context.Background()); err != nil {
				t.Fatal(err)
			}
			stored := f.repo.policies[policy.ID]
			if len(f.batches.inputs) != 0 || !strings.Contains(stored.Value.LastError, "相邻") || stored.Value.Paused || stored.AttemptVersion != 0 {
				t.Fatalf("unsafe auto skip or unhelpful retry %#v", stored)
			}
			f.catalog.deployment = f.catalog.deployment[:2]
			if err = f.s.ReconcilePolicies(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(f.batches.inputs) != 1 {
				t.Fatal("adjacent migration did not create an ordinary batch")
			}
			candidateID := f.batches.inputs[0].Definition.Targets[0].TemplateCandidateID
			frozen, err := f.s.Candidate(context.Background(), f.p, candidateID, true)
			if err != nil || !frozen.Candidate.RequiresApproval || !frozen.Candidate.Cumulative.Migration.RequiresApproval {
				t.Fatalf("migration bypassed approval %#v %v", frozen, err)
			}
		})
	}
}
