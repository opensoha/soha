package templateupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repomanifest "github.com/opensoha/soha/internal/repository/manifest"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTemplateAdoptionWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_MANIFEST_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_MANIFEST_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	requireOK(t, err)
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 4, MaxIdleConns: 2}, zap.NewNop())
	requireOK(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	requireOK(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")))
	for name, check := range map[string]func(*testing.T, context.Context, *gorm.DB){"atomic_receipt_and_environment_isolation": verifyAtomicAdoption, "stale_and_canceled_worker": verifyAdoptionFencing, "policy_cas_and_private_identity": verifyPolicyPersistence} {
		t.Run(name, func(t *testing.T) {
			tx := store.DB().Begin()
			requireOK(t, tx.Error)
			defer func() { requireOK(t, tx.Rollback().Error) }()
			check(t, ctx, tx)
		})
	}
}

type adoptionFixture struct {
	repo      *Repository
	manifest  *repomanifest.Repository
	delivery  *repodelivery.Repository
	workflow  *repoworkflow.Repository
	candidate domainupdate.FrozenCandidate
	bindings  []domainmanifest.EnvironmentBinding
	run       domainworkflow.Run
	plan      domaindelivery.DeliveryPlan
}

func newAdoptionFixture(t *testing.T, ctx context.Context, db *gorm.DB) adoptionFixture {
	t.Helper()
	f := adoptionFixture{repo: New(db), manifest: repomanifest.New(db), delivery: repodelivery.New(db), workflow: repoworkflow.New(db)}
	appID, clusterID := uuid.NewString(), uuid.NewString()
	requireOK(t, db.Exec(`INSERT INTO applications(id,name,app_key,app_group,language) VALUES(?,'Template acceptance',?,'test','configuration')`, appID, appID).Error)
	requireOK(t, db.Exec(`INSERT INTO clusters(id,name) VALUES(?,'Template acceptance')`, clusterID).Error)
	now := time.Now().UTC().Truncate(time.Microsecond)
	pkg, err := f.manifest.Create(ctx, domainmanifest.Package{ID: uuid.NewString(), Name: "shared", ApplicationID: appID, Renderer: "raw_yaml", Status: "published", CurrentRevision: 1, Files: []domainmanifest.File{}, Bindings: []domainmanifest.Binding{}, CreatedAt: now, UpdatedAt: now})
	requireOK(t, err)
	for _, key := range []string{"test", "production"} {
		envID := uuid.NewString()
		requireOK(t, db.Exec(`INSERT INTO application_environments(id,application_id,environment_id) VALUES(?,?,?)`, envID, appID, key).Error)
		binding, err := f.manifest.CreateBinding(ctx, domainmanifest.EnvironmentBinding{ID: uuid.NewString(), PackageID: pkg.ID, ApplicationEnvironmentID: envID, EnvironmentKey: key, ClusterID: clusterID, Namespace: key, Overlay: map[string]string{}, TemplateParameters: map[string]any{"replicas": float64(7)}, Enabled: true, Version: 1, DriftPolicy: "report", DeletionPolicy: "orphan", CreatedAt: now, UpdatedAt: now})
		requireOK(t, err)
		f.bindings = append(f.bindings, binding)
	}
	candidateID, previewID := uuid.NewString(), uuid.NewString()
	f.candidate = domainupdate.FrozenCandidate{PreviewID: previewID, Candidate: sohaapi.TemplateUpdateCandidate{ID: candidateID, Target: sohaapi.TemplateUpdateTarget{ID: f.bindings[0].ID, ApplicationID: appID, ApplicationEnvironmentID: f.bindings[0].ApplicationEnvironmentID}, Eligible: true}, ExpiresAt: now.Add(time.Hour)}
	requireOK(t, f.repo.SavePreview(ctx, sohaapi.TemplateUpdatePreview{ID: previewID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Candidates: []sohaapi.TemplateUpdateCandidate{f.candidate.Candidate}}, []domainupdate.FrozenCandidate{f.candidate}))
	target := domainworkflow.DeliveryTargetInput{ID: "adopt", ApplicationID: appID, ApplicationEnvironmentID: f.bindings[0].ApplicationEnvironmentID, Action: "config_update", TemplateCandidateID: candidateID}
	batch := domainworkflow.DeliveryBatch{ID: uuid.NewString(), RootRunID: "workflow:" + uuid.NewString(), Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "adopt", Targets: []domainworkflow.DeliveryTargetInput{target}}, Targets: []domainworkflow.DeliveryTargetSnapshot{{Target: target}}, CreatedBy: "test", CreatedAt: now, UpdatedAt: now}
	run := domainworkflow.Run{ID: batch.RootRunID, Scope: domainworkflow.ScopeDeliveryBatch, DeliveryBatchID: batch.ID, WorkflowName: "adopt", Status: "queued", Steps: []domainworkflow.Step{}, NodeRuns: []domainworkflow.NodeRun{{NodeID: "adopt:plan", TargetID: "adopt", Stage: "plan", Status: "running"}, {NodeID: "adopt:deploy", TargetID: "adopt", Stage: "deploy", Status: "running"}}, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}
	_, _, err = f.workflow.CreateDeliveryBatch(ctx, batch, run, uuid.NewString(), "sha256:"+strings.Repeat("a", 64))
	requireOK(t, err)
	f.run, err = f.workflow.ClaimManagedRun(ctx, "template-test", time.Minute)
	requireOK(t, err)
	planctx := domainworkflow.WithNodeExecution(ctx, f.run, f.run.NodeRuns[0])
	f.plan, err = f.delivery.CreateDeliveryPlan(planctx, domaindelivery.DeliveryPlanInput{Source: domainworkflow.ScopeDeliveryBatch, ApplicationID: appID, ApplicationEnvironmentID: target.ApplicationEnvironmentID, Action: domaindelivery.ApplicationDeliveryActionDeploy, TemplateCandidateID: candidateID, Impact: map[string]any{"configurationOnly": true}}, "test")
	requireOK(t, err)
	if f.plan.TemplateCandidateID != candidateID {
		t.Fatal("Plan lost template candidate identity")
	}
	return f
}

