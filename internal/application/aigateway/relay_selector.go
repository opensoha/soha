package aigateway

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func filterRelayRealtimeSelections(selections []relaySelection, providerKind string) []relaySelection {
	out := make([]relaySelection, 0, len(selections))
	for _, selection := range selections {
		if relayTransformPlanForRoute(selection.route, providerKind).enabled {
			continue
		}
		if !relayProviderUsesOpenAIWireProtocol(selection.upstream.ProviderKind) {
			continue
		}
		out = append(out, selection)
	}
	return out
}

type relaySelection struct {
	route    domainaigateway.LLMModelRoute
	upstream domainaigateway.LLMUpstream
}

type relaySelectionRepository interface {
	ListLLMModelRoutes(context.Context, domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error)
	ListLLMUpstreams(context.Context, domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error)
	GetLLMUpstream(context.Context, string) (domainaigateway.LLMUpstream, error)
}

type relaySelector struct {
	repository relaySelectionRepository
}

func newRelaySelector(repository relaySelectionRepository) *relaySelector {
	return &relaySelector{repository: repository}
}

func (s *Service) relaySelectorComponent() *relaySelector {
	if s.relaySelector != nil {
		return s.relaySelector
	}
	return newRelaySelector(s.llmRelayRepository())
}

func (s *Service) selectRelayUpstreamCandidates(ctx context.Context, providerKind, publicModel string) ([]relaySelection, error) {
	return s.relaySelectorComponent().selectRelayUpstreamCandidates(ctx, providerKind, publicModel)
}

func (s *Service) selectRelayUpstreamCandidatesForPrincipal(ctx context.Context, principal domainidentity.Principal, providerKind, publicModel string) ([]relaySelection, error) {
	return s.relaySelectorComponent().selectRelayUpstreamCandidatesForPrincipal(ctx, principal, providerKind, publicModel)
}

func (s *Service) relayRouteUpstreamTeamPolicyAllows(ctx context.Context, principalTeams []string, route domainaigateway.LLMModelRoute, providerKind string) bool {
	return s.relaySelectorComponent().relayRouteUpstreamTeamPolicyAllows(ctx, principalTeams, route, providerKind)
}

func (s *relaySelector) selectRelayUpstreamCandidates(ctx context.Context, providerKind, publicModel string) ([]relaySelection, error) {
	return s.selectRelayUpstreamCandidatesForPrincipal(ctx, domainidentity.Principal{}, providerKind, publicModel)
}

func (s *relaySelector) selectRelayUpstreamCandidatesForPrincipal(ctx context.Context, principal domainidentity.Principal, providerKind, publicModel string) ([]relaySelection, error) {
	repo := s.repository
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	requestProvider := normalizeRelayProviderKind(providerKind)
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{
		PublicModel: publicModel,
	})
	if err != nil {
		return nil, err
	}
	filteredRoutes := make([]domainaigateway.LLMModelRoute, 0, len(routes))
	for _, route := range routes {
		if !route.Enabled || !relayRouteMatchesRequestProvider(route, requestProvider) {
			continue
		}
		if !relayMetadataTeamPolicyAllows(principal.Teams, route.Metadata) {
			continue
		}
		filteredRoutes = append(filteredRoutes, route)
	}
	sort.SliceStable(filteredRoutes, func(i, j int) bool {
		if filteredRoutes[i].Priority != filteredRoutes[j].Priority {
			return filteredRoutes[i].Priority < filteredRoutes[j].Priority
		}
		return filteredRoutes[i].ID < filteredRoutes[j].ID
	})
	candidates := make([]relaySelection, 0, len(filteredRoutes))
	for _, route := range filteredRoutes {
		plan := relayTransformPlanForRoute(route, requestProvider)
		upstreamProvider := requestProvider
		if plan.enabled {
			upstreamProvider = plan.upstreamProvider
		}
		upstreams, err := s.routeUpstreamCandidates(ctx, route, upstreamProvider)
		if err != nil {
			continue
		}
		for _, upstream := range upstreams {
			if !relayMetadataTeamPolicyAllows(principal.Teams, upstream.Metadata) {
				continue
			}
			if !relayUpstreamSupportsModel(upstream, route.UpstreamModel) {
				continue
			}
			candidates = append(candidates, relaySelection{route: route, upstream: upstream})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no active relay route for model %s", apperrors.ErrNotFound, publicModel)
	}
	return relayWeightedSelectionOrder(candidates), nil
}

func relayRequestedUpstreamID(req LLMRelayHTTPRequest) string {
	return strings.TrimSpace(relayHeaderValue(req.Headers, relayHeaderUpstreamID))
}

