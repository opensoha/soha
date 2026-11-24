package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	plugincontracts "github.com/opensoha/soha/contracts/plugins"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const marketplaceRepositoriesVersion = "marketplace.opensoha.io/repositories/v1"

var errMarketplaceRepositoryPreviewRequired = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_repository_preview_required",
	"Preview the repository and confirm the installation.", "请预览仓库后确认安装。")

var errMarketplaceRepositoryMismatch = apperrors.NewBusiness(apperrors.ErrInvalidArgument, "plugin_marketplace_repository_mismatch",
	"The repository does not match this marketplace registration.", "仓库与市场登记不匹配。")

var marketplaceRepositoriesSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileMarketplaceCatalogSchema("https://contracts.opensoha.dev/plugins/marketplace-repositories.schema.json", plugincontracts.MarketplaceRepositoriesSchema())
})

type marketplaceRepositoryRegistration struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Publisher     string   `json:"publisher"`
	Type          string   `json:"type"`
	RepositoryURL string   `json:"repositoryUrl"`
	Summary       string   `json:"summary"`
	PublisherKeys []string `json:"publisherKeys"`
}

func (p *RemoteMarketplaceProvider) decodeMarketplaceRepositories(raw []byte) ([]domainplugin.MarketplacePlugin, error) {
	schema, err := marketplaceRepositoriesSchema()
	if err != nil {
		return nil, err
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil || schema.Validate(document) != nil {
		return nil, fmt.Errorf("%w: invalid marketplace repositories schema", apperrors.ErrInvalidArgument)
	}
	var catalog struct {
		Repositories []marketplaceRepositoryRegistration `json:"repositories"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("%w: decode marketplace repositories", apperrors.ErrInvalidArgument)
	}
	items := make([]domainplugin.MarketplacePlugin, 0, len(catalog.Repositories))
	ids, repositories := make(map[string]bool), make(map[string]bool)
	for _, entry := range catalog.Repositories {
		if ids[entry.ID] || repositories[entry.RepositoryURL] {
			return nil, fmt.Errorf("%w: duplicate marketplace repository or plugin identity", apperrors.ErrInvalidArgument)
		}
		ids[entry.ID], repositories[entry.RepositoryURL] = true, true
		var keys []string
		if p.official {
			keys = entry.PublisherKeys
		}
		items = append(items, domainplugin.MarketplacePlugin{
			ID: entry.ID, Name: entry.Name, Publisher: entry.Publisher, Type: entry.Type, Summary: entry.Summary,
			Manifest: domainplugin.PluginManifest{
				ID: entry.ID, Name: entry.Name, Publisher: entry.Publisher, Type: entry.Type,
			},
			Source: "marketplace:" + entry.ID, SourceID: p.sourceID, SourceURL: p.catalogURL,
			RepositoryURL: entry.RepositoryURL, PublisherKeys: keys,
			SourceInfo: &domainplugin.PluginSourceInfo{
				Kind: domainplugin.SourceKindMarketplace, CanonicalSource: p.catalogURL,
				SourceID: p.sourceID, RepositoryURL: entry.RepositoryURL, Official: p.official,
			},
		})
	}
	return items, nil
}

func (s *Service) marketplaceRepositoryRegistration(ctx context.Context, sourceID, catalogURL, pluginID, repositoryURL string) (domainplugin.MarketplacePlugin, error) {
	provider, err := s.providerFor(catalogURL)
	if err != nil {
		return domainplugin.MarketplacePlugin{}, repositorySourceConflict()
	}
	item, err := provider.Get(ctx, domainplugin.PluginVersionRef{SourceID: sourceID, PluginID: pluginID})
	if err != nil {
		return domainplugin.MarketplacePlugin{}, err
	}
	if !matchesMarketplaceRepositoryRegistration(item, sourceID, catalogURL, pluginID, repositoryURL) {
		return domainplugin.MarketplacePlugin{}, repositorySourceConflict()
	}
	return item, nil
}

func matchesMarketplaceRepositoryRegistration(item domainplugin.MarketplacePlugin, sourceID, catalogURL, pluginID, repositoryURL string) bool {
	return item.ID == pluginID && item.RepositoryURL != "" && item.RepositoryURL == repositoryURL &&
		item.SourceID == sourceID && item.SourceURL != "" && (catalogURL == "" || item.SourceURL == catalogURL) &&
		item.SourceInfo != nil && item.SourceInfo.Kind == domainplugin.SourceKindMarketplace &&
		item.SourceInfo.SourceID == sourceID && item.SourceInfo.CanonicalSource == item.SourceURL && item.SourceInfo.RepositoryURL == repositoryURL
}
