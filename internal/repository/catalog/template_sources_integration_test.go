package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	workflowrepo "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

func readySourceRun(t *testing.T, ctx context.Context, repo *Repository, source domaindocument.Source, mutations []domaindocument.Mutation) domaindocument.StoredSyncRun {
	t.Helper()
	run, created, err := repo.BeginTemplateSync(ctx, source.ID, "test-actor", domaindocument.SyncInput{ExpectedGeneration: source.Generation, IdempotencyKey: uuid.NewString()})
	requireTemplateNoError(t, err)
	if !created {
		t.Fatal("new sync was replayed")
	}
	expires := time.Now().UTC().Add(30 * time.Minute)
	run.Preview = &domaindocument.Preview{ID: run.ID, Valid: true, CandidateDigest: contractdelivery.Digest([]byte(run.ID)), ExpiresAt: &expires, Candidates: []domaindocument.Candidate{}, Diagnostics: []contractdelivery.Diagnostic{}}
	for _, mutation := range mutations {
		run.Preview.Candidates = append(run.Preview.Candidates, mutation.Candidate)
	}
	run.Status, run.ResolvedCommit, run.TreeDigest = "ready", strings.Repeat("a", 40), strings.Repeat("b", 40)
	run, err = repo.FinishTemplateSync(ctx, run)
	requireTemplateNoError(t, err)
	return run
}

func applySourceRun(t *testing.T, ctx context.Context, repo *Repository, run domaindocument.StoredSyncRun, mutations []domaindocument.Mutation) domaindocument.StoredSyncRun {
	t.Helper()
	input := domaindocument.SyncApplyInput{ExpectedGeneration: run.SourceGeneration, CandidateDigest: run.Preview.CandidateDigest, IdempotencyKey: "apply-" + run.ID}
	result, err := repo.ApplyTemplateSync(ctx, run, input, mutations)
	requireTemplateNoError(t, err)
	return result
}

func verifyTemplateSources(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	source, err := repo.SaveTemplateSource(ctx, domaindocument.Source{Name: "Git fixtures", RepositoryID: uuid.NewString(), Path: ".", RefType: "branch", RefValue: "main", Kinds: []contractsapi.DeliveryDocumentKind{"BuildTemplate", "Workflow"}, Enabled: true}, 0)
	requireTemplateNoError(t, err)
	mutations := []domaindocument.Mutation{testBuildDocument("source-" + uuid.NewString()), testBuildDocument("occupied-" + uuid.NewString())}
	run := readySourceRun(t, ctx, repo, source, mutations)
	occupied, err := repo.CreateBuildTemplate(ctx, mutations[1].Build)
	requireTemplateNoError(t, err)
	input := domaindocument.SyncApplyInput{ExpectedGeneration: source.Generation, CandidateDigest: run.Preview.CandidateDigest, IdempotencyKey: "apply-" + run.ID}
	if _, err := repo.ApplyTemplateSync(ctx, run, input, mutations); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expected atomic conflict: %v", err)
	}
	objects, err := repo.ListTemplateSourceObjects(ctx, source.ID)
	requireTemplateNoError(t, err)
	if len(objects) != 0 {
		t.Fatal("failed sync left associations")
	}
	if _, err := repo.FindDocumentKey(ctx, "BuildTemplate", mutations[0].Build.Key); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("failed sync left a draft: %v", err)
	}
	requireTemplateNoError(t, repo.db.Exec(`DELETE FROM build_templates WHERE id = ?`, occupied.ID).Error)
	results, errs := make(chan domaindocument.StoredSyncRun, 2), make(chan error, 2)
	for range 2 {
		go func() {
			result, err := repo.ApplyTemplateSync(ctx, run, input, mutations)
			results <- result
			errs <- err
		}()
	}
	first, second := <-results, <-results
	requireTemplateNoError(t, <-errs)
	requireTemplateNoError(t, <-errs)
	if first.Status != "applied" || !reflect.DeepEqual(first.Result, second.Result) {
		t.Fatal("concurrent replay diverged")
	}
	verifySourceOwnershipAndProvenance(t, ctx, repo, source, run, mutations, first)
	verifySourceStaleRuns(t, ctx, repo)
	verifyWorkflowSourceOwnership(t, ctx, repo)
}

