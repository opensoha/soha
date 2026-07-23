package systemintegration

import (
	"context"
	"fmt"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListSourceConnections(ctx context.Context, principal domainidentity.Principal) ([]sohaapi.SourceConnection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, domain.Filter{Category: domain.CategorySourceControl})
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.SourceConnection, 0, len(items))
	defaultAssigned := false
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		isDefault := !defaultAssigned
		defaultAssigned = true
		result = append(result, sourceConnection(item, isDefault))
	}
	return result, nil
}

func (s *Service) GetSourceConnection(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SourceConnection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return sohaapi.SourceConnection{}, err
	}
	item, _, err := s.sourceAdapter(ctx, strings.TrimSpace(id), true)
	if err != nil {
		return sohaapi.SourceConnection{}, err
	}
	defaultID, _ := s.defaultSourceConnectionID(ctx, "")
	return sourceConnection(item, item.ID == defaultID), nil
}

func (s *Service) ListSourceRepositories(ctx context.Context, principal domainidentity.Principal, id, search, cursor string, limit int) ([]sohaapi.SourceRepository, string, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, "", err
	}
	_, adapter, err := s.sourceAdapter(ctx, strings.TrimSpace(id), true)
	if err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	return adapter.ListRepositories(ctx, strings.TrimSpace(search), strings.TrimSpace(cursor), limit)
}

func (s *Service) ListSourceBranches(ctx context.Context, principal domainidentity.Principal, id, repositoryID string) ([]sohaapi.SourceBranch, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, err
	}
	_, adapter, err := s.sourceAdapter(ctx, strings.TrimSpace(id), true)
	if err != nil {
		return nil, err
	}
	return adapter.ListRepositoryBranches(ctx, strings.TrimSpace(repositoryID), "", 50)
}

func (s *Service) ListSourceTags(ctx context.Context, principal domainidentity.Principal, id, repositoryID string) ([]sohaapi.SourceTag, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, err
	}
	_, adapter, err := s.sourceAdapter(ctx, strings.TrimSpace(id), true)
	if err != nil {
		return nil, err
	}
	return adapter.ListRepositoryTags(ctx, strings.TrimSpace(repositoryID), "", 50)
}

func (s *Service) GetSourceFile(ctx context.Context, principal domainidentity.Principal, id, repositoryID, ref, path string) (sohaapi.SourceFile, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return sohaapi.SourceFile{}, err
	}
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(path) == "" {
		return sohaapi.SourceFile{}, fmt.Errorf("%w: ref and path are required", apperrors.ErrInvalidArgument)
	}
	_, adapter, err := s.sourceAdapter(ctx, strings.TrimSpace(id), true)
	if err != nil {
		return sohaapi.SourceFile{}, err
	}
	return adapter.GetRepositoryFile(ctx, strings.TrimSpace(repositoryID), strings.TrimSpace(ref), strings.TrimSpace(path))
}

// Legacy methods preserve the old delivery-facing GitLab routes while callers migrate.
type legacyCommitLister interface {
	ListCommits(context.Context, string, string, int, int) (domainapp.GitCommitPage, error)
}

func (s *Service) ListProjects(ctx context.Context, search string, limit int) ([]domainapp.GitRepository, error) {
	id, err := s.defaultSourceConnectionID(ctx, domain.ProviderGitLab)
	if err != nil {
		return nil, err
	}
	_, adapter, err := s.sourceAdapter(ctx, id, true)
	if err != nil {
		return nil, err
	}
	items, _, err := adapter.ListRepositories(ctx, search, "", limit)
	if err != nil {
		return nil, err
	}
	result := make([]domainapp.GitRepository, 0, len(items))
	for _, item := range items {
		result = append(result, domainapp.GitRepository{ID: item.ID, Name: item.Name, Path: item.Name, PathWithNamespace: item.FullName, DefaultBranch: item.DefaultBranch, WebURL: item.WebURL})
	}
	return result, nil
}

func (s *Service) ListBranches(ctx context.Context, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	id, err := s.defaultSourceConnectionID(ctx, domain.ProviderGitLab)
	if err != nil {
		return nil, err
	}
	_, adapter, err := s.sourceAdapter(ctx, id, true)
	if err != nil {
		return nil, err
	}
	items, err := adapter.ListRepositoryBranches(ctx, projectID, strings.TrimSpace(search), normalizeReferenceLimit(limit))
	if err != nil {
		return nil, err
	}
	result := make([]domainapp.GitReference, 0, len(items))
	for _, item := range items {
		result = append(result, domainapp.GitReference{Name: item.Name, CommitSHA: item.CommitID})
	}
	return result, nil
}

func (s *Service) ListTags(ctx context.Context, projectID, search string, limit int) ([]domainapp.GitReference, error) {
	id, err := s.defaultSourceConnectionID(ctx, domain.ProviderGitLab)
	if err != nil {
		return nil, err
	}
	_, adapter, err := s.sourceAdapter(ctx, id, true)
	if err != nil {
		return nil, err
	}
	items, err := adapter.ListRepositoryTags(ctx, projectID, strings.TrimSpace(search), normalizeReferenceLimit(limit))
	if err != nil {
		return nil, err
	}
	result := make([]domainapp.GitReference, 0, len(items))
	for _, item := range items {
		result = append(result, domainapp.GitReference{Name: item.Name, CommitSHA: item.CommitID})
	}
	return result, nil
}

func (s *Service) ListCommits(ctx context.Context, projectID, search string, page, limit int) (domainapp.GitCommitPage, error) {
	id, err := s.defaultSourceConnectionID(ctx, domain.ProviderGitLab)
	if err != nil {
		return domainapp.GitCommitPage{}, err
	}
	_, adapter, err := s.sourceAdapter(ctx, id, true)
	if err != nil {
		return domainapp.GitCommitPage{}, err
	}
	commits, ok := adapter.(legacyCommitLister)
	if !ok {
		return domainapp.GitCommitPage{}, fmt.Errorf("%w: source adapter does not support commit listing", apperrors.ErrInvalidArgument)
	}
	return commits.ListCommits(ctx, projectID, strings.TrimSpace(search), page, limit)
}

func (s *Service) defaultSourceConnectionID(ctx context.Context, providerType string) (string, error) {
	items, err := s.repo.List(ctx, domain.Filter{Category: domain.CategorySourceControl, ProviderType: providerType, Enabled: boolPointer(true)})
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf("%w: source connection is not configured", apperrors.ErrInvalidArgument)
	}
	return items[0].ID, nil
}

func sourceConnection(item domain.Integration, isDefault bool) sohaapi.SourceConnection {
	configuration := configurationMap(item.Configuration)
	return sohaapi.SourceConnection{ID: item.ID, IntegrationID: item.ID, ProviderType: item.ProviderType, Name: item.Name, DisplayEndpoint: configuration["base_url"], Enabled: item.Enabled, DefaultConnection: isDefault, Capabilities: []sohaapi.SourceConnectionCapabilities{sohaapi.Repositories, sohaapi.Branches, sohaapi.Tags, sohaapi.Files}}
}

func boolPointer(value bool) *bool { return &value }

func normalizeReferenceLimit(limit int) int {
	if limit < 1 || limit > 100 {
		return 50
	}
	return limit
}
