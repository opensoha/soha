package plugin

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sync/errgroup"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	repositoryUpdateTimeout     = 10 * time.Second
	repositoryUpdateItemTimeout = 4 * time.Second
	maxRepositoryUpdatePlugins  = 24
	maxRepositoryReleaseChecks  = 5
)

var errRepositoryUpdatesUnavailable = apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "plugin_updates_unavailable",
	"The installed plugin repositories are temporarily unavailable. Try again later.", "已安装插件的仓库暂时不可用，请稍后重试。")

type repositoryUpdateStatus uint8

const (
	repositoryUpdateChecked repositoryUpdateStatus = iota + 1
	repositoryUpdateUnavailable
)

type repositoryUpdateCheck struct {
	update domainplugin.PluginUpdate
	status repositoryUpdateStatus
}

type repositoryRefSource interface {
	ListReleases(context.Context, string, int) (domainplugin.PluginRepositoryRefList, error)
	ListBranches(context.Context, string, int) (domainplugin.PluginRepositoryRefList, error)
}

func (s *Service) ListRepositoryRefs(ctx context.Context, actor domainidentity.Principal, input domainplugin.PluginRepositoryRefsRequest) (domainplugin.PluginRepositoryRefList, error) {
	if err := s.authorizeRepository(ctx, actor, appaccess.PermPluginView); err != nil {
		return domainplugin.PluginRepositoryRefList{}, err
	}
	page := 1
	if input.Page != nil {
		page = *input.Page
	}
	if page < 1 || page > 1000 || strings.TrimSpace(input.RepositoryURL) == "" || len(input.RepositoryURL) > 2048 {
		return domainplugin.PluginRepositoryRefList{}, apperrors.ErrInvalidArgument
	}
	source, ok := s.repositories.source.(repositoryRefSource)
	if !ok {
		return domainplugin.PluginRepositoryRefList{}, apperrors.ErrUnsupportedOperation
	}
	var result domainplugin.PluginRepositoryRefList
	var err error
	switch input.Kind {
	case "release":
		result, err = source.ListReleases(ctx, input.RepositoryURL, page)
	case "branch":
		result, err = source.ListBranches(ctx, input.RepositoryURL, page)
	default:
		return result, apperrors.ErrInvalidArgument
	}
	if result.Refs == nil {
		result.Refs = []domainplugin.PluginRepositoryRef{}
	}
	return result, err
}

func (s *Service) ListUpdates(ctx context.Context, actor domainidentity.Principal, pluginIDs ...string) ([]domainplugin.PluginUpdate, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, actor, appaccess.PermPluginView); err != nil {
		return nil, err
	}
	if len(pluginIDs) > maxRepositoryUpdatePlugins || slices.ContainsFunc(pluginIDs, func(id string) bool {
		return strings.TrimSpace(id) == ""
	}) {
		return nil, apperrors.ErrInvalidArgument
	}
	result := []domainplugin.PluginUpdate{}
	if s.repositories == nil {
		return result, nil
	}
	source, ok := s.repositories.source.(repositoryRefSource)
	if !ok {
		return result, nil
	}
	installed, err := s.repo.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	updates, err := s.checkRepositoryUpdates(ctx, source, installed, pluginIDs)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return updates, err
}

func (s *Service) checkRepositoryUpdates(ctx context.Context, source repositoryRefSource, installed []domainplugin.InstalledPlugin, pluginIDs []string) ([]domainplugin.PluginUpdate, error) {
	ctx, cancel := context.WithTimeout(ctx, repositoryUpdateTimeout)
	defer cancel()
	var group errgroup.Group
	group.SetLimit(4)
	catalogs := make(map[string]marketplaceUpdateCatalog)
	var checks [maxRepositoryUpdatePlugins]repositoryUpdateCheck
	index := 0
	for _, item := range installed {
		if index == len(checks) || ctx.Err() != nil {
			break
		}
		if !repositoryInstalled(item) || (len(pluginIDs) > 0 && !slices.Contains(pluginIDs, item.ID)) {
			continue
		}
		current, err := repositorySemver(item.Version)
		if err != nil {
			continue
		}
		slot := index
		index++
		catalog := s.repositoryUpdateCatalog(ctx, catalogs, item.SourceInfo)
		group.Go(func() error {
			updateCtx, cancel := context.WithTimeout(ctx, repositoryUpdateItemTimeout)
			defer cancel()
			checks[slot] = s.checkRepositoryUpdate(updateCtx, source, item, current, catalog)
			return nil
		})
	}
	_ = group.Wait()
	result := make([]domainplugin.PluginUpdate, 0, index)
	var healthy, unavailable bool
	for _, check := range checks[:index] {
		healthy = healthy || check.status == repositoryUpdateChecked
		unavailable = unavailable || check.status == repositoryUpdateUnavailable
		if check.update.PluginID != "" {
			result = append(result, check.update)
		}
	}
	if unavailable && !healthy {
		return nil, errRepositoryUpdatesUnavailable
	}
	return result, nil
}

type marketplaceUpdateCatalog func() ([]domainplugin.MarketplacePlugin, error)