func filterRelaySelectionsByUpstream(selections []relaySelection, upstreamID string) []relaySelection {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return selections
	}
	out := make([]relaySelection, 0, len(selections))
	for _, selection := range selections {
		if selection.upstream.ID == upstreamID {
			out = append(out, selection)
		}
	}
	return out
}

func relayMetadataTeamPolicyAllows(principalTeams []string, metadata map[string]any) bool {
	allowed, denied, err := relayMetadataTeamPolicy(metadata)
	if err != nil {
		return false
	}
	return relayTeamPolicyAllows(principalTeams, allowed, denied)
}

func relayMetadataTeamPolicy(metadata map[string]any) ([]string, []string, error) {
	if len(metadata) == 0 {
		return nil, nil, nil
	}
	values := gatewayConditionValues(
		metadata,
		"teamPolicy",
		"team_policy",
		"tenantPolicy",
		"tenant_policy",
		"accessPolicy",
		"access_policy",
	)
	allowed, err := relayMetadataTeamList(values, "allowedTeams", "allowedTeamIds", "teamIds", "teams", "organizations", "orgs")
	if err != nil {
		return nil, nil, err
	}
	denied, err := relayMetadataTeamList(values, "deniedTeams", "deniedTeamIds", "blockedTeams", "blockedTeamIds", "excludedTeams", "excludedTeamIds")
	if err != nil {
		return nil, nil, err
	}
	return allowed, denied, nil
}

func relayMetadataTeamList(values map[string]any, keys ...string) ([]string, error) {
	out := make([]string, 0)
	for _, key := range keys {
		items, err := metadataStringList(values, key, false)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return normalizeStringSlice(out), nil
}

func (s *relaySelector) relayRouteUpstreamTeamPolicyAllows(ctx context.Context, principalTeams []string, route domainaigateway.LLMModelRoute, providerKind string) bool {
	upstreamID := strings.TrimSpace(route.UpstreamID)
	if upstreamID == "" {
		return true
	}
	repo := s.repository
	if repo == nil {
		return true
	}
	upstream, err := repo.GetLLMUpstream(ctx, upstreamID)
	if err != nil {
		return true
	}
	requestProvider := normalizeRelayProviderKind(providerKind)
	plan := relayTransformPlanForRoute(route, requestProvider)
	upstreamProvider := requestProvider
	if plan.enabled {
		upstreamProvider = plan.upstreamProvider
	}
	if !relayUpstreamActiveForProvider(upstream, upstreamProvider) {
		return true
	}
	return relayMetadataTeamPolicyAllows(principalTeams, upstream.Metadata)
}

func relayRouteProviderMatches(routeProvider, providerKind string) bool {
	routeProvider = normalizeRelayProviderKind(routeProvider)
	providerKind = normalizeRelayProviderKind(providerKind)
	if routeProvider == "" {
		return true
	}
	if providerKind == "openai" {
		return routeProvider == "openai" || routeProvider == "openai-compatible"
	}
	return routeProvider == providerKind
}

func relayRouteMatchesRequestProvider(route domainaigateway.LLMModelRoute, requestProvider string) bool {
	plan := relayTransformPlanForRoute(route, requestProvider)
	if !plan.enabled {
		return relayRouteProviderMatches(route.ProviderKind, requestProvider)
	}
	return true
}

func (s *relaySelector) routeUpstreamCandidates(ctx context.Context, route domainaigateway.LLMModelRoute, providerKind string) ([]domainaigateway.LLMUpstream, error) {
	repo := s.repository
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(route.UpstreamID) != "" {
		upstream, err := repo.GetLLMUpstream(ctx, route.UpstreamID)
		if err != nil {
			return nil, err
		}
		if relayUpstreamActiveForProvider(upstream, providerKind) && !relayUpstreamCircuitOpen(upstream, time.Now().UTC()) {
			return []domainaigateway.LLMUpstream{upstream}, nil
		}
		return nil, fmt.Errorf("%w: relay upstream is not active", apperrors.ErrNotFound)
	}
	upstreams, err := repo.ListLLMUpstreams(ctx, domainaigateway.LLMUpstreamFilter{})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(upstreams, func(i, j int) bool {
		if upstreams[i].Priority != upstreams[j].Priority {
			return upstreams[i].Priority < upstreams[j].Priority
		}
		return upstreams[i].ID < upstreams[j].ID
	})
	out := make([]domainaigateway.LLMUpstream, 0, len(upstreams))
	now := time.Now().UTC()
	for _, upstream := range upstreams {
		if relayUpstreamActiveForProvider(upstream, providerKind) && !relayUpstreamCircuitOpen(upstream, now) {
			out = append(out, upstream)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no active relay upstream for provider %s", apperrors.ErrNotFound, providerKind)
	}
	return relayWeightedUpstreamOrder(out), nil
}

func relayUpstreamActiveForProvider(upstream domainaigateway.LLMUpstream, providerKind string) bool {
	if !strings.EqualFold(strings.TrimSpace(upstream.Status), "active") {
		return false
	}
	upstreamProvider := normalizeRelayProviderKind(upstream.ProviderKind)
	if providerKind == "openai" {
		return upstreamProvider == "openai" || upstreamProvider == "openai-compatible"
	}
	return upstreamProvider == providerKind
}

func relayUpstreamSupportsModel(upstream domainaigateway.LLMUpstream, model string) bool {
	if len(upstream.SupportedModels) == 0 {
		return true
	}
	return containsFold(upstream.SupportedModels, model)
}

func relayWeightedSelectionOrder(items []relaySelection) []relaySelection {
	if len(items) <= 1 {
		return append([]relaySelection(nil), items...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].route.Priority != items[j].route.Priority {
			return items[i].route.Priority < items[j].route.Priority
		}
		if items[i].upstream.Priority != items[j].upstream.Priority {
			return items[i].upstream.Priority < items[j].upstream.Priority
		}
		if items[i].route.ID != items[j].route.ID {
			return items[i].route.ID < items[j].route.ID
		}
		return items[i].upstream.ID < items[j].upstream.ID
	})
	out := make([]relaySelection, 0, len(items))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].route.Priority == items[start].route.Priority && items[end].upstream.Priority == items[start].upstream.Priority {
			end++
		}
		out = append(out, relayWeightedSelectionBucket(items[start:end])...)
		start = end
	}
	return out
}

