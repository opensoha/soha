package catalog

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var templateSyncCommit = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func (s *TemplateSourceService) Sync(ctx context.Context, principal domainidentity.Principal, id string, input domaindocument.SyncInput) (domaindocument.SyncRun, error) {
	if err := s.authorize(ctx, principal, "sync"); err != nil {
		return domaindocument.SyncRun{}, err
	}
	if len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || input.ExpectedGeneration < 1 {
		return domaindocument.SyncRun{}, apperrors.ErrInvalidArgument
	}
	if input.ResolvedCommit != "" && !templateSyncCommit.MatchString(input.ResolvedCommit) {
		return domaindocument.SyncRun{}, apperrors.ErrInvalidArgument
	}
	source, err := s.Get(ctx, principal, id)
	if err != nil {
		return domaindocument.SyncRun{}, err
	}
	repository, err := s.documents.sources.GetRepository(ctx, principal, source.RepositoryID)
	if err != nil {
		return domaindocument.SyncRun{}, err
	}
	run, created, err := s.runs.BeginTemplateSync(ctx, id, principal.UserID, input)
	if err != nil {
		return domaindocument.SyncRun{}, err
	}
	if !created {
		return s.visibleSyncRun(ctx, principal, run)
	}
	syncCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	err = s.prepareTemplateSync(syncCtx, principal, source, repository, &run)
	if err != nil {
		run.Status, run.ErrorCode, run.ErrorMessage, run.Preview, run.Removed = "failed", "sync_failed", "Source could not be read or validated; check connection, permissions and source limits.", nil, nil
	}
	// Record cancellation and timeout with a separate bounded context; never rerun a
	// floating ref on recovery. A crash leaves a lease that expires on the next read.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	finished, finishErr := s.runs.FinishTemplateSync(finishCtx, run)
	if finishErr != nil {
		return domaindocument.SyncRun{}, finishErr
	}
	s.record(finishCtx, principal, "sync", id, "completed bounded source read and draft preview without execution")
	if errors.Is(err, apperrors.ErrAccessDenied) {
		return domaindocument.SyncRun{}, apperrors.ErrAccessDenied
	}
	return finished.SyncRun, nil
}

func (s *TemplateSourceService) prepareTemplateSync(ctx context.Context, principal domainidentity.Principal, source domaindocument.Source, repository domainapp.SourceRepository, run *domaindocument.StoredSyncRun) error {
	if err := validateTemplateRepository(repository); err != nil {
		return err
	}
	readSource := source
	if run.RequestedCommit != "" {
		readSource.RefType, readSource.RefValue = "commit", run.RequestedCommit
	}
	content, err := s.git.ReadDeliveryDocuments(ctx, repository, readSource)
	if err != nil {
		return err
	}
	if len(content.Files) > 0 {
		if err := validateDocumentFiles(content.Files); err != nil {
			return err
		}
	}
	objects, err := s.repo.ListTemplateSourceObjects(ctx, source.ID)
	if err != nil {
		return err
	}
	preview, removed, err := s.previewSourceFiles(ctx, principal, source, content.Files, objects)
	if err != nil {
		return err
	}
	run.ResolvedCommit, run.TreeDigest, run.Preview, run.Removed = content.ResolvedCommit, content.TreeDigest, &preview, removed
	run.Status = "invalid"
	if preview.Valid {
		run.Status = "ready"
		preview.ID = run.ID
		expires := time.Now().UTC().Add(30 * time.Minute)
		preview.ExpiresAt = &expires
	}
	return nil
}

func (s *TemplateSourceService) previewSourceFiles(ctx context.Context, principal domainidentity.Principal, source domaindocument.Source, files []domaindocument.File, objects []domaindocument.StoredAssociation) (domaindocument.Preview, []domaindocument.Association, error) {
	preview := domaindocument.Preview{Candidates: []domaindocument.Candidate{}, Diagnostics: []contractdelivery.Diagnostic{}}
	known := map[string]domaindocument.StoredAssociation{}
	for _, object := range objects {
		known[string(object.Kind)+":"+object.Key] = object
	}
	seen, keys := map[string]bool{}, map[string]bool{}
	for _, file := range files {
		candidate, invalid, err := s.previewSourceFile(ctx, principal, source, file, known, seen, keys)
		if err != nil {
			return preview, nil, err
		}
		preview.Diagnostics = append(preview.Diagnostics, invalid...)
		if len(invalid) == 0 {
			preview.Candidates = append(preview.Candidates, candidate)
		}
	}
	removed := []domaindocument.Association{}
	for _, object := range objects {
		if seen[string(object.Kind)+":"+object.Key] || object.Removed {
			continue
		}
		if err := s.authorizeSourceRemoval(ctx, principal, object, "keep"); err != nil {
			return preview, nil, err
		}
		removed = append(removed, object.Association)
	}
	if len(preview.Diagnostics) > 0 {
		return preview, removed, nil
	}
	slices.SortFunc(preview.Candidates, func(a, b domaindocument.Candidate) int { return strings.Compare(a.Path, b.Path) })
	var err error
	preview.CandidateDigest, err = documentCandidateDigest(preview.Candidates)
	preview.Valid = err == nil
	return preview, removed, err
}

