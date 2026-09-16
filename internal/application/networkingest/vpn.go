package networkingest

import (
	"context"
	"regexp"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkingest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var vpnIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type VPNQueryStore interface {
	VPNProbe(context.Context, string, string, string, time.Time) (*networkprotocol.VPNProbeBatch, error)
	VPNMetrics(context.Context, domain.VPNMetricsQuery) (domain.VPNMetrics, error)
}

func (s *Service) VPNProbe(ctx context.Context, identity networkidentity.Identity, producerID, intentID, batchID string) (*networkprotocol.VPNProbeBatch, error) {
	if identity.Scope != networkidentity.ScopeIngest || (identity.Kind != "core" && identity.Kind != "network-control") {
		return nil, apperrors.ErrAccessDenied
	}
	if !vpnIdentifier.MatchString(producerID) || !vpnIdentifier.MatchString(batchID) || (intentID != "" && !vpnIdentifier.MatchString(intentID)) || (identity.Kind == "network-control" && intentID == "") {
		return nil, apperrors.ErrInvalidArgument
	}
	store, ok := s.store.(VPNQueryStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	return store.VPNProbe(ctx, producerID, intentID, batchID, s.now().UTC())
}

func (s *Service) VPNMetrics(ctx context.Context, identity networkidentity.Identity, query domain.VPNMetricsQuery) (domain.VPNMetrics, error) {
	if identity.Scope != networkidentity.ScopeIngest || identity.Kind != "core" {
		return domain.VPNMetrics{}, apperrors.ErrAccessDenied
	}
	if err := validateVPNMetricsQuery(query, s.now().UTC(), s.options.MaxClockSkew); err != nil {
		return domain.VPNMetrics{}, err
	}
	store, ok := s.store.(VPNQueryStore)
	if !ok {
		return domain.VPNMetrics{}, apperrors.ErrServiceUnavailable
	}
	return store.VPNMetrics(ctx, query)
}

func validateVPNMetricsQuery(q domain.VPNMetricsQuery, now time.Time, skew time.Duration) error {
	if q.From.IsZero() || !q.To.After(q.From) || q.To.Sub(q.From) > 7*24*time.Hour || q.To.After(now.Add(skew)) || len(q.Sessions) > 2000 || len(q.Probes) > 2000 {
		return apperrors.ErrInvalidArgument
	}
	seen := make(map[string]bool, len(q.Sessions))
	for _, binding := range q.Sessions {
		if seen[binding.SessionID] {
			return apperrors.ErrInvalidArgument
		}
		seen[binding.SessionID] = true
		for _, value := range []string{binding.SessionID, binding.ProfileID, binding.GatewayID, binding.GatewayRuntimeID, binding.EndpointRuntimeID} {
			if !vpnIdentifier.MatchString(value) {
				return apperrors.ErrInvalidArgument
			}
		}
	}
	seen = make(map[string]bool, len(q.Probes))
	for _, binding := range q.Probes {
		if seen[binding.IntentID] || len(binding.GatewayIDs) > 32 {
			return apperrors.ErrInvalidArgument
		}
		seen[binding.IntentID] = true
		for _, value := range append([]string{binding.IntentID, binding.ProfileID, binding.EndpointRuntimeID}, binding.GatewayIDs...) {
			if !vpnIdentifier.MatchString(value) {
				return apperrors.ErrInvalidArgument
			}
		}
	}
	return nil
}