func relayWeightedSelectionBucket(items []relaySelection) []relaySelection {
	remaining := append([]relaySelection(nil), items...)
	out := make([]relaySelection, 0, len(remaining))
	for len(remaining) > 0 {
		index := relayWeightedSelectionIndex(remaining)
		out = append(out, remaining[index])
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return out
}

func relayWeightedSelectionIndex(items []relaySelection) int {
	total := 0
	for _, item := range items {
		total += relaySelectionWeight(item)
	}
	if total <= 0 {
		return 0
	}
	pick := relayRandomIntn(total)
	for index, item := range items {
		weight := relaySelectionWeight(item)
		if pick < weight {
			return index
		}
		pick -= weight
	}
	return len(items) - 1
}

func relaySelectionWeight(item relaySelection) int {
	routeWeight := item.route.Weight
	if routeWeight <= 0 {
		routeWeight = 1
	}
	upstreamWeight := item.upstream.Weight
	if upstreamWeight <= 0 {
		upstreamWeight = 1
	}
	return routeWeight * upstreamWeight
}

func relayWeightedUpstreamOrder(items []domainaigateway.LLMUpstream) []domainaigateway.LLMUpstream {
	if len(items) <= 1 {
		return append([]domainaigateway.LLMUpstream(nil), items...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		return items[i].ID < items[j].ID
	})
	out := make([]domainaigateway.LLMUpstream, 0, len(items))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].Priority == items[start].Priority {
			end++
		}
		out = append(out, relayWeightedUpstreamBucket(items[start:end])...)
		start = end
	}
	return out
}

func relayWeightedUpstreamBucket(items []domainaigateway.LLMUpstream) []domainaigateway.LLMUpstream {
	remaining := append([]domainaigateway.LLMUpstream(nil), items...)
	out := make([]domainaigateway.LLMUpstream, 0, len(remaining))
	for len(remaining) > 0 {
		index := relayWeightedUpstreamIndex(remaining)
		out = append(out, remaining[index])
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return out
}

func relayWeightedUpstreamIndex(items []domainaigateway.LLMUpstream) int {
	total := 0
	for _, item := range items {
		total += relayPositiveWeight(item.Weight)
	}
	if total <= 0 {
		return 0
	}
	pick := relayRandomIntn(total)
	for index, item := range items {
		weight := relayPositiveWeight(item.Weight)
		if pick < weight {
			return index
		}
		pick -= weight
	}
	return len(items) - 1
}

func relayPositiveWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}

func (s relaySelection) upstreamProviderKind() string {
	return normalizeRelayProviderKind(s.upstream.ProviderKind)
}
