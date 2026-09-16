package networkaccess

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	ingest "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type VPNDashboardStore interface {
	VPNScopes(context.Context, domain.VPNDashboardFilter) ([]domain.VPNScope, error)
	VPNDashboardRecords(context.Context, domain.VPNDashboardQuery, time.Time) ([]domain.VPNDashboardRecord, error)
}
type VPNMetricsReader interface {
	VPNMetrics(context.Context, ingest.VPNMetricsQuery) (ingest.VPNMetrics, error)
	VPNProbe(context.Context, string, string, string) (*networkprotocol.VPNProbeBatch, error)
}

func (s *VPNService) SetVPNMetricsReader(reader VPNMetricsReader) { s.metrics = reader }

func (s *VPNService) dashboardScopes(ctx context.Context, p identity.Principal, filter domain.VPNDashboardFilter) (VPNDashboardStore, []domain.VPNScope, error) {
	if err := s.base.authorize(ctx, p, "network_access.vpn_dashboard.view"); err != nil {
		return nil, nil, err
	}
	store, ok := s.profiles.(VPNDashboardStore)
	if !ok {
		return nil, nil, apperrors.ErrServiceUnavailable
	}
	scopes, err := store.VPNScopes(ctx, filter)
	if err != nil {
		return nil, nil, err
	}
	if len(scopes) > 5000 {
		return nil, nil, invalid("narrow the VPN dashboard profile or site filter")
	}
	allowed := make([]domain.VPNScope, 0, len(scopes))
	for _, scope := range scopes {
		err := s.authorize(ctx, p, "network_access.vpn_dashboard.view", "NetworkVPNProfile", scope.ProfileID, scope.SiteID, scope.NetworkSpaceID)
		if errors.Is(err, apperrors.ErrAccessDenied) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		allowed = append(allowed, scope)
	}
	return store, allowed, nil
}

func (s *VPNService) Dashboard(ctx context.Context, p identity.Principal, filter domain.VPNDashboardFilter) (domain.VPNDashboard, error) {
	now := s.now().UTC()
	if filter.From.IsZero() || !filter.To.After(filter.From) || filter.To.Sub(filter.From) > 7*24*time.Hour || filter.To.After(now.Add(time.Minute)) {
		return domain.VPNDashboard{}, invalid("VPN dashboard window must be within seven days")
	}
	store, scopes, err := s.dashboardScopes(ctx, p, filter)
	if err != nil {
		return domain.VPNDashboard{}, err
	}
	records, err := store.VPNDashboardRecords(ctx, domain.VPNDashboardQuery{VPNDashboardFilter: filter, Scopes: scopes}, now)
	if err != nil {
		return domain.VPNDashboard{}, err
	}
	result := domain.VPNDashboard{AsOf: now, From: filter.From, To: filter.To, Partial: len(records) > 2000, Gateways: []domain.VPNGatewayMetrics{}, Series: []domain.VPNSeriesPoint{}, Sessions: []domain.VPNConnectionView{}, Decisions: []domain.VPNDecision{}}
	records = records[:min(2000, len(records))]
	gateways, err := s.dashboardGateways(ctx, p, scopes, filter, &result)
	if err != nil {
		return result, err
	}
	points := map[time.Time]*domain.VPNSeriesPoint{}
	for _, record := range records {
		addVPNRecord(&result, gateways, points, record, filter)
	}
	if s.metrics != nil {
		metrics, err := s.metrics.VPNMetrics(ctx, vpnMetricBindings(records, filter))
		if err == nil {
			mergeVPNMetrics(&result, gateways, points, metrics)
		}
	}
	for _, item := range gateways {
		result.Gateways = append(result.Gateways, *item)
	}
	for _, item := range points {
		result.Series = append(result.Series, *item)
	}
	slices.SortFunc(result.Gateways, func(a, b domain.VPNGatewayMetrics) int { return strings.Compare(a.GatewayID, b.GatewayID) })
	slices.SortFunc(result.Series, func(a, b domain.VPNSeriesPoint) int { return a.At.Compare(b.At) })
	limit := boundedLimit(filter.Limit)
	result.Sessions = result.Sessions[:min(limit, len(result.Sessions))]
	result.Decisions = result.Decisions[:min(limit, len(result.Decisions))]
	if len(result.Gateways) > 200 {
		result.Partial = true
		result.Gateways = result.Gateways[:200]
	}
	return result, nil
}

