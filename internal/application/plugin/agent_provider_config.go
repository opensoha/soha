package plugin

import (
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"maps"
	"net/url"
	"strings"
)

func agentProviderEndpointsReady(manifest domainplugin.PluginManifest, configuration map[string]any) bool {
	if manifest.ExtensionPoints == nil || manifest.ExtensionPoints.AI == nil {
		return true
	}
	for _, contribution := range manifest.ExtensionPoints.AI.AgentProviders {
		runtime, _ := contribution.Metadata["runtime"].(map[string]any)
		key, _ := runtime["endpointConfigKey"].(string)
		if key == "" {
			continue
		}
		endpoint, _ := configuration[key].(string)
		parsed, err := url.Parse(strings.TrimSpace(endpoint))
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
	}
	return true
}

func configuredAgentProviderMetadata(point string, metadata, configuration map[string]any) map[string]any {
	if point != "ai.agentProviders" {
		return metadata
	}
	runtime, ok := metadata["runtime"].(map[string]any)
	if !ok {
		return metadata
	}
	key, _ := runtime["endpointConfigKey"].(string)
	if key == "" {
		return metadata
	}
	result := maps.Clone(metadata)
	runtime = maps.Clone(runtime)
	endpoint, _ := configuration[key].(string)
	runtime["endpoint"] = strings.TrimSpace(endpoint)
	result["runtime"] = runtime
	return result
}