func (s *TemplateSourceService) previewSourceFile(ctx context.Context, principal domainidentity.Principal, source domaindocument.Source, file domaindocument.File, known map[string]domaindocument.StoredAssociation, seen, keys map[string]bool) (domaindocument.Candidate, []contractdelivery.Diagnostic, error) {
	document, invalid := contractdelivery.Parse(file.Path, []byte(file.Content))
	if len(invalid) > 0 {
		return domaindocument.Candidate{}, invalid, nil
	}
	if !slices.Contains(source.Kinds, contractsapi.DeliveryDocumentKind(document.Kind)) {
		return domaindocument.Candidate{}, []contractdelivery.Diagnostic{documentDiagnostic(file.Path, "kind_not_allowed", "document kind is not enabled for this source")}, nil
	}
	key := document.Kind + ":" + document.Metadata.Name
	seen[key] = true
	if object, exists := known[key]; exists {
		current, revision, state, err := s.documents.readDocument(ctx, principal, document.Kind, object.ObjectID, 0)
		if err != nil {
			return domaindocument.Candidate{}, nil, err
		}
		if document.Kind == "Workflow" {
			current.Metadata = object.Document.Metadata
		}
		if state == "deprecated" || !reflect.DeepEqual(current.Value(), object.Document.Value()) {
			return domaindocument.Candidate{}, []contractdelivery.Diagnostic{documentDiagnostic(file.Path, "source_conflict", "managed definition changed or was deprecated; resolve its source before importing")}, nil
		}
		file.TargetID, file.ExpectedRevision = object.ObjectID, int(revision)
	}
	candidate, invalid, err := s.documents.previewFile(ctx, principal, file, keys)
	if errors.Is(err, apperrors.ErrConflict) {
		return candidate, []contractdelivery.Diagnostic{documentDiagnostic(file.Path, "source_conflict", "document key or target is already managed or changed; use a new key or preview again")}, nil
	}
	return candidate, invalid, err
}

func (s *TemplateSourceService) Run(ctx context.Context, principal domainidentity.Principal, source, id string) (domaindocument.SyncRun, error) {
	if _, err := s.Get(ctx, principal, source); err != nil {
		return domaindocument.SyncRun{}, err
	}
	run, err := s.runs.GetTemplateSyncRun(ctx, source, id)
	if err != nil {
		return domaindocument.SyncRun{}, err
	}
	return s.visibleSyncRun(ctx, principal, run)
}

func (s *TemplateSourceService) Runs(ctx context.Context, principal domainidentity.Principal, source string, offset, limit int) ([]domaindocument.SyncRun, error) {
	if _, err := s.Get(ctx, principal, source); err != nil {
		return nil, err
	}
	if !validSourcePagination(offset, limit) {
		return nil, apperrors.ErrInvalidArgument
	}
	return s.runs.ListTemplateSyncRuns(ctx, source, offset, limit)
}

func (s *TemplateSourceService) visibleSyncRun(ctx context.Context, principal domainidentity.Principal, run domaindocument.StoredSyncRun) (domaindocument.SyncRun, error) {
	if run.ActorID != principal.UserID {
		run.Preview, run.Removed, run.Result = nil, nil, nil
		return run.SyncRun, nil
	}
	if run.Preview != nil {
		for _, candidate := range run.Preview.Candidates {
			if _, err := s.documents.prepareDocument(ctx, principal, candidate); err != nil {
				return domaindocument.SyncRun{}, err
			}
		}
	}
	for _, object := range run.Removed {
		if _, _, _, err := s.documents.readDocument(ctx, principal, string(object.Kind), object.ObjectID, 0); err != nil {
			return domaindocument.SyncRun{}, err
		}
	}
	if run.Result != nil {
		for _, object := range run.Result.Objects {
			if _, _, _, err := s.documents.readDocument(ctx, principal, object.Kind, object.ID, 0); err != nil {
				return domaindocument.SyncRun{}, err
			}
		}
	}
	return run.SyncRun, nil
}

func (s *TemplateSourceService) Apply(ctx context.Context, principal domainidentity.Principal, source, id string, input domaindocument.SyncApplyInput) (domaindocument.SyncRun, error) {
	if err := s.authorize(ctx, principal, "sync"); err != nil {
		return domaindocument.SyncRun{}, err
	}
	if len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return domaindocument.SyncRun{}, apperrors.ErrInvalidArgument
	}
	if _, err := s.Get(ctx, principal, source); err != nil {
		return domaindocument.SyncRun{}, err
	}
	run, err := s.runs.GetTemplateSyncRun(ctx, source, id)
	if err != nil {
		return domaindocument.SyncRun{}, err
	}
	if run.ActorID != principal.UserID {
		return domaindocument.SyncRun{}, apperrors.ErrAccessDenied
	}
	if run.Preview == nil || !run.Preview.Valid || run.Status != "ready" && run.Status != "applied" {
		return domaindocument.SyncRun{}, fmt.Errorf("%w: sync has no valid preview", apperrors.ErrConflict)
	}
	mutations := make([]domaindocument.Mutation, 0, len(run.Preview.Candidates))
	for _, candidate := range run.Preview.Candidates {
		mutation, err := s.documents.prepareDocument(ctx, principal, candidate)
		if err != nil {
			return domaindocument.SyncRun{}, err
		}
		if !reflect.DeepEqual(mutation.Candidate, candidate) {
			return domaindocument.SyncRun{}, fmt.Errorf("%w: references changed; preview again", apperrors.ErrConflict)
		}
		mutations = append(mutations, mutation)
	}
	for _, object := range run.Removed {
		if err := s.authorizeSourceRemoval(ctx, principal, domaindocument.StoredAssociation{Association: object}, "keep"); err != nil {
			return domaindocument.SyncRun{}, err
		}
	}
	applied, err := s.runs.ApplyTemplateSync(ctx, run, input, mutations)
	if err == nil {
		s.record(ctx, principal, "import", source, "accepted fixed-commit definitions as drafts without publishing or execution")
	}
	return applied.SyncRun, err
}
