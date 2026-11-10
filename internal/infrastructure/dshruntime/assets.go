package dshruntime

import (
	"context"
	"net/http"
	"slices"

	appcopilot "github.com/opensoha/soha/internal/application/copilot"
	domain "github.com/opensoha/soha/internal/domain/copilot"
)

var _ appcopilot.DSHRuntimeAssets = (*Adapter)(nil)

type runtimeAssetList struct {
	Items []domain.RuntimeAssetDescriptor `json:"items"`
}

type runtimeMCPProbe domain.RuntimeMCPProbe

func (a *Adapter) ListRuntimeAssets(ctx context.Context) ([]domain.RuntimeAssetDescriptor, error) {
	var result runtimeAssetList
	if err := a.client.call(ctx, http.MethodGet, "/v1/assets", nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if len(result.Items) > 514 {
		return nil, ErrProtocol
	}
	seen := make(map[domain.RuntimeAssetRef]bool, len(result.Items))
	for _, item := range result.Items {
		if !validRuntimeAssetDescriptor(item) || seen[item.Ref()] {
			return nil, ErrProtocol
		}
		seen[item.Ref()] = true
	}
	return result.Items, nil
}

func validRuntimeAssetDescriptor(asset domain.RuntimeAssetDescriptor) bool {
	if !assetIDPattern.MatchString(asset.ID) || !assetVersionPattern.MatchString(asset.Version) {
		return false
	}
	switch asset.Kind {
	case "mcp":
		return (asset.Transport == "" || asset.Transport == "http" || asset.Transport == "stdio") &&
			(asset.ExecutionPosition == "" || asset.Transport == "stdio" && assetIDPattern.MatchString(asset.ExecutionPosition))
	case "skill", "hook":
		return asset.Transport == "" && asset.ExecutionPosition == ""
	default:
		return false
	}
}

func (a *Adapter) ProbeRuntimeMCP(ctx context.Context, id, version, scope string) (domain.RuntimeMCPProbe, error) {
	var result runtimeMCPProbe
	if !assetIDPattern.MatchString(id) || !assetVersionPattern.MatchString(version) || (scope != domain.RuntimeScopeChat && scope != domain.RuntimeScopeHeya) {
		return domain.RuntimeMCPProbe{}, ErrInput
	}
	input := struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Scope   string `json:"scope"`
	}{ID: id, Version: version, Scope: scope}
	if err := a.client.call(ctx, http.MethodPost, "/v1/assets/mcp/probe", input, &result, http.StatusOK); err != nil {
		return domain.RuntimeMCPProbe{}, err
	}
	if !validRuntimeMCPProbe(result) {
		return domain.RuntimeMCPProbe{}, ErrProtocol
	}
	return domain.RuntimeMCPProbe(result), nil
}

func validRuntimeMCPProbe(probe runtimeMCPProbe) bool {
	switch probe.Status {
	case domain.RuntimeMCPProbeReady:
		return len(probe.Capabilities) <= 2 &&
			(len(probe.Capabilities) < 2 || probe.Capabilities[0] != probe.Capabilities[1]) &&
			!slices.ContainsFunc(probe.Capabilities, func(capability string) bool {
				return capability != "tools" && capability != "resources"
			})
	case domain.RuntimeMCPProbeUnavailable, domain.RuntimeMCPProbeCleanupUnknown:
		return len(probe.Capabilities) == 0
	default:
		return false
	}
}