func verifySourceOwnershipAndProvenance(t *testing.T, ctx context.Context, repo *Repository, source domaindocument.Source, run domaindocument.StoredSyncRun, mutations []domaindocument.Mutation, applied domaindocument.StoredSyncRun) {
	t.Helper()
	object := applied.Result.Objects[0]
	mutation := mutations[0]
	mutation.Build.ExpectedRevision = &object.Revision
	mutation.Build.Name = "UI overwrite"
	if _, err := repo.UpdateBuildTemplate(ctx, object.ID, mutation.Build); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("Git draft accepted UI write: %v", err)
	}
	mutation.TargetID, mutation.ExpectedRevision, mutation.Action = object.ID, object.Revision, "update"
	preview := saveTestDocumentPreview(t, ctx, repo, "test-actor", []domaindocument.Mutation{mutation})
	if _, err := repo.ApplyDocumentImport(ctx, preview, uuid.NewString(), []domaindocument.Mutation{mutation}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("file import bypassed Git ownership: %v", err)
	}
	published, err := repo.PublishBuildTemplate(ctx, object.ID, object.Revision)
	requireTemplateNoError(t, err)
	info, err := repo.GetDocumentSource(ctx, "BuildTemplate", object.ID, published.PublishedVersion)
	requireTemplateNoError(t, err)
	if info.Provenance == nil || info.Provenance.SourceID != source.ID || info.Provenance.ResolvedCommit != run.ResolvedCommit || info.Provenance.Kind != "BuildTemplate" || info.Provenance.Version != 1 {
		t.Fatalf("missing immutable provenance: %+v", info.Provenance)
	}
	if err := repo.db.Exec(`UPDATE delivery_document_provenance SET provenance = '{}' WHERE object_id = ?`, object.ID).Error; err == nil {
		t.Fatal("provenance was mutable")
	}
	replayed := applySourceRun(t, ctx, repo, run, mutations)
	if !reflect.DeepEqual(replayed.Result, applied.Result) {
		t.Fatal("publication changed the original import receipt")
	}
	source, err = repo.GetTemplateSource(ctx, source.ID)
	requireTemplateNoError(t, err)
	objects, err := repo.ListTemplateSourceObjects(ctx, source.ID)
	requireTemplateNoError(t, err)
	removed := readySourceRun(t, ctx, repo, source, nil)
	for _, item := range objects {
		removed.Removed = append(removed.Removed, item.Association)
	}
	// Prepare a deletion candidate as the service does, before the terminal write.
	requireTemplateNoError(t, repo.db.Transaction(func(tx *gorm.DB) error { return writeTemplateSyncRun(tx, &removed) }))
	applySourceRun(t, ctx, repo, removed, nil)
	info, err = repo.GetDocumentSource(ctx, "BuildTemplate", object.ID, 1)
	requireTemplateNoError(t, err)
	if info.Association == nil || !info.Association.Removed || info.Provenance == nil {
		t.Fatal("source deletion erased history instead of marking removed")
	}
	source, err = repo.GetTemplateSource(ctx, source.ID)
	requireTemplateNoError(t, err)
	requireTemplateNoError(t, repo.RemoveTemplateSource(ctx, source.ID, source.Generation, "", "", "keep"))
	info, err = repo.GetDocumentSource(ctx, "BuildTemplate", object.ID, 1)
	requireTemplateNoError(t, err)
	if info.Association != nil || info.Provenance == nil {
		t.Fatal("detachment did not preserve provenance")
	}
	mutation.Build.ExpectedRevision = &published.Revision
	_, err = repo.UpdateBuildTemplate(ctx, object.ID, mutation.Build)
	requireTemplateNoError(t, err)
}

