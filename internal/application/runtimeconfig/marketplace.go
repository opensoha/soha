package runtimeconfig

import (
	"fmt"
	"slices"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appplugin "github.com/opensoha/soha/internal/application/plugin"
)

func marketplaceThirdPartyURLsDefinition() Definition {
	return Definition{
		Key: KeyMarketplaceThirdPartyURLs, Category: "连接", Label: "第三方插件市场目录",
		Description: "添加公开 HTTPS 目录地址。移除目录后，已安装插件保留。",
		ValueType:   sohaapi.RuntimeConfigValueTypeStringList, ApplyMode: sohaapi.RuntimeConfigApplyModeReconfigure,
		DefaultValue: []string{}, BaselineValue: []string{}, Editable: true,
		normalize: func(value any) (any, error) {
			rawURLs, ok := value.([]string)
			if !ok {
				return nil, fmt.Errorf("must be a string list")
			}
			sources, err := appplugin.ManagedMarketplaceSources(rawURLs)
			if err != nil {
				return nil, err
			}
			urls := make([]string, 0, len(sources))
			for _, source := range sources {
				urls = append(urls, source.URL)
			}
			return urls, nil
		},
	}
}

// ManagedMarketplaceSources validates the saved third-party URLs against the current deployment and main source.
func (s Snapshot) ManagedMarketplaceSources() ([]appplugin.MarketplaceSource, error) {
	raw, ok := s.Value(KeyMarketplaceThirdPartyURLs)
	if !ok {
		return nil, nil
	}
	value, err := normalizeValue(sohaapi.RuntimeConfigValueTypeStringList, raw)
	if err != nil {
		return nil, err
	}
	urls, ok := value.([]string)
	if !ok {
		return nil, fmt.Errorf("must be a string list")
	}
	mainURL, _ := s.Definition(KeyMarketplaceURL)
	mainID, _ := s.Definition(KeyMarketplaceSourceID)
	baselineURL, urlOK := mainURL.BaselineValue.(string)
	baselineID, idOK := mainID.BaselineValue.(string)
	if !urlOK || !idOK {
		return nil, fmt.Errorf("marketplace baseline URL and source ID must be strings")
	}
	effectiveURL := s.String(KeyMarketplaceURL, DefaultMarketplaceURL)
	effectiveID := s.String(KeyMarketplaceSourceID, DefaultMarketplaceSourceID)
	declared := append(slices.Clone(s.registry.marketplaceSources),
		appplugin.MarketplaceSource{ID: DefaultMarketplaceSourceID, URL: DefaultMarketplaceURL},
		appplugin.MarketplaceSource{ID: baselineID, URL: baselineURL},
		appplugin.MarketplaceSource{ID: effectiveID, URL: effectiveURL},
	)
	if effectiveURL == DefaultMarketplaceURL && effectiveID == DefaultMarketplaceSourceID {
		declared = append(declared,
			appplugin.MarketplaceSource{URL: "https://marketplace.opensoha.com/marketplace/index.json"},
			appplugin.MarketplaceSource{URL: "https://opensoha.github.io/soha-marketplace/marketplace/index.json"},
		)
	}
	return appplugin.ManagedMarketplaceSources(urls, declared...)
}

func isMarketplaceConfigKey(key string) bool {
	return key == KeyMarketplaceURL || key == KeyMarketplaceSourceID || key == KeyMarketplaceThirdPartyURLs
}

func (s Snapshot) validateMarketplaceChanges(changes []sohaapi.RuntimeConfigValidatedChange) error {
	var overrides map[string]any
	for _, change := range changes {
		if !isMarketplaceConfigKey(change.Key) {
			continue
		}
		if overrides == nil {
			overrides = cloneValues(s.Overrides)
		}
		overrides[change.Key] = change.ProposedValue
	}
	if overrides == nil {
		return nil
	}
	s.Overrides = overrides
	_, err := s.ManagedMarketplaceSources()
	return err
}