func verifyAtomicAdoption(t *testing.T, ctx context.Context, db *gorm.DB) {
	f := newAdoptionFixture(t, ctx, db)
	worker := domainworkflow.WithNodeExecution(ctx, f.run, f.run.NodeRuns[1])
	f.plan.Status = domaindelivery.DeliveryPlanStatusConfirming
	var err error
	f.plan, err = f.delivery.UpdateDeliveryPlan(worker, f.plan)
	requireOK(t, err)
	selection := domainmanifest.TemplateSelection{TemplateID: "template", Version: 4, BaseFilesDigest: "baseline", AdoptionID: f.plan.ID, Parameters: map[string]any{"replicas": float64(1)}}
	apply := func(txctx context.Context, _ domainupdate.FrozenCandidate) error {
		return f.manifest.SetTemplateSelection(txctx, f.bindings[0].ID, 1, selection)
	}
	failure := errors.New("receipt failure")
	err = f.repo.ApplyCandidate(worker, f.candidate.Candidate.ID, f.plan.ID, func(txctx context.Context, c domainupdate.FrozenCandidate) error {
		if err := apply(txctx, c); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("rollback failure: %v", err)
	}
	binding, err := f.manifest.GetBinding(ctx, f.bindings[0].ID)
	requireOK(t, err)
	if binding.TemplateSelection != nil || binding.Version != 1 {
		t.Fatal("failed receipt leaked configuration")
	}
	candidate, err := f.repo.GetCandidate(ctx, f.candidate.Candidate.ID)
	requireOK(t, err)
	if candidate.AppliedPlanID != "" {
		t.Fatal("failed receipt marked adoption complete")
	}
	requireOK(t, f.repo.ApplyCandidate(worker, candidate.Candidate.ID, f.plan.ID, func(txctx context.Context, c domainupdate.FrozenCandidate) error {
		if err := apply(txctx, c); err != nil {
			return err
		}
		plan := f.plan
		plan.Status = domaindelivery.DeliveryPlanStatusConfirmed
		_, err := f.delivery.UpdateDeliveryPlan(txctx, plan)
		return err
	}))
	for i, b := range f.bindings {
		loaded, err := f.manifest.GetBinding(ctx, b.ID)
		requireOK(t, err)
		if i == 0 {
			if loaded.TemplateSelection == nil || loaded.TemplateSelection.Version != 4 || loaded.Version != 2 {
				t.Fatal("selected environment was not adopted")
			}
		} else if loaded.TemplateSelection != nil || loaded.Version != 1 {
			t.Fatal("fixed production environment changed")
		}
		if loaded.TemplateParameters["replicas"] != float64(7) {
			t.Fatal("environment override changed")
		}
	}
	plan, err := f.delivery.GetDeliveryPlan(ctx, f.plan.ID)
	requireOK(t, err)
	if plan.Status != "confirmed" {
		t.Fatal("atomic Plan receipt missing")
	}
	requireOK(t, f.repo.ApplyCandidate(worker, candidate.Candidate.ID, f.plan.ID, func(context.Context, domainupdate.FrozenCandidate) error {
		t.Fatal("idempotent replay applied twice")
		return nil
	}))
	if err := f.repo.ApplyCandidate(worker, candidate.Candidate.ID, "another-plan", apply); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("candidate reused by another Plan: %v", err)
	}
}

