package plugin

import (
	"strings"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

func metadataString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func (s *Service) constrainLegacyUpgrade(current domainplugin.InstalledPlugin, input *domainplugin.PluginInstallRequest) error {
	id := metadataString(current.Metadata, "sourceId")
	url := metadataString(current.Metadata, "marketplaceUrl")
	if current.SourceInfo != nil {
		if current.SourceInfo.Kind == domainplugin.SourceKindDirectManifest {
			if input.Manifest == nil {
				return repositorySourceConflict()
			}
			return nil
		}
		id, url = current.SourceInfo.SourceID, current.SourceInfo.CanonicalSource
	} else if current.Source == "direct-manifest" {
		if input.Manifest == nil {
			return repositorySourceConflict()
		}
		return nil
	}
	if url == "" {
		return repositorySourceConflict()
	}
	if input.Manifest != nil ||
		(input.MarketplaceURL != "" && strings.TrimSpace(input.MarketplaceURL) != url) {
		return repositorySourceConflict()
	}
	sourceID := id
	if current.SourceInfo == nil && url != "" {
		// Historical catalog metadata could supply an ID different from its configured ID.
		provider, err := s.providerFor(url)
		if err != nil {
			return repositorySourceConflict()
		}
		if remote, ok := provider.(*RemoteMarketplaceProvider); ok {
			sourceID = remote.sourceID
		}
	}
	if input.SourceID != "" && input.SourceID != id && input.SourceID != sourceID {
		return repositorySourceConflict()
	}
	input.SourceID, input.MarketplaceURL = sourceID, url
	return nil
}

func updatePluginMetadata(current, proposed map[string]any) map[string]any {
	next := normalizeMetadata(proposed)
	if next == nil {
		next = make(map[string]any)
	}
	for _, key := range []string{"sourceId", "marketplaceUrl", "manifestChecksum"} {
		if value, ok := current[key]; ok {
			next[key] = value
		} else {
			delete(next, key)
		}
	}
	next["permissionModel"] = "requested-only"
	return next
}
