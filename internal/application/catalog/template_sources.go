package catalog

import (
	"context"
	"errors"
	"reflect"

	contractsapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type TemplateSourceRepository interface {
	GetTemplateSource(context.Context, string) (domaindocument.Source, error)
	ListTemplateSources(context.Context, int, int) ([]domaindocument.Source, error)
	SaveTemplateSource(context.Context, domaindocument.Source, int) (domaindocument.Source, error)
	ListTemplateSourceObjects(context.Context, string) ([]domaindocument.StoredAssociation, error)
	GetDocumentSource(context.Context, string, string, int64) (domaindocument.SourceInfo, error)
	RemoveTemplateSource(context.Context, string, int, string, string, string) error
}

type TemplateSyncRepository interface {
	BeginTemplateSync(context.Context, string, string, domaindocument.SyncInput) (domaindocument.StoredSyncRun, bool, error)
	FinishTemplateSync(context.Context, domaindocument.StoredSyncRun) (domaindocument.StoredSyncRun, error)
	GetTemplateSyncRun(context.Context, string, string) (domaindocument.StoredSyncRun, error)
	ListTemplateSyncRuns(context.Context, string, int, int) ([]domaindocument.SyncRun, error)
	ApplyTemplateSync(context.Context, domaindocument.StoredSyncRun, domaindocument.SyncApplyInput, []domaindocument.Mutation) (domaindocument.StoredSyncRun, error)
}

type TemplateGitReader interface {
	ReadDeliveryDocuments(context.Context, domainapp.SourceRepository, domaindocument.Source) (domaindocument.GitDocuments, error)
}

type TemplateSourceService struct {
	documents *DocumentService
	repo      TemplateSourceRepository
	runs      TemplateSyncRepository
	git       TemplateGitReader
}

func NewTemplateSources(documents *DocumentService, repo TemplateSourceRepository, runs TemplateSyncRepository, git TemplateGitReader) *TemplateSourceService {
	for _, dependency := range []any{documents, repo, runs, git} {
		if dependency == nil || reflect.ValueOf(dependency).Kind() == reflect.Pointer && reflect.ValueOf(dependency).IsNil() {
			panic("template sources require documents, source repository, sync repository and Git reader")
		}
	}
	return &TemplateSourceService{documents: documents, repo: repo, runs: runs, git: git}
}

func (s *TemplateSourceService) authorize(ctx context.Context, principal domainidentity.Principal, action string) error {
	if principal.UserID == "" {
		return apperrors.ErrAccessDenied
	}
	return s.documents.catalog.authorize(ctx, principal, "delivery.template-sources."+action)
}

func (s *TemplateSourceService) Get(ctx context.Context, principal domainidentity.Principal, id string) (domaindocument.Source, error) {
	if err := s.authorize(ctx, principal, "view"); err != nil {
		return domaindocument.Source{}, err
	}
	item, err := s.repo.GetTemplateSource(ctx, id)
	if err != nil {
		return item, err
	}
	_, err = s.documents.sources.GetRepository(ctx, principal, item.RepositoryID)
	return item, err
}

func (s *TemplateSourceService) List(ctx context.Context, principal domainidentity.Principal, offset, limit int) ([]domaindocument.Source, error) {
	if err := s.authorize(ctx, principal, "view"); err != nil {
		return nil, err
	}
	if !validSourcePagination(offset, limit) {
		return nil, apperrors.ErrInvalidArgument
	}
	items := []domaindocument.Source{}
	visible := 0
	for scanned := 0; ; scanned += 200 {
		batch, err := s.repo.ListTemplateSources(ctx, scanned, 200)
		if err != nil {
			return nil, err
		}
		for _, item := range batch {
			if _, err := s.documents.sources.GetRepository(ctx, principal, item.RepositoryID); err != nil {
				if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
					continue
				}
				return nil, err
			}
			if visible >= offset {
				items = append(items, item)
			}
			visible++
			if len(items) == limit {
				return items, nil
			}
		}
		if len(batch) < 200 {
			return items, nil
		}
	}
}

func (s *TemplateSourceService) Save(ctx context.Context, principal domainidentity.Principal, id string, input domaindocument.SourceInput) (domaindocument.Source, error) {
	action := "create"
	if id != "" {
		action = "update"
	}
	if err := s.authorize(ctx, principal, action); err != nil {
		return domaindocument.Source{}, err
	}
	if id != "" {
		if _, err := s.Get(ctx, principal, id); err != nil {
			return domaindocument.Source{}, err
		}
	}
	item, err := validateTemplateSourceInput(id, input)
	if err != nil {
		return item, err
	}
	repository, err := s.documents.sources.GetRepository(ctx, principal, item.RepositoryID)
	if err != nil {
		return item, err
	}
	if err := validateTemplateRepository(repository); err != nil {
		return item, err
	}
	item, err = s.repo.SaveTemplateSource(ctx, item, input.ExpectedGeneration)
	if err == nil {
		s.record(ctx, principal, "save", item.ID, "saved Git source configuration without synchronization or execution")
	}
	return item, err
}