func verifyAdoptionFencing(t *testing.T, ctx context.Context, db *gorm.DB) {
	f := newAdoptionFixture(t, ctx, db)
	apply := func(context.Context, domainupdate.FrozenCandidate) error {
		t.Fatal("fenced worker applied configuration")
		return nil
	}
	if err := f.repo.ApplyCandidate(ctx, f.candidate.Candidate.ID, f.plan.ID, apply); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unscoped worker: %v", err)
	}
	worker := domainworkflow.WithNodeExecution(ctx, f.run, f.run.NodeRuns[1])
	_, err := f.workflow.StopManagedRun(ctx, f.run.ID, "user", "stop")
	requireOK(t, err)
	if err := f.repo.ApplyCandidate(worker, f.candidate.Candidate.ID, f.plan.ID, apply); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stopped worker: %v", err)
	}
	selection := domainmanifest.TemplateSelection{TemplateID: "template", Version: 4}
	requireOK(t, f.manifest.SetTemplateSelection(ctx, f.bindings[0].ID, 1, selection))
	if err := f.manifest.SetTemplateSelection(ctx, f.bindings[0].ID, 1, selection); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale binding CAS passed")
	}
}

func verifyPolicyPersistence(t *testing.T, ctx context.Context, db *gorm.DB) {
	r := New(db)
	policy := domainupdate.Policy{ApplicationID: "app", SubjectID: "subject-reference", TokenID: "token-reference", Value: sohaapi.TemplateUpdatePolicy{ID: uuid.NewString(), Kind: "deployment", TemplateID: "template", TargetID: uuid.NewString(), Mode: "automatic", Paused: false}}
	value, err := r.SavePolicy(ctx, policy, 0)
	requireOK(t, err)
	if value.Value.Version != 1 || value.TokenID != policy.TokenID {
		t.Fatal("policy identity lost")
	}
	if _, err := r.SavePolicy(ctx, policy, 0); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("policy create CAS passed")
	}
	requireOK(t, r.WithPolicy(ctx, value.Value.ID, 1, func(_ context.Context, v *domainupdate.Policy) error {
		v.AttemptVersion = 4
		v.Value.LastBatchID = "batch"
		return nil
	}))
	items, err := r.ListPolicies(ctx, domainupdate.Filter{Kind: "deployment", TemplateID: "template", ApplicationID: "app"})
	requireOK(t, err)
	if len(items) != 1 || items[0].AttemptVersion != 4 || items[0].Value.LastBatchID != "batch" {
		t.Fatal("policy progress not durable")
	}
	// A concurrent worker can publish its receipt without changing the policy
	// configuration version. A stale settings save must retain that newer link.
	value.Value.Paused = true
	value, err = r.SavePolicy(ctx, value, 1)
	requireOK(t, err)
	due, err := r.ListDuePolicies(ctx, 16)
	requireOK(t, err)
	if len(due) != 0 || value.Value.Version != 2 || value.Value.LastBatchID != "batch" {
		t.Fatal("paused policy dequeued")
	}
	requireOK(t, r.WithPolicy(ctx, value.Value.ID, 1, func(context.Context, *domainupdate.Policy) error { t.Fatal("stale policy dequeued"); return nil }))
}
