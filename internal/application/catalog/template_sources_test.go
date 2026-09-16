package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type templateSourceStub struct {
	info domaindocument.SourceInfo
	TemplateSourceRepository
	TemplateSyncRepository
	source             domaindocument.Source
	objects            []domaindocument.StoredAssociation
	run                domaindocument.StoredSyncRun
	mutations          []domaindocument.Mutation
	saves, applies     int
	finishContextError error
}

func (r *templateSourceStub) GetTemplateSource(context.Context, string) (domaindocument.Source, error) {
	return r.source, nil
}
func (r *templateSourceStub) SaveTemplateSource(_ context.Context, source domaindocument.Source, _ int) (domaindocument.Source, error) {
	r.saves++
	return source, nil
}
func (r *templateSourceStub) ListTemplateSourceObjects(context.Context, string) ([]domaindocument.StoredAssociation, error) {
	return r.objects, nil
}
func (r *templateSourceStub) BeginTemplateSync(_ context.Context, source, actor string, input domaindocument.SyncInput) (domaindocument.StoredSyncRun, bool, error) {
	if r.run.ID != "" && r.run.IdempotencyKey == input.IdempotencyKey {
		return r.run, false, nil
	}
	r.run = domaindocument.StoredSyncRun{SyncRun: domaindocument.SyncRun{ID: "sync-1", SourceID: source, ActorID: actor, SourceGeneration: input.ExpectedGeneration, Status: "running", RequestedCommit: input.ResolvedCommit}, IdempotencyKey: input.IdempotencyKey}
	return r.run, true, nil
}
func (r *templateSourceStub) FinishTemplateSync(ctx context.Context, run domaindocument.StoredSyncRun) (domaindocument.StoredSyncRun, error) {
	r.run, r.finishContextError = run, ctx.Err()
	return run, nil
}
func (r *templateSourceStub) GetTemplateSyncRun(context.Context, string, string) (domaindocument.StoredSyncRun, error) {
	return r.run, nil
}
func (r *templateSourceStub) ApplyTemplateSync(_ context.Context, run domaindocument.StoredSyncRun, _ domaindocument.SyncApplyInput, mutations []domaindocument.Mutation) (domaindocument.StoredSyncRun, error) {
	r.applies++
	r.mutations = mutations
	run.Status = "applied"
	return run, nil
}

type sourceRepositoryStub struct {
	repository domainapp.SourceRepository
	err        error
}

func (s sourceRepositoryStub) GetRepository(context.Context, domainidentity.Principal, string) (domainapp.SourceRepository, error) {
	return s.repository, s.err
}

type templateGitStub struct {
	files  []domaindocument.File
	reads  int
	err    error
	source domaindocument.Source
}

func (g *templateGitStub) ReadDeliveryDocuments(_ context.Context, _ domainapp.SourceRepository, source domaindocument.Source) (domaindocument.GitDocuments, error) {
	g.reads++
	g.source = source
	return domaindocument.GitDocuments{Files: g.files, ResolvedCommit: strings.Repeat("a", 40), TreeDigest: strings.Repeat("b", 40)}, g.err
}

func TestTemplateSourceSyncPinsRequestedCommitWithoutChangingConfiguredRef(t *testing.T) {
	service, repo, git, principal := newTemplateSourceTestService()
	commit := strings.Repeat("a", 40)
	input := domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "event-sync", ResolvedCommit: commit}
	run, err := service.Sync(t.Context(), principal, "source", input)
	if err != nil || run.Status != "ready" || git.source.RefType != "commit" || git.source.RefValue != commit || repo.source.RefType != "branch" || repo.source.RefValue != "main" {
		t.Fatalf("event commit was not frozen: %+v %v", git.source, err)
	}
	input.ResolvedCommit = "main"
	if _, err := service.Sync(t.Context(), principal, "source", input); !errors.Is(err, apperrors.ErrInvalidArgument) || git.reads != 1 {
		t.Fatalf("floating event commit was fetched: %v", err)
	}
}

func sourceTestPermissions() *appaccess.PermissionResolver {
	return catalogPermissions("delivery.template-sources.view", "delivery.template-sources.create", "delivery.template-sources.update", "delivery.template-sources.sync", appaccess.PermDeliveryBuildTemplatesManage, appaccess.PermDeliveryBuildTemplatesView)
}

