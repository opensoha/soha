package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	contractdelivery "github.com/opensoha/soha-contracts/delivery"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DocumentRepository interface {
	FindDocumentKey(context.Context, string, string) (string, error)
	SaveDocumentPreview(context.Context, domaindocument.StoredPreview) error
	GetDocumentPreview(context.Context, string, string) (domaindocument.StoredPreview, error)
	ApplyDocumentImport(context.Context, domaindocument.StoredPreview, string, []domaindocument.Mutation) (domaindocument.Import, error)
}

type DocumentWorkflowService interface {
	GetDeliveryWorkflow(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryWorkflow, error)
	PrepareDeliveryWorkflow(context.Context, domainidentity.Principal, string, domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error)
}

type DocumentSourceReader interface {
	GetRepository(context.Context, domainidentity.Principal, string) (domainapp.SourceRepository, error)
}

type DocumentService struct {
	catalog   *Service
	workflows DocumentWorkflowService
	sources   DocumentSourceReader
	repo      DocumentRepository
}

func NewDocuments(catalog *Service, workflows DocumentWorkflowService, sources DocumentSourceReader, repo DocumentRepository) *DocumentService {
	for _, dependency := range []any{catalog, workflows, sources, repo} {
		if dependency == nil || reflect.ValueOf(dependency).Kind() == reflect.Pointer && reflect.ValueOf(dependency).IsNil() {
			panic("delivery documents require catalog, workflows, sources and repository")
		}
	}
	return &DocumentService{catalog: catalog, workflows: workflows, sources: sources, repo: repo}
}

