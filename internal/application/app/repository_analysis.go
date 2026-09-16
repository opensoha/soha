package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// RepositoryAnalyzer consumes a stored, authorized binding, never a client URL or credential.
type RepositoryAnalyzer interface {
	AnalyzeSourceRepository(context.Context, domainapp.SourceRepository, sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error)
	ValidateSourceRepositoryBinding(context.Context, domainapp.SourceRepositoryInput) error
}

func (s *Service) AnalyzeRepository(ctx context.Context, principal domainidentity.Principal, applicationID string, input sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error) {
	application, err := s.Get(ctx, principal, applicationID)
	if err != nil {
		return sohaapi.RepositoryAnalysis{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionView, "Repository", input.RepositoryID, application.Key, application.BusinessLineID, application.Group, application.ID); err != nil {
		return sohaapi.RepositoryAnalysis{}, err
	}
	if s.repositories == nil || s.analyzer == nil {
		return sohaapi.RepositoryAnalysis{}, fmt.Errorf("%w: repository analysis is unavailable", apperrors.ErrInvalidArgument)
	}
	repository, err := s.repositories.GetRepository(ctx, strings.TrimSpace(input.RepositoryID))
	if err != nil {
		return sohaapi.RepositoryAnalysis{}, normalizeRepoError(err)
	}
	if !slices.Contains(repository.ApplicationIDs, application.ID) {
		return sohaapi.RepositoryAnalysis{}, fmt.Errorf("%w: repository is not associated with this application", apperrors.ErrAccessDenied)
	}
	result, err := s.analyzer.AnalyzeSourceRepository(ctx, repository, input)
	if err != nil {
		return sohaapi.RepositoryAnalysis{}, err
	}
	result.ApplicationID = application.ID
	result.RepositoryID = repository.ID
	_ = s.recordAudit(ctx, principal, "", "Repository", repository.Name, "analyze", string(result.Status), "analyzed repository metadata")
	return result, nil
}

func (s *Service) authorizeRepositoryAssociations(ctx context.Context, principal domainidentity.Principal, requested, current []string) error {
	for _, id := range requested {
		id = strings.TrimSpace(id)
		if id == "" || slices.Contains(current, id) {
			continue
		}
		if err := s.authorize(ctx, principal, domainaccess.ActionUpdate, "Repository", id, "", "", "", ""); err != nil {
			return err
		}
		if s.repositories == nil {
			return fmt.Errorf("%w: repository catalog is unavailable", apperrors.ErrInvalidArgument)
		}
		repository, err := s.repositories.GetRepository(ctx, id)
		if err != nil {
			return normalizeRepoError(err)
		}
		// Association with a privileged source connection is an administrator grant.
		if repository.SourceConnectionID != "" {
			if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsSystemIntegrationsManage, "update")); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) validateRepositoryBindingChange(ctx context.Context, principal domainidentity.Principal, input domainapp.SourceRepositoryInput, current domainapp.SourceRepository) error {
	mappingChanged := input.SourceConnectionID != current.SourceConnectionID || input.ProviderRepositoryID != current.ProviderRepositoryID || input.URL != current.URL || input.Provider != current.Provider
	applicationsChanged := !sameRepositoryAssociations(input.ApplicationIDs, current.ApplicationIDs)
	if (input.SourceConnectionID != "" || current.SourceConnectionID != "") && (mappingChanged || applicationsChanged) {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsSystemIntegrationsManage, "update")); err != nil {
			return err
		}
		if input.SourceConnectionID != "" && mappingChanged {
			if s.analyzer == nil {
				return fmt.Errorf("%w: source connection validation is unavailable", apperrors.ErrInvalidArgument)
			}
			if err := s.analyzer.ValidateSourceRepositoryBinding(ctx, input); err != nil {
				return err
			}
		}
	}
	for _, id := range append(slices.Clone(input.ApplicationIDs), current.ApplicationIDs...) {
		if slices.Contains(input.ApplicationIDs, id) == slices.Contains(current.ApplicationIDs, id) {
			continue
		}
		application, err := s.repo.Get(ctx, strings.TrimSpace(id))
		if err != nil {
			return normalizeRepoError(err)
		}
		if err := s.authorize(ctx, principal, domainaccess.ActionUpdate, "Application", application.Name, application.Key, application.BusinessLineID, application.Group, application.ID); err != nil {
			return err
		}
	}
	return nil
}

func sameRepositoryAssociations(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}