func newTemplateSourceTestService() (*TemplateSourceService, *templateSourceStub, *templateGitStub, domainidentity.Principal) {
	documents, _, _, principal := newDocumentTestService()
	documents.catalog.permissions = sourceTestPermissions()
	documents.sources = sourceRepositoryStub{repository: domainapp.SourceRepository{ID: "repo", URL: "https://git.example/catalog.git", SourceConnectionID: "connection", ProviderRepositoryID: "42", CredentialRef: "connection"}}
	repo := &templateSourceStub{source: domaindocument.Source{ID: "source", Generation: 1, RepositoryID: "repo", Enabled: true, Path: ".", RefType: "branch", RefValue: "main", Kinds: []contractsapi.DeliveryDocumentKind{"BuildTemplate"}}}
	git := &templateGitStub{files: []domaindocument.File{{Path: "build.soha.yaml", Content: buildDocumentYAML}}}
	return NewTemplateSources(documents, repo, repo, git), repo, git, principal
}

func TestTemplateSourceSavesWithoutFetchingAndRechecksPermissions(t *testing.T) {
	service, repo, git, principal := newTemplateSourceTestService()
	input := domaindocument.SourceInput{Name: "Catalog", RepositoryID: "repo", Path: ".", RefType: "branch", RefValue: "main", Kinds: repo.source.Kinds, Enabled: true}
	if _, err := service.Save(context.Background(), principal, "", input); err != nil || repo.saves != 1 || git.reads != 0 {
		t.Fatalf("save executed Git: saves=%d reads=%d err=%v", repo.saves, git.reads, err)
	}
	service.documents.sources = sourceRepositoryStub{err: apperrors.ErrAccessDenied}
	if _, err := service.Sync(context.Background(), principal, repo.source.ID, domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "sync-key"}); !errors.Is(err, apperrors.ErrAccessDenied) || git.reads != 0 {
		t.Fatalf("source permission bypass: %v", err)
	}
}

func TestTemplateSourcePreviewIsActorBoundAndAppliesFixedContent(t *testing.T) {
	service, repo, git, principal := newTemplateSourceTestService()
	ctx := context.Background()
	run, err := service.Sync(ctx, principal, repo.source.ID, domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "sync-key"})
	if err != nil || run.Status != "ready" || run.Preview == nil || run.Preview.ID != run.ID || run.Preview.ExpiresAt == nil || repo.applies != 0 {
		t.Fatalf("invalid preview: %+v %v", run, err)
	}
	input := domaindocument.SyncApplyInput{ExpectedGeneration: 1, CandidateDigest: run.Preview.CandidateDigest, IdempotencyKey: "apply-key"}
	other := principal
	other.UserID = "other"
	read, err := service.Run(ctx, other, repo.source.ID, run.ID)
	if err != nil || read.Preview != nil {
		t.Fatalf("candidate exposed to another actor: %+v %v", read, err)
	}
	if _, err := service.Apply(ctx, other, repo.source.ID, run.ID, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("another actor applied preview: %v", err)
	}
	service.documents.catalog.permissions = catalogPermissions("delivery.template-sources.view", "delivery.template-sources.sync")
	if _, err := service.Apply(ctx, principal, repo.source.ID, run.ID, input); !errors.Is(err, apperrors.ErrAccessDenied) || repo.applies != 0 {
		t.Fatalf("revoked template write accepted: %v", err)
	}
	service.documents.catalog.permissions = sourceTestPermissions()
	git.files[0].Content = "invalid new branch content"
	if _, err := service.Apply(ctx, principal, repo.source.ID, run.ID, input); err != nil || git.reads != 1 || repo.applies != 1 || repo.mutations[0].Build.BuildCommands[0] != "echo build" {
		t.Fatalf("apply did not use fixed contents: %+v %v", repo.mutations, err)
	}
}

