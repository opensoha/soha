package catalog

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func testBuildDocument(key string) domaindocument.Mutation {
	document := contractdelivery.Document{APIVersion: contractdelivery.APIVersion, Kind: "BuildTemplate", Metadata: contractdelivery.Metadata{Name: key, DisplayName: "Imported build"}, Spec: map[string]any{"buildCommands": []any{"echo imported"}, "enabled": true}}
	return domaindocument.Mutation{
		Candidate: domaindocument.Candidate{Path: key + ".yaml", Document: document, Action: "create", SourceDigest: contractdelivery.Digest([]byte(key)), ChangedPaths: []string{""}},
		Build:     domaincatalog.BuildTemplateInput{Key: key, Name: "Imported build", BuildCommands: []string{"echo imported"}, Enabled: true},
	}
}

func saveTestDocumentPreview(t *testing.T, ctx context.Context, repo *Repository, actor string, mutations []domaindocument.Mutation) domaindocument.StoredPreview {
	t.Helper()
	expires := time.Now().UTC().Add(30 * time.Minute)
	preview := domaindocument.StoredPreview{Preview: domaindocument.Preview{ID: uuid.NewString(), Valid: true, ExpiresAt: &expires}, ActorID: actor}
	for _, mutation := range mutations {
		preview.Candidates = append(preview.Candidates, mutation.Candidate)
	}
	preview.CandidateDigest = contractdelivery.Digest([]byte(preview.ID))
	requireTemplateNoError(t, repo.SaveDocumentPreview(ctx, preview))
	return preview
}

func verifyDocumentImports(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	actor := uuid.NewString()
	mutations := []domaindocument.Mutation{testBuildDocument("a-" + uuid.NewString()), testBuildDocument("b-" + uuid.NewString())}
	preview := saveTestDocumentPreview(t, ctx, repo, actor, mutations)
	if _, err := repo.GetDocumentPreview(ctx, preview.ID, "another-actor"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("preview leaked across actors: %v", err)
	}
	// Force a conflict on the second insert, after the first write has happened.
	occupied, err := repo.CreateBuildTemplate(ctx, mutations[1].Build)
	requireTemplateNoError(t, err)
	if _, err := repo.ApplyDocumentImport(ctx, preview, "atomic-import", mutations); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expected key conflict: %v", err)
	}
	if _, err := repo.FindDocumentKey(ctx, "BuildTemplate", mutations[0].Build.Key); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("partial import survived rollback: %v", err)
	}
	stored, err := repo.GetDocumentPreview(ctx, preview.ID, actor)
	requireTemplateNoError(t, err)
	if stored.Result != nil || stored.IdempotencyKey != "" {
		t.Fatal("failed import recorded success")
	}
	requireTemplateNoError(t, repo.db.Exec(`DELETE FROM build_templates WHERE id = ?`, occupied.ID).Error)
	results := make(chan domaindocument.Import, 2)
	errorsCh := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := repo.ApplyDocumentImport(ctx, preview, "atomic-import", mutations)
			results <- result
			errorsCh <- err
		}()
	}
	first, second := <-results, <-results
	requireTemplateNoError(t, <-errorsCh)
	requireTemplateNoError(t, <-errorsCh)
	if len(first.Objects) != 2 || !reflect.DeepEqual(first, second) {
		t.Fatalf("concurrent replay created different results: %+v / %+v", first, second)
	}
	for _, imported := range first.Objects {
		item, err := repo.GetBuildTemplate(ctx, imported.ID)
		requireTemplateNoError(t, err)
		if item.Revision != 1 || item.PublishedVersion != 0 || item.PublicationState != "draft" {
			t.Fatalf("import was published or duplicated: %+v", item)
		}
	}
	if _, err := repo.ApplyDocumentImport(ctx, preview, "different-key", mutations); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("applied preview accepted a different key: %v", err)
	}
	next := saveTestDocumentPreview(t, ctx, repo, actor, []domaindocument.Mutation{testBuildDocument(uuid.NewString())})
	if _, err := repo.ApplyDocumentImport(ctx, next, "atomic-import", []domaindocument.Mutation{{Candidate: next.Candidates[0]}}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("idempotency key reused on a different preview: %v", err)
	}
	verifyDocumentImportCAS(t, ctx, repo, actor, first.Objects[0], mutations[0])
}

func verifyDocumentImportCAS(t *testing.T, ctx context.Context, repo *Repository, actor string, target domaindocument.ImportedObject, mutation domaindocument.Mutation) {
	t.Helper()
	mutation.TargetID, mutation.ExpectedRevision, mutation.Action, mutation.ChangedPaths = target.ID, 1, "unchanged", []string{}
	mutation.Build.ExpectedRevision = &mutation.ExpectedRevision
	preview := saveTestDocumentPreview(t, ctx, repo, actor, []domaindocument.Mutation{mutation})
	result, err := repo.ApplyDocumentImport(ctx, preview, "unchanged-import", []domaindocument.Mutation{mutation})
	requireTemplateNoError(t, err)
	if result.Objects[0].Revision != 1 {
		t.Fatal("unchanged import incremented revision")
	}
	stale := saveTestDocumentPreview(t, ctx, repo, actor, []domaindocument.Mutation{mutation})
	_, err = repo.PublishBuildTemplate(ctx, target.ID, 1)
	requireTemplateNoError(t, err)
	if _, err := repo.ApplyDocumentImport(ctx, stale, "stale-import", []domaindocument.Mutation{mutation}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unchanged candidate bypassed CAS: %v", err)
	}
	mutation.ExpectedRevision, mutation.Action = 2, "update"
	mutation.Build.ExpectedRevision, mutation.Build.BuildCommands = &mutation.ExpectedRevision, []string{"echo updated"}
	update := saveTestDocumentPreview(t, ctx, repo, actor, []domaindocument.Mutation{mutation})
	_, err = repo.ApplyDocumentImport(ctx, update, "draft-update", []domaindocument.Mutation{mutation})
	requireTemplateNoError(t, err)
	version, err := repo.GetBuildTemplateVersion(ctx, target.ID, 1)
	requireTemplateNoError(t, err)
	head, err := repo.GetBuildTemplate(ctx, target.ID)
	requireTemplateNoError(t, err)
	if version.BuildCommands[0] != "echo imported" || head.BuildCommands[0] != "echo updated" || head.PublishedVersion != 1 || head.PublicationState != "draft" {
		t.Fatal("import changed a published version")
	}
	mutation.ExpectedRevision, mutation.Action = 3, "unchanged"
	deprecated := saveTestDocumentPreview(t, ctx, repo, actor, []domaindocument.Mutation{mutation})
	requireTemplateNoError(t, repo.DeleteBuildTemplate(ctx, target.ID))
	if _, err := repo.ApplyDocumentImport(ctx, deprecated, "deprecated-import", []domaindocument.Mutation{mutation}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("deprecated candidate accepted: %v", err)
	}
	expired := saveTestDocumentPreview(t, ctx, repo, actor, []domaindocument.Mutation{testBuildDocument(uuid.NewString())})
	requireTemplateNoError(t, repo.db.Exec(`UPDATE delivery_document_imports SET expires_at = NOW() - INTERVAL '1 second' WHERE id = ?`, expired.ID).Error)
	if _, err := repo.ApplyDocumentImport(ctx, expired, "expired-import", []domaindocument.Mutation{{Candidate: expired.Candidates[0]}}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expired candidate accepted: %v", err)
	}
}
