package networkaccess

import (
	"context"
	"slices"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func normalizeVPNProfile(config domain.VPNProfileConfig) domain.VPNProfileConfig {
	config.Name, config.SiteID, config.NetworkSpaceID, config.SelectionPolicyID = strings.TrimSpace(config.Name), strings.TrimSpace(config.SiteID), strings.TrimSpace(config.NetworkSpaceID), strings.TrimSpace(config.SelectionPolicyID)
	config.ResourceIDs = normalizedVPNIDs(config.ResourceIDs)
	config.GatewayIDs = normalizedVPNIDs(config.GatewayIDs)
	config.Assignments.UserIDs = normalizedVPNIDs(config.Assignments.UserIDs)
	config.Assignments.TeamIDs = normalizedVPNIDs(config.Assignments.TeamIDs)
	config.Assignments.DeviceIDs = normalizedVPNIDs(config.Assignments.DeviceIDs)
	return config
}

func normalizedVPNIDs(input []string) []string {
	result := append([]string{}, input...)
	for i := range result {
		result[i] = strings.TrimSpace(result[i])
	}
	slices.Sort(result)
	return result
}

func validateVPNIDs(values []string, max int) error {
	if len(values) > max {
		return invalid("too many VPN references")
	}
	seen := make(map[string]bool, len(values))
	for _, id := range values {
		if !runtimeIdentifierPattern.MatchString(id) || seen[id] {
			return invalid("VPN references must be valid and unique")
		}
		seen[id] = true
	}
	return nil
}

func validateVPNProfileShape(config domain.VPNProfileConfig, publishing bool) error {
	if err := requiredText("name", config.Name, 200); err != nil {
		return err
	}
	if err := validateVPNIDs([]string{config.SiteID}, 1); err != nil {
		return err
	}
	if !runtimeIdentifierPattern.MatchString(config.NetworkSpaceID) || !runtimeIdentifierPattern.MatchString(config.SelectionPolicyID) {
		return invalid("VPN target and selection policy are required")
	}
	if !oneOf(config.Mode, domain.ModeExternalVPN, domain.ModeExternalVPNZTNA, domain.ModeInternalZTNA, domain.ModeExternalDirectZTNA) {
		return invalid("VPN access mode is invalid")
	}
	if (config.Mode == domain.ModeExternalVPN) != (len(config.ResourceIDs) == 0) {
		return invalid("resource scope does not match the VPN access mode")
	}
	if len(config.GatewayIDs) == 0 || len(config.GatewayIDs) > 32 {
		return invalid("one through 32 gateways are required")
	}
	for _, list := range [][]string{config.ResourceIDs, config.GatewayIDs, config.Assignments.UserIDs, config.Assignments.TeamIDs, config.Assignments.DeviceIDs} {
		if err := validateVPNIDs(list, 256); err != nil {
			return err
		}
	}
	if publishing && config.Enabled && len(config.Assignments.UserIDs)+len(config.Assignments.TeamIDs)+len(config.Assignments.DeviceIDs) == 0 {
		return invalid("an enabled VPN profile needs an explicit assignment")
	}
	return nil
}

func (s *VPNService) validateProfile(ctx context.Context, config domain.VPNProfileConfig, publishing bool) error {
	if err := validateVPNProfileShape(config, publishing); err != nil {
		return err
	}
	site, err := s.base.store.GetSite(ctx, config.SiteID)
	if err != nil {
		return err
	}
	space, err := s.base.store.GetSpace(ctx, config.NetworkSpaceID)
	if err != nil {
		return err
	}
	if space.SiteID != site.ID {
		return invalid("VPN network space belongs to another site")
	}
	if publishing && config.Enabled && (space.Status != domain.StatusActive || site.Status != domain.StatusActive) {
		return invalid("VPN target is not active")
	}
	for _, id := range config.ResourceIDs {
		resource, err := s.base.store.GetResource(ctx, id)
		if err != nil {
			return err
		}
		if _, ok := domain.WireGuardResourceTarget(resource, space); !ok {
			return invalid("VPN resource is outside the target space or cannot use WireGuard")
		}
	}
	for _, id := range config.GatewayIDs {
		if _, err := s.base.store.GetGateway(ctx, id); err != nil {
			return err
		}
	}
	policy, err := s.policies.GetVPNSelectionPolicy(ctx, config.SelectionPolicyID)
	if err != nil {
		return err
	}
	if publishing && config.Enabled && policy.PublishedRevision == 0 {
		return apperrors.NewBusiness(apperrors.ErrConflict, "vpn_policy_not_published", "Publish the selection policy first.", "请先发布接入点选择策略。")
	}
	return nil
}

func validateVPNSelectionPolicy(config domain.VPNSelectionPolicyConfig) error {
	if err := requiredText("name", config.Name, 200); err != nil {
		return err
	}
	if !oneOf(config.Strategy, domain.VPNStrategyLatency, domain.VPNStrategyProvider, domain.VPNStrategyPriority) || !oneOf(config.ProviderPreference, "prefer", "require") || !oneOf(config.MissingMeasurements, "priority", "deny") {
		return invalid("VPN selection policy strategy is invalid")
	}
	if len(config.ProviderOrder) > 16 || ((config.Strategy == domain.VPNStrategyProvider || config.ProviderPreference == "require") && len(config.ProviderOrder) == 0) {
		return invalid("provider order is required and bounded to 16 entries")
	}
	seen := make(map[string]bool, len(config.ProviderOrder))
	for _, provider := range config.ProviderOrder {
		if provider == "" || !vpnProviderPattern.MatchString(provider) || seen[provider] {
			return invalid("provider order must contain unique supplier codes")
		}
		seen[provider] = true
	}
	return validateVPNQualityLimits(config)
}

func validateVPNQualityLimits(config domain.VPNSelectionPolicyConfig) error {
	if config.MaxLatencyMs < 1 || config.MaxLatencyMs > 10000 || config.MaxTimeoutPercent < 0 || config.MaxTimeoutPercent > 100 {
		return invalid("VPN quality threshold is invalid")
	}
	if config.MaxSampleAgeSeconds < 5 || config.MaxSampleAgeSeconds > 300 || config.MinSamples < 1 || config.MinSamples > 10 {
		return invalid("VPN measurement window is invalid")
	}
	if config.MaxAttempts < 1 || config.MaxAttempts > 5 || config.RetryCooldownSeconds < 5 || config.RetryCooldownSeconds > 300 {
		return invalid("VPN retry limits are invalid")
	}
	return nil
}