func (s *Service) repositoryUpdateCatalog(ctx context.Context, catalogs map[string]marketplaceUpdateCatalog, source *domainplugin.PluginSourceInfo) marketplaceUpdateCatalog {
	if source.Kind != domainplugin.SourceKindMarketplace {
		return nil
	}
	read := catalogs[source.CanonicalSource]
	if read == nil {
		read = sync.OnceValues(func() ([]domainplugin.MarketplacePlugin, error) {
			provider, err := s.providerFor(source.CanonicalSource)
			if err != nil {
				return nil, nil
			}
			catalogCtx, cancel := context.WithTimeout(ctx, repositoryUpdateItemTimeout)
			defer cancel()
			return provider.List(catalogCtx, domainplugin.MarketplaceFilter{})
		})
		catalogs[source.CanonicalSource] = read
	}
	return read
}

func (s *Service) checkRepositoryUpdate(ctx context.Context, source repositoryRefSource, item domainplugin.InstalledPlugin, current *semver.Version, catalog marketplaceUpdateCatalog) repositoryUpdateCheck {
	binding := item.SourceInfo
	repo := binding.CanonicalSource
	if catalog != nil {
		repo = binding.RepositoryURL
		entries, err := catalog()
		if err != nil {
			return repositoryUpdateCheck{status: repositoryUpdateUnavailable}
		}
		if !slices.ContainsFunc(entries, func(entry domainplugin.MarketplacePlugin) bool {
			return matchesMarketplaceRepositoryRegistration(entry, binding.SourceID, binding.CanonicalSource, item.ID, repo)
		}) {
			return repositoryUpdateCheck{}
		}
	}
	releases, err := source.ListReleases(ctx, repo, 1)
	if err != nil {
		if repositoryDiscoveryUnavailable(ctx, err) {
			return repositoryUpdateCheck{status: repositoryUpdateUnavailable}
		}
		return repositoryUpdateCheck{}
	}
	return s.checkRepositoryReleases(ctx, item, repo, current, releases.Refs)
}

func (s *Service) checkRepositoryReleases(ctx context.Context, item domainplugin.InstalledPlugin, repo string, current *semver.Version, releases []domainplugin.PluginRepositoryRef) repositoryUpdateCheck {
	type releaseCandidate struct {
		ref     domainplugin.PluginRepositoryRef
		version *semver.Version
	}
	candidates := make([]releaseCandidate, 0, len(releases))
	for _, release := range releases {
		version, err := repositorySemver(firstNonEmpty(release.Version, release.Name))
		if err == nil && !release.Prerelease && version.Prerelease() == "" && version.GreaterThan(current) && release.Name != "" {
			candidates = append(candidates, releaseCandidate{ref: release, version: version})
		}
	}
	slices.SortStableFunc(candidates, func(a, b releaseCandidate) int { return b.version.Compare(a.version) })
	status := repositoryUpdateChecked
	if len(candidates) > 0 {
		status = repositoryUpdateUnavailable
	}
	for _, candidate := range candidates[:min(len(candidates), maxRepositoryReleaseChecks)] {
		ref := "refs/tags/" + candidate.ref.Name
		manifest, err := s.readRepositoryUpdateManifest(ctx, repo, ref, item.ID)
		if ctx.Err() != nil || errors.Is(err, domainplugin.ErrRepositoryRateLimited) {
			break
		}
		if repositoryDiscoveryUnavailable(ctx, err) {
			continue
		}
		status = repositoryUpdateChecked
		if err != nil {
			continue
		}
		version, err := repositorySemver(manifest.Version)
		if err == nil && version.String() == candidate.version.String() {
			return repositoryUpdateCheck{status: status, update: domainplugin.PluginUpdate{
				PluginID: item.ID, RepositoryURL: repo, InstalledVersion: item.Version, TargetVersion: manifest.Version,
				Ref: ref, ReleaseURL: candidate.ref.ReleaseURL, MarketplaceSourceID: item.SourceInfo.SourceID,
			}}
		}
	}
	return repositoryUpdateCheck{status: status}
}

func repositoryDiscoveryUnavailable(ctx context.Context, err error) bool {
	return ctx.Err() != nil || (err != nil && !errors.Is(err, apperrors.ErrNotFound) &&
		!errors.Is(err, apperrors.ErrInvalidArgument) && !errors.Is(err, apperrors.ErrConflict))
}

func (s *Service) readRepositoryUpdateManifest(ctx context.Context, repo, ref, pluginID string) (domainplugin.PluginManifest, error) {
	canonical, resolvedRef, commit, err := s.repositories.source.Resolve(ctx, repo, ref)
	if err != nil {
		return domainplugin.PluginManifest{}, err
	}
	if canonical != repo || resolvedRef != ref {
		return domainplugin.PluginManifest{}, repositorySourceConflict()
	}
	resolved, err := s.repositories.fetch(ctx, repositoryPreviewClaims{Repository: canonical, Ref: resolvedRef, Commit: commit, ID: pluginID})
	return resolved.Manifest, err
}

func repositorySemver(value string) (*semver.Version, error) {
	return semver.StrictNewVersion(strings.TrimPrefix(value, "v"))
}