func (s *VPNService) dashboardGateways(ctx context.Context, p identity.Principal, scopes []domain.VPNScope, filter domain.VPNDashboardFilter, result *domain.VPNDashboard) (map[string]*domain.VPNGatewayMetrics, error) {
	items := map[string]*domain.VPNGatewayMetrics{}
	seen := map[string]bool{}
	for _, scope := range scopes {
		if seen[scope.ProfileID] {
			continue
		}
		seen[scope.ProfileID] = true
		profile, err := s.profiles.GetVPNProfile(ctx, scope.ProfileID)
		if errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		config := profile.PublishedConfiguration
		if config == nil || !config.Enabled || !slices.Contains(scopes, domain.VPNScope{ProfileID: profile.ID, SiteID: config.SiteID, NetworkSpaceID: config.NetworkSpaceID}) {
			continue
		}
		facts, err := s.connections.VPNGatewayCandidates(ctx, *config, s.now().UTC())
		if err != nil {
			return nil, err
		}
		views := VPNGatewayViews(facts, s.now().UTC(), 2*time.Minute)
		for i, fact := range facts {
			g := fact.Gateway
			if (filter.GatewayID != "" && filter.GatewayID != g.ID) || (filter.ProviderCode != "" && filter.ProviderCode != g.ProviderCode) {
				continue
			}
			if items[g.ID] != nil {
				continue
			}
			item := &domain.VPNGatewayMetrics{GatewayID: g.ID, Name: g.Name, Region: g.Region, ProviderCode: g.ProviderCode, ProviderName: g.ProviderName}
			if err := s.authorize(ctx, p, "network_access.gateways.view", "NetworkGateway", g.ID, g.SiteID, ""); err == nil {
				healthy := views[i].Available
				item.Healthy, item.AcceptNewConnections, item.MaxSessions = &healthy, &g.AcceptNewConnections, &g.MaxSessions
			} else if !errors.Is(err, apperrors.ErrAccessDenied) {
				return nil, err
			}
			items[g.ID] = item
			result.TotalGateways++
			if views[i].Available {
				result.AvailableGateways++
			}
		}
	}
	return items, nil
}

func addVPNRecord(result *domain.VPNDashboard, gateways map[string]*domain.VPNGatewayMetrics, points map[time.Time]*domain.VPNSeriesPoint, record domain.VPNDashboardRecord, filter domain.VPNDashboardFilter) {
	d := record.Decision
	var g *domain.VPNGatewayMetrics
	if d.EffectiveGatewayID != "" {
		g = gateways[d.EffectiveGatewayID]
		if g == nil {
			g = &domain.VPNGatewayMetrics{GatewayID: d.EffectiveGatewayID}
			if record.Connection != nil {
				g.Name, g.ProviderCode = record.Connection.GatewayName, record.Connection.ProviderCode
			}
			gateways[g.GatewayID] = g
			result.TotalGateways++
		}
	}
	if !d.CreatedAt.Before(filter.From) && d.CreatedAt.Before(filter.To) {
		result.Attempts++
		point := vpnSeriesPoint(points, d.CreatedAt)
		point.Attempts++
		if g != nil {
			g.Attempts++
		}
		if record.EstablishedAt != nil {
			result.Successes++
			point.Successes++
			if g != nil {
				g.Successes++
			}
		}
		if strings.Contains(d.ReasonCode, "fallback") || d.ReasonCode == "auto_gateway_retry" {
			result.Fallbacks++
			point.Fallbacks++
			if g != nil {
				g.Fallbacks++
			}
		}
		result.Decisions = append(result.Decisions, d)
	}
	if record.Connection != nil {
		result.Sessions = append(result.Sessions, *record.Connection)
		if record.Connection.State == "connected" {
			result.ActiveSessions++
			if g != nil {
				g.ActiveSessions++
			}
		}
	}
}

func vpnSeriesPoint(points map[time.Time]*domain.VPNSeriesPoint, at time.Time) *domain.VPNSeriesPoint {
	at = at.UTC().Truncate(time.Hour)
	if points[at] == nil {
		points[at] = &domain.VPNSeriesPoint{At: at}
	}
	return points[at]
}

func vpnMetricBindings(records []domain.VPNDashboardRecord, filter domain.VPNDashboardFilter) ingest.VPNMetricsQuery {
	q := ingest.VPNMetricsQuery{From: filter.From, To: filter.To, Sessions: []ingest.VPNSessionBinding{}, Probes: []ingest.VPNProbeBinding{}}
	for _, record := range records {
		if c := record.Connection; c != nil {
			q.Sessions = append(q.Sessions, ingest.VPNSessionBinding{SessionID: c.SessionID, ProfileID: c.ProfileID, GatewayID: c.GatewayID, GatewayRuntimeID: c.GatewayRuntimeID, EndpointRuntimeID: c.RuntimeID})
		}
		ids := []string{}
		for _, candidate := range record.Decision.Candidates {
			if (filter.GatewayID == "" || candidate.GatewayID == filter.GatewayID) && (filter.ProviderCode == "" || candidate.ProviderCode == filter.ProviderCode) {
				ids = append(ids, candidate.GatewayID)
			}
		}
		if len(ids) > 0 {
			q.Probes = append(q.Probes, ingest.VPNProbeBinding{IntentID: record.Decision.ID, ProfileID: record.Decision.ProfileID, EndpointRuntimeID: record.RuntimeID, GatewayIDs: ids})
		}
	}
	return q
}