func TestTemplateSourceRenameAfterPublishAndEmptyTree(t *testing.T) {
	service, repo, git, principal := newTemplateSourceTestService()
	ctx := context.Background()
	before, revision, _, err := service.documents.readDocument(ctx, principal, "BuildTemplate", "build-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	repo.objects = []domaindocument.StoredAssociation{{Association: domaindocument.Association{SourceID: "source", Kind: "BuildTemplate", Key: "example-build", ObjectID: "build-1", Path: "old.soha.yaml", LastImportedRevision: int(revision) - 1}, Document: before}}
	git.files[0].Path = "renamed.soha.yaml"
	run, err := service.Sync(ctx, principal, "source", domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "sync-key"})
	if err != nil || run.Status != "ready" || run.Preview.Candidates[0].TargetID != "build-1" || run.Preview.Candidates[0].Action != "unchanged" || run.Preview.Candidates[0].ExpectedRevision != revision {
		t.Fatalf("publication or file rename created a false conflict: %+v %v", run, err)
	}
	git.files = nil
	run, err = service.Sync(ctx, principal, "source", domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "empty-key"})
	if err != nil || run.Status != "ready" || len(run.Removed) != 1 || len(run.Preview.Candidates) != 0 || repo.applies != 0 {
		t.Fatalf("empty tree did not produce explicit removal preview: %+v %v", run, err)
	}
}

func TestTemplateSourceInvalidFilesAndGitFailureDoNotWriteObjects(t *testing.T) {
	service, repo, git, principal := newTemplateSourceTestService()
	git.files = append(git.files, domaindocument.File{Path: "invalid.json", Content: "not JSON"})
	run, err := service.Sync(context.Background(), principal, "source", domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "sync-key"})
	if err != nil || run.Status != "invalid" || len(run.Preview.Diagnostics) == 0 || repo.applies != 0 {
		t.Fatalf("invalid batch was accepted: %+v %v", run, err)
	}
	if _, err := service.Apply(context.Background(), principal, "source", run.ID, domaindocument.SyncApplyInput{IdempotencyKey: "apply-key"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("invalid candidate applied: %v", err)
	}
	git.err = fmt.Errorf("secret command output")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, err = service.Sync(ctx, principal, "source", domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "failed-key"})
	if err != nil || run.Status != "failed" || strings.Contains(run.ErrorMessage, "secret") || run.Preview != nil || repo.finishContextError != nil {
		t.Fatalf("failure did not finalize safely: %+v %v", run, err)
	}
}

func TestTemplateSourceDuplicateIdentityAndDraftDrift(t *testing.T) {
	service, repo, git, principal := newTemplateSourceTestService()
	git.files = append(git.files, domaindocument.File{Path: "duplicate.soha.yaml", Content: buildDocumentYAML})
	run, err := service.Sync(context.Background(), principal, "source", domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "sync-key"})
	if err != nil || run.Status != "invalid" || run.Preview.Diagnostics[0].Code != "duplicate_key" {
		t.Fatalf("duplicate identity accepted: %+v %v", run, err)
	}
	git.files = git.files[:1]
	repo.objects = []domaindocument.StoredAssociation{{Association: domaindocument.Association{Kind: "BuildTemplate", Key: "example-build", ObjectID: "build-1"}, Document: contractdelivery.Document{Kind: "BuildTemplate", Spec: map[string]any{}}}}
	run, err = service.Sync(context.Background(), principal, "source", domaindocument.SyncInput{ExpectedGeneration: 1, IdempotencyKey: "drift-key"})
	if err != nil || run.Status != "invalid" || run.Preview.Diagnostics[0].Code != "source_conflict" {
		t.Fatalf("draft drift accepted: %+v %v", run, err)
	}
}

func (r *templateSourceStub) GetDocumentSource(context.Context, string, string, int64) (domaindocument.SourceInfo, error) {
	return r.info, nil
}

func TestTemplateSourceInfoRequiresRepositoryPermissionForCloneAddress(t *testing.T) {
	service, repo, _, principal := newTemplateSourceTestService()
	repo.info = domaindocument.SourceInfo{Association: &domaindocument.Association{SourceID: repo.source.ID}}
	info, err := service.SourceInfo(t.Context(), principal, "BuildTemplate", "build-1", 0)
	if err != nil || info.Repository == nil || info.Repository.URL != "https://git.example/catalog.git" {
		t.Fatalf("repository unavailable: %+v %v", info, err)
	}
	service.documents.sources = sourceRepositoryStub{err: apperrors.ErrAccessDenied}
	info, err = service.SourceInfo(t.Context(), principal, "BuildTemplate", "build-1", 0)
	if err != nil || info.Repository != nil || info.Association == nil {
		t.Fatalf("repository details leaked or source audit lost: %+v %v", info, err)
	}
}
