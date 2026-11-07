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
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoapp "github.com/opensoha/soha/internal/repository/application"
	repocatalog "github.com/opensoha/soha/internal/repository/catalog"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repomanifest "github.com/opensoha/soha/internal/repository/manifest"
	"github.com/opensoha/soha/internal/repository/servicereferencelock"
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
	t.Run("application-lock-precedes-candidate-and-workflow", func(t *testing.T) {
		verifyAdoptionBeforeWorkflowLock(t, ctx, store.DB())
	})
}

func verifyAdoptionBeforeWorkflowLock(t *testing.T, ctx context.Context, db *gorm.DB) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	f := newAdoptionFixture(t, ctx, db)
	binding := f.bindings[0]
	appID := f.candidate.Candidate.Target.ApplicationID
	t.Cleanup(func() {
		for _, row := range []struct{ table, id string }{
			{"delivery_plans", f.plan.ID}, {"template_update_previews", f.candidate.PreviewID},
			{"delivery_batches", f.run.DeliveryBatchID}, {"workflow_runs", f.run.ID},
			{"manifest_packages", binding.PackageID}, {"applications", appID}, {"clusters", binding.ClusterID},
		} {
			if err := db.Exec("DELETE FROM "+row.table+" WHERE id=?", row.id).Error; err != nil {
				t.Error(err)
			}
		}
	})
	plan := f.plan
	plan.Status = domaindelivery.DeliveryPlanStatusWaitingApproval
	plan.Impact["approval"] = []any{map[string]any{"status": "requested"}}
	waiting, err := f.delivery.UpdateDeliveryPlan(domainworkflow.WithNodeExecution(ctx, f.run, f.run.NodeRuns[0]), plan)
	requireOK(t, err)
	holder := db.WithContext(ctx).Begin()
	requireOK(t, holder.Error)
	defer func() { _ = holder.Rollback().Error }()
	requireOK(t, servicereferencelock.Applications(holder, appID))
	var pid int
	requireOK(t, holder.Raw(`SELECT pg_backend_pid()`).Row().Scan(&pid))
	done := make(chan error, 1)
	go func() {
		worker := domainworkflow.WithNodeExecution(ctx, f.run, f.run.NodeRuns[1])
		done <- f.repo.ApplyCandidate(worker, f.candidate.Candidate.ID, f.plan.ID, func(txctx context.Context, _ domainupdate.FrozenCandidate) error {
			return f.manifest.SetTemplateSelection(txctx, binding.ID, binding.Version, domainmanifest.TemplateSelection{TemplateID: "ordered", Version: 2})
		})
	}()
	waitForAdoptionLock(t, ctx, db, pid, done)
	// The waiting adoption must still leave both rows available to this transaction.
	requireOK(t, holder.Exec(`SELECT id FROM template_update_candidates WHERE id=? FOR UPDATE NOWAIT`, f.candidate.Candidate.ID).Error)
	requireOK(t, holder.Exec(`SELECT id FROM workflow_runs WHERE id=? FOR UPDATE NOWAIT`, f.run.ID).Error)
	waiting.Status = domaindelivery.DeliveryPlanStatusDraft
	history, ok := waiting.Impact["approval"].([]any)
	if !ok {
		t.Fatal("waiting Plan approval history is not an array")
	}
	waiting.Impact["approval"] = append(history, map[string]any{"status": "approved"})
	_, err = repodelivery.New(holder).UpdateDeliveryPlan(ctx, waiting)
	requireOK(t, err)
	requireOK(t, holder.Commit().Error)
	select {
	case err := <-done:
		requireOK(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stored, err := f.manifest.GetBinding(ctx, binding.ID)
	requireOK(t, err)
	if stored.TemplateSelection == nil || stored.TemplateSelection.TemplateID != "ordered" {
		t.Fatal("adoption did not complete after concurrent Plan approval")
	}
}

func waitForAdoptionLock(t *testing.T, ctx context.Context, db *gorm.DB, pid int, done <-chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		requireOK(t, db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)))`, pid).Row().Scan(&waiting))
		if waiting {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("adoption completed before reaching the held lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
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
	services  []domainapp.Service
}

func newAdoptionFixture(t *testing.T, ctx context.Context, db *gorm.DB) adoptionFixture {
	t.Helper()
	f := adoptionFixture{repo: New(db), manifest: repomanifest.New(db), delivery: repodelivery.New(db), workflow: repoworkflow.New(db)}
	appID, clusterID := uuid.NewString(), uuid.NewString()
	requireOK(t, db.Exec(`INSERT INTO applications(id,name,app_key,app_group,language) VALUES(?,'Template acceptance',?,'test','configuration')`, appID, appID).Error)
	requireOK(t, db.Exec(`INSERT INTO clusters(id,name) VALUES(?,'Template acceptance')`, clusterID).Error)
	for _, key := range []string{"api", "worker"} {
		service, err := repoapp.New(db).CreateService(ctx, appID, domainapp.ServiceInput{Key: key, Name: key, Enabled: true,
			Containers: []domainapp.ServiceContainerInput{{Name: key, ImageRepository: "example.invalid/" + key}}})
		requireOK(t, err)
		f.services = append(f.services, service)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	pkg, err := f.manifest.Create(ctx, domainmanifest.Package{ID: uuid.NewString(), Name: "shared", ApplicationID: appID, Renderer: "raw_yaml", Status: "published", CurrentRevision: 1, Files: []domainmanifest.File{}, Bindings: []domainmanifest.Binding{}, CreatedAt: now, UpdatedAt: now})
	requireOK(t, err)
	for _, key := range []string{"test", "production"} {
		envID := uuid.NewString()
		requireOK(t, db.Exec(`INSERT INTO application_environments(id,application_id,environment_id) VALUES(?,?,?)`, envID, appID, key).Error)
		binding, err := f.manifest.CreateBinding(ctx, domainmanifest.EnvironmentBinding{ID: uuid.NewString(), PackageID: pkg.ID, ApplicationEnvironmentID: envID, EnvironmentKey: key, ClusterID: clusterID, Namespace: key, Overlay: map[string]string{}, TemplateParameters: map[string]any{"replicas": float64(7)}, Enabled: true, Version: 1, DriftPolicy: "report", DeletionPolicy: "orphan", CreatedAt: now, UpdatedAt: now})
		requireOK(t, err)
		requireOK(t, db.Exec(`INSERT INTO release_targets(id,application_environment_id,cluster_id,namespace,target_kind,executor_kind,config_ref,workload_kind,workload_name) VALUES(?,?,?,?,'k8s_workload','manifest_ssa',?,'ManifestPackage',?)`, uuid.NewString(), envID, clusterID, key, binding.ID, pkg.ID).Error)
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
		if err := verifyAdoptionTransactionalReads(txctx, db, f); err != nil {
			return err
		}
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

func verifyAdoptionTransactionalReads(txctx context.Context, db *gorm.DB, f adoptionFixture) error {
	// Candidate revalidation reads real environment/target relations within the
	// same transaction, where an open outer result set cannot borrow another connection.
	environments, err := repocatalog.New(db).ListApplicationEnvironments(txctx)
	if err != nil {
		return err
	}
	matched := 0
	for _, environment := range environments {
		for _, binding := range f.bindings {
			if environment.ID == binding.ApplicationEnvironmentID {
				matched++
				if len(environment.Targets) != 1 || environment.Targets[0].ConfigRef != binding.ID {
					return errors.New("transactional environment read lost its release target")
				}
			}
		}
	}
	if matched != len(f.bindings) {
		return errors.New("transactional environment read lost a binding")
	}
	services, err := repoapp.New(db).ListServices(txctx, f.candidate.Candidate.Target.ApplicationID)
	if err != nil {
		return err
	}
	if len(services) != len(f.services) {
		return errors.New("transactional service read lost a service")
	}
	for index, service := range services {
		if service.ID != f.services[index].ID || len(service.Containers) != 1 || service.Containers[0].Name != service.Key {
			return errors.New("transactional service read lost ordering or containers")
		}
	}
	return nil
}