func (s *TemplateSourceService) Objects(ctx context.Context, principal domainidentity.Principal, id string, offset, limit int) ([]domaindocument.Association, error) {
	if _, err := s.Get(ctx, principal, id); err != nil {
		return nil, err
	}
	if !validSourcePagination(offset, limit) {
		return nil, apperrors.ErrInvalidArgument
	}
	objects, err := s.repo.ListTemplateSourceObjects(ctx, id)
	if err != nil {
		return nil, err
	}
	items := []domaindocument.Association{}
	for _, object := range objects {
		if _, _, _, err := s.documents.readDocument(ctx, principal, string(object.Kind), object.ObjectID, 0); err != nil {
			if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
				continue
			}
			return nil, err
		}
		if offset > 0 {
			offset--
			continue
		}
		items = append(items, object.Association)
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func (s *TemplateSourceService) SourceInfo(ctx context.Context, principal domainidentity.Principal, kind, id string, version int64) (domaindocument.SourceInfo, error) {
	if version < 0 {
		return domaindocument.SourceInfo{}, apperrors.ErrInvalidArgument
	}
	if _, _, _, err := s.documents.readDocument(ctx, principal, kind, id, version); err != nil {
		return domaindocument.SourceInfo{}, err
	}
	info, err := s.repo.GetDocumentSource(ctx, kind, id, version)
	if err != nil {
		return info, err
	}
	repositoryID := ""
	if info.Provenance != nil {
		repositoryID = info.Provenance.RepositoryID
	} else if info.Association != nil {
		source, sourceErr := s.repo.GetTemplateSource(ctx, info.Association.SourceID)
		if sourceErr != nil && !errors.Is(sourceErr, apperrors.ErrNotFound) {
			return info, sourceErr
		}
		repositoryID = source.RepositoryID
	}
	if repositoryID != "" {
		repository, readErr := s.documents.sources.GetRepository(ctx, principal, repositoryID)
		if readErr == nil {
			info.Repository = &contractsapi.DeliveryDocumentRepository{ID: repository.ID, Name: repository.Name, URL: repository.URL}
		} else if !errors.Is(readErr, apperrors.ErrAccessDenied) && !errors.Is(readErr, apperrors.ErrNotFound) {
			return info, readErr
		}
	}
	return info, nil
}

func (s *TemplateSourceService) Remove(ctx context.Context, principal domainidentity.Principal, id, kind, objectID string, input domaindocument.SourceRemoveInput) error {
	action := "delete"
	if objectID != "" {
		action = "update"
	}
	if err := s.authorize(ctx, principal, action); err != nil {
		return err
	}
	if _, err := s.Get(ctx, principal, id); err != nil {
		return err
	}
	if input.Disposition != "keep" && input.Disposition != "deprecate" {
		return apperrors.ErrInvalidArgument
	}
	objects, err := s.repo.ListTemplateSourceObjects(ctx, id)
	if err != nil {
		return err
	}
	for _, object := range objects {
		if objectID != "" && (object.ObjectID != objectID || string(object.Kind) != kind) {
			continue
		}
		if err := s.authorizeSourceRemoval(ctx, principal, object, string(input.Disposition)); err != nil {
			return err
		}
	}
	err = s.repo.RemoveTemplateSource(ctx, id, input.ExpectedGeneration, kind, objectID, string(input.Disposition))
	if err == nil {
		s.record(ctx, principal, "detach", id, "removed active source association; existing versions and execution history retained")
	}
	return err
}

func (s *TemplateSourceService) authorizeSourceRemoval(ctx context.Context, principal domainidentity.Principal, object domaindocument.StoredAssociation, disposition string) error {
	if object.Kind != "Workflow" {
		if err := s.documents.authorizeDocumentWrite(ctx, principal, string(object.Kind), object.ObjectID); err != nil {
			return err
		}
		if disposition == "deprecate" {
			permission := map[string]string{"BuildTemplate": "delivery.build-templates.delete", "WorkflowTemplate": "delivery.workflow-templates.delete", "DeploymentTemplate": "delivery.deployment-templates.delete"}[string(object.Kind)]
			return s.documents.catalog.authorize(ctx, principal, permission)
		}
		return nil
	}
	if disposition != "keep" {
		return apperrors.ErrInvalidArgument
	}
	// Workflow permissions include the current application/environment references.
	document, revision, _, err := s.documents.readDocument(ctx, principal, "Workflow", object.ObjectID, 0)
	if err != nil {
		return err
	}
	_, err = s.documents.prepareDocument(ctx, principal, domaindocument.Candidate{Path: object.Path, TargetID: object.ObjectID, Document: document, ExpectedRevision: revision})
	return err
}

func (s *TemplateSourceService) record(ctx context.Context, principal domainidentity.Principal, action, id, summary string) {
	s.documents.catalog.recordWriteLogs(ctx, principal, "delivery.template-source."+action, "DeliveryTemplateSource", id, id, summary)
}

func validSourcePagination(offset, limit int) bool { return offset >= 0 && limit >= 1 && limit <= 200 }