func verifySourceStaleRuns(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	source, err := repo.SaveTemplateSource(ctx, domaindocument.Source{Name: "Stale", RepositoryID: uuid.NewString(), Enabled: true}, 0)
	requireTemplateNoError(t, err)
	old := readySourceRun(t, ctx, repo, source, nil)
	latest := readySourceRun(t, ctx, repo, source, nil)
	input := domaindocument.SyncApplyInput{ExpectedGeneration: source.Generation, CandidateDigest: old.Preview.CandidateDigest, IdempotencyKey: uuid.NewString()}
	if _, err := repo.ApplyTemplateSync(ctx, old, input, nil); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("old preview overwrote newer sync: %v", err)
	}
	latest.Preview.ExpiresAt = new(time.Now().Add(-time.Minute))
	requireTemplateNoError(t, repo.db.Transaction(func(tx *gorm.DB) error { return writeTemplateSyncRun(tx, &latest) }))
	input.CandidateDigest = latest.Preview.CandidateDigest
	if _, err := repo.ApplyTemplateSync(ctx, latest, input, nil); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expired preview accepted: %v", err)
	}
	syncInput := domaindocument.SyncInput{ExpectedGeneration: source.Generation, IdempotencyKey: uuid.NewString(), ResolvedCommit: strings.Repeat("a", 40)}
	running, _, err := repo.BeginTemplateSync(ctx, source.ID, "test-actor", syncInput)
	requireTemplateNoError(t, err)
	replayed, created, err := repo.BeginTemplateSync(ctx, source.ID, "test-actor", syncInput)
	requireTemplateNoError(t, err)
	if created || replayed.ID != running.ID || replayed.RequestedCommit != syncInput.ResolvedCommit {
		t.Fatal("fixed event commit or sync identity changed on replay")
	}
	syncInput.ResolvedCommit = strings.Repeat("b", 40)
	if _, _, err := repo.BeginTemplateSync(ctx, source.ID, "test-actor", syncInput); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("idempotency key reused for another event commit: %v", err)
	}
	running.CreatedAt = time.Now().Add(-3 * time.Minute)
	requireTemplateNoError(t, repo.db.Transaction(func(tx *gorm.DB) error { return writeTemplateSyncRun(tx, &running) }))
	recovered, err := repo.GetTemplateSyncRun(ctx, source.ID, running.ID)
	requireTemplateNoError(t, err)
	if recovered.Status != "failed" || recovered.ErrorCode != "sync_interrupted" {
		t.Fatal("interrupted run stayed running")
	}
}

func verifyWorkflowSourceOwnership(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	source, err := repo.SaveTemplateSource(ctx, domaindocument.Source{Name: "Workflow", RepositoryID: uuid.NewString(), Enabled: true}, 0)
	requireTemplateNoError(t, err)
	mutation := domaindocument.Mutation{Candidate: domaindocument.Candidate{Path: "flow.soha.json", Action: "create", Document: contractdelivery.Document{Kind: "Workflow", Metadata: contractdelivery.Metadata{Name: "flow"}, Spec: map[string]any{}}}, Workflow: domainworkflow.DeliveryWorkflowInput{Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "Git flow", Targets: []domainworkflow.DeliveryTargetInput{}}}}
	run := readySourceRun(t, ctx, repo, source, []domaindocument.Mutation{mutation})
	result := applySourceRun(t, ctx, repo, run, []domaindocument.Mutation{mutation})
	id := result.Result.Objects[0].ID
	t.Cleanup(func() { _ = repo.db.Exec(`DELETE FROM delivery_workflows WHERE id = ?`, id).Error })
	workflow, err := workflowrepo.New(repo.db).GetDeliveryWorkflow(ctx, id)
	requireTemplateNoError(t, err)
	workflow.Definition.Name = "UI change"
	if _, err := workflowrepo.New(repo.db).SaveDeliveryWorkflow(ctx, workflow, 1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("saved workflow bypassed Git ownership: %v", err)
	}
	info, err := repo.GetDocumentSource(ctx, "Workflow", id, 1)
	requireTemplateNoError(t, err)
	if info.Provenance == nil || info.Provenance.ObjectID != id {
		t.Fatal("workflow import lost version provenance")
	}
}
