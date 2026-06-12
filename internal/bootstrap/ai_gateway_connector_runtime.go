package bootstrap

import (
	"context"
	"fmt"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func registerAIGatewayConnectorRuntimes(ctx context.Context, service *appaigateway.Service, cfg cfgpkg.AIGatewayConfig) error {
	if service == nil {
		return nil
	}
	runtimes := cfg.ConnectorRuntimeConfigs()
	if len(runtimes) == 0 {
		return nil
	}
	providers := make([]appaigateway.CapabilityProvider, 0, len(runtimes))
	for _, runtime := range runtimes {
		provider, err := appaigateway.DiscoverConnectorRuntime(
			ctx,
			runtime.Endpoint,
			nil,
			appaigateway.WithConnectorRuntimeToken(runtime.Token),
			appaigateway.WithConnectorRuntimePluginID(runtime.PluginID),
			appaigateway.WithConnectorRuntimeConnectorID(runtime.ConnectorID),
		)
		if err != nil {
			return fmt.Errorf("discover AI Gateway connector runtime %q: %w", runtime.Endpoint, err)
		}
		providers = append(providers, provider)
	}
	service.AddCapabilityProviders(providers...)
	return nil
}