func (s *DocumentService) Preview(ctx context.Context, principal domainidentity.Principal, input domaindocument.PreviewInput) (domaindocument.Preview, error) {
	result := domaindocument.Preview{Candidates: []domaindocument.Candidate{}, Diagnostics: []contractdelivery.Diagnostic{}}
	if principal.UserID == "" {
		return result, apperrors.ErrAccessDenied
	}
	if err := validateDocumentFiles(input.Files); err != nil {
		return result, err
	}
	keys := map[string]bool{}
	for _, file := range input.Files {
		candidate, invalid, err := s.previewFile(ctx, principal, file, keys)
		if err != nil {
			return result, err
		}
		result.Diagnostics = append(result.Diagnostics, invalid...)
		if len(invalid) == 0 {
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	if len(result.Diagnostics) != 0 {
		return result, nil
	}
	slices.SortFunc(result.Candidates, func(a, b domaindocument.Candidate) int { return strings.Compare(a.Path, b.Path) })
	var err error
	result.CandidateDigest, err = documentCandidateDigest(result.Candidates)
	if err != nil {
		return result, err
	}
	result.Valid = true
	if input.ValidateOnly {
		return result, nil
	}
	result.ID = uuid.NewString()
	expires := time.Now().UTC().Add(30 * time.Minute)
	result.ExpiresAt = &expires
	if err := s.repo.SaveDocumentPreview(ctx, domaindocument.StoredPreview{Preview: result, ActorID: principal.UserID}); err != nil {
		return domaindocument.Preview{}, err
	}
	s.catalog.recordWriteLogs(ctx, principal, "delivery.document.preview", "DeliveryDocumentImport", result.ID, result.ID, "validated delivery definitions for draft import")
	return result, nil
}

func validateDocumentFiles(files []domaindocument.File) error {
	if len(files) == 0 || len(files) > contractdelivery.MaxFiles {
		return fmt.Errorf("%w: import requires 1 to 100 files", apperrors.ErrInvalidArgument)
	}
	paths, targets := map[string]bool{}, map[string]bool{}
	total := 0
	for _, file := range files {
		total += len(file.Content)
		if !contractdelivery.ValidPath(file.Path) || paths[file.Path] || total > contractdelivery.MaxTotalBytes {
			return fmt.Errorf("%w: invalid or duplicated path, or import exceeds 2 MiB", apperrors.ErrInvalidArgument)
		}
		paths[file.Path] = true
		if file.TargetID == "" && file.ExpectedRevision != 0 || file.TargetID != "" && file.ExpectedRevision < 1 {
			return fmt.Errorf("%w: targetId and a positive expectedRevision must be supplied together", apperrors.ErrInvalidArgument)
		}
		if file.TargetID != "" {
			if targets[file.TargetID] {
				return fmt.Errorf("%w: a target cannot be imported twice", apperrors.ErrInvalidArgument)
			}
			targets[file.TargetID] = true
		}
	}
	return nil
}

func (s *DocumentService) previewFile(ctx context.Context, principal domainidentity.Principal, file domaindocument.File, keys map[string]bool) (domaindocument.Candidate, []contractdelivery.Diagnostic, error) {
	candidate := domaindocument.Candidate{Path: file.Path, TargetID: file.TargetID, ExpectedRevision: int64(file.ExpectedRevision), SourceDigest: contractdelivery.Digest([]byte(file.Content)), ChangedPaths: []string{}}
	document, invalid := contractdelivery.Parse(file.Path, []byte(file.Content))
	if len(invalid) > 0 {
		return candidate, invalid, nil
	}
	key := document.Kind + ":" + document.Metadata.Name
	if keys[key] {
		return candidate, []contractdelivery.Diagnostic{documentDiagnostic(file.Path, "duplicate_key", "document kind and name must be unique in an import")}, nil
	}
	keys[key] = true
	candidate.Document = document
	mutation, err := s.prepareDocument(ctx, principal, candidate)
	if err != nil {
		if errors.Is(err, apperrors.ErrInvalidArgument) {
			return candidate, []contractdelivery.Diagnostic{documentValidationDiagnostic(file.Path, err)}, nil
		}
		return candidate, nil, err
	}
	candidate = mutation.Candidate
	candidate.Action, candidate.ChangedPaths = "create", []string{""}
	if file.TargetID != "" {
		before, revision, state, err := s.readDocument(ctx, principal, document.Kind, file.TargetID, 0)
		if err != nil {
			return candidate, nil, err
		}
		if state == "deprecated" || revision != candidate.ExpectedRevision {
			return candidate, nil, fmt.Errorf("%w: target changed or was deprecated; preview again", apperrors.ErrConflict)
		}
		if document.Kind != "Workflow" && before.Metadata.Name != document.Metadata.Name {
			return candidate, nil, fmt.Errorf("%w: importing cannot rename an existing template key", apperrors.ErrConflict)
		}
		if document.Kind == "Workflow" {
			before.Metadata = candidate.Document.Metadata
		}
		candidate.Action, candidate.ChangedPaths = "update", documentChangedPaths(before.Value(), candidate.Document.Value(), "")
		if len(candidate.ChangedPaths) == 0 {
			candidate.Action = "unchanged"
		}
	} else if document.Kind != "Workflow" {
		_, err := s.repo.FindDocumentKey(ctx, document.Kind, document.Metadata.Name)
		if err == nil {
			return candidate, nil, fmt.Errorf("%w: template key already exists; select its ID and revision to update", apperrors.ErrConflict)
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return candidate, nil, err
		}
	}
	return candidate, nil, nil
}

func (s *DocumentService) Apply(ctx context.Context, principal domainidentity.Principal, id string, input domaindocument.ApplyInput) (domaindocument.Import, error) {
	if principal.UserID == "" || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return domaindocument.Import{}, fmt.Errorf("%w: actor and 8 to 128 character idempotencyKey are required", apperrors.ErrInvalidArgument)
	}
	preview, err := s.repo.GetDocumentPreview(ctx, id, principal.UserID)
	if err != nil {
		return domaindocument.Import{}, err
	}
	digest, err := documentCandidateDigest(preview.Candidates)
	if err != nil || digest != preview.CandidateDigest || digest != input.CandidateDigest {
		return domaindocument.Import{}, fmt.Errorf("%w: preview content does not match; preview again", apperrors.ErrConflict)
	}
	if preview.Result != nil {
		return s.replayDocumentImport(ctx, principal, preview, input.IdempotencyKey)
	}
	if preview.ExpiresAt == nil || !time.Now().Before(*preview.ExpiresAt) {
		return domaindocument.Import{}, fmt.Errorf("%w: import preview expired", apperrors.ErrConflict)
	}
	mutations := make([]domaindocument.Mutation, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		mutation, err := s.prepareDocument(ctx, principal, candidate)
		if err != nil {
			return domaindocument.Import{}, err
		}
		if mutation.NormalizedSpecDigest != candidate.NormalizedSpecDigest {
			return domaindocument.Import{}, fmt.Errorf("%w: referenced definition changed; preview again", apperrors.ErrConflict)
		}
		mutations = append(mutations, mutation)
	}
	result, err := s.repo.ApplyDocumentImport(ctx, preview, input.IdempotencyKey, mutations)
	if err == nil {
		s.catalog.recordWriteLogs(ctx, principal, "delivery.document.import", "DeliveryDocumentImport", id, id, "imported delivery definitions as drafts without execution")
	}
	return result, err
}

func (s *DocumentService) replayDocumentImport(ctx context.Context, principal domainidentity.Principal, preview domaindocument.StoredPreview, key string) (domaindocument.Import, error) {
	if preview.IdempotencyKey != key {
		return domaindocument.Import{}, fmt.Errorf("%w: preview was already applied with another key", apperrors.ErrConflict)
	}
	for _, item := range preview.Result.Objects {
		if _, _, _, err := s.readDocument(ctx, principal, item.Kind, item.ID, 0); err != nil {
			return domaindocument.Import{}, err
		}
	}
	return *preview.Result, nil
}

func documentCandidateDigest(candidates []domaindocument.Candidate) (string, error) {
	entries := make([]any, 0, len(candidates))
	for _, candidate := range candidates {
		entries = append(entries, map[string]any{"path": candidate.Path, "sourceDigest": candidate.SourceDigest})
	}
	data, err := contractdelivery.CanonicalJSON(entries)
	if err != nil {
		return "", err
	}
	return contractdelivery.Digest(data), nil
}

func documentChangedPaths(before, after any, pointer string) []string {
	if reflect.DeepEqual(before, after) {
		return []string{}
	}
	a, aMap := before.(map[string]any)
	b, bMap := after.(map[string]any)
	if !aMap || !bMap {
		return []string{pointer}
	}
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	changed := []string{}
	for _, key := range slices.Compact(keys) {
		path := pointer + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
		changed = append(changed, documentChangedPaths(a[key], b[key], path)...)
	}
	return changed
}

func documentDiagnostic(path, code, message string) contractdelivery.Diagnostic {
	return contractdelivery.Diagnostic{Path: path, Document: 1, Code: code, Message: message}
}

func documentJSON(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}

func documentValidationDiagnostic(path string, err error) contractdelivery.Diagnostic {
	diagnostic := documentDiagnostic(path, "invalid_definition", "definition failed business validation; check parameters, references and workflow structure")
	var business *apperrors.BusinessError
	if errors.As(err, &business) {
		diagnostic.Code, diagnostic.Message = business.Code(), business.Message("en")
	}
	var target *domainworkflow.TargetValidationError
	if errors.As(err, &target) {
		diagnostic.Pointer = fmt.Sprintf("/spec/definition/targets/%d", target.Index)
		field := map[string]string{"delivery_service_build_source_missing": "serviceId", "delivery_target_environment_mismatch": "applicationEnvironmentId", "delivery_target_binding_mismatch": "releaseTargetId", "delivery_target_bundle_mismatch": "releaseBundleId"}[diagnostic.Code]
		if field != "" {
			diagnostic.Pointer += "/" + field
		}
	}
	return diagnostic
}
