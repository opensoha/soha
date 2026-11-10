package manifest

import (
	"context"
	"fmt"
	"slices"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *DeclarativeService) templateSourceFiles(ctx context.Context, p domainidentity.Principal, item domainmanifest.Package, source domaincatalog.DeploymentTemplateSource) ([]domainmanifest.File, error) {
	if source.Git == nil {
		return source.Files, nil
	}
	app, err := s.base.applications.Get(ctx, p, item.ApplicationID)
	if err != nil {
		return nil, err
	}
	return s.readTemplateGitFiles(ctx, app, source)
}

func (s *DeclarativeService) readTemplateGitFiles(ctx context.Context, app domainapp.App, source domaincatalog.DeploymentTemplateSource) ([]domainmanifest.File, error) {
	git := source.Git
	if git == nil || !slices.Contains(app.RepositoryIDs, git.RepositoryID) || s.sources == nil || s.deliveryGit == nil {
		return nil, fmt.Errorf("%w: template Git source must be registered to this application", apperrors.ErrAccessDenied)
	}
	repository, err := s.sources.GetRepository(ctx, git.RepositoryID)
	if err != nil {
		return nil, err
	}
	documents, err := s.deliveryGit.ReadDeliveryDocuments(ctx, repository, domaindocument.Source{RepositoryID: git.RepositoryID, RefType: "commit", RefValue: git.Commit, Path: git.Path, IncludePatterns: []string{"*.yaml", "*.yml", "*.json", "**/*.yaml", "**/*.yml", "**/*.json"}})
	if err != nil {
		return nil, err
	}
	if documents.ResolvedCommit != git.Commit {
		return nil, fmt.Errorf("%w: immutable template Git commit changed", apperrors.ErrConflict)
	}
	files := make([]domainmanifest.File, 0, len(documents.Files))
	for _, file := range documents.Files {
		files = append(files, domainmanifest.File{Path: file.Path, Content: file.Content})
	}
	return files, nil
}

func (s *DeclarativeService) validateTemplateAdoptionBaseline(ctx context.Context, p domainidentity.Principal, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding) error {
	if selection := binding.TemplateSelection; selection != nil {
		if selection.BaseFilesDigest != TemplateFilesDigest(item.Files) {
			return fmt.Errorf("%w: Manifest files were independently edited", apperrors.ErrConflict)
		}
		return nil
	}
	service, err := s.base.applications.GetService(ctx, p, item.ApplicationID, item.ServiceID)
	if err != nil {
		return err
	}
	if service.DeploymentTemplate == nil || service.DeploymentTemplate.Detached {
		return apperrors.ErrConflict
	}
	template, err := s.base.serviceDeploymentTemplate(ctx, p, *service.DeploymentTemplate)
	if err != nil {
		return err
	}
	files, err := s.templateSourceFiles(ctx, p, item, template.Source)
	if err != nil {
		return err
	}
	if TemplateFilesDigest(files) != TemplateFilesDigest(item.Files) {
		return fmt.Errorf("%w: Manifest files are an independent copy and cannot be replaced by propagation", apperrors.ErrConflict)
	}
	return nil
}
