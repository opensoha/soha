package networkaccess

import (
	"cmp"
	"crypto/sha256"
	"math"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

// RankVPNCandidates never changes the authorized candidate set or the access mode.
// Its output is a preference; the connection transaction must recheck live inputs.
func RankVPNCandidates(policy domain.VPNSelectionPolicyConfig, selection, requested, affinity string, candidates []domain.VPNCandidate, now time.Time) ([]string, []domain.VPNCandidateDecision, string) {
	if selection != domain.VPNSelectionAuto && selection != domain.VPNSelectionManual {
		return []string{}, []domain.VPNCandidateDecision{}, "invalid_selection"
	}
	if selection == domain.VPNSelectionManual && !slices.ContainsFunc(candidates, func(candidate domain.VPNCandidate) bool { return candidate.GatewayID == requested }) {
		return []string{}, []domain.VPNCandidateDecision{}, "manual_gateway_not_allowed"
	}
	decisions := make([]domain.VPNCandidateDecision, 0, len(candidates))
	eligible := make([]domain.VPNCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		reason := vpnCandidateExclusion(policy, candidate, now)
		if selection == domain.VPNSelectionManual && candidate.GatewayID != requested && !policy.AllowManualFallback {
			reason = "manual_gateway_not_selected"
		}
		decisions = append(decisions, domain.VPNCandidateDecision{GatewayID: candidate.GatewayID, Name: candidate.Name, ProviderCode: candidate.ProviderCode, Eligible: reason == "", ReasonCode: reason, LatencyMs: candidate.LatencyMs, TimeoutPercent: candidate.TimeoutPercent, MeasuredAt: candidate.MeasuredAt})
		if reason == "" {
			eligible = append(eligible, candidate)
		}
	}
	slices.SortStableFunc(eligible, func(a, b domain.VPNCandidate) int {
		if selection == domain.VPNSelectionManual {
			if a.GatewayID == requested {
				return -1
			}
			if b.GatewayID == requested {
				return 1
			}
		}
		return compareVPNCandidates(policy, affinity, a, b, now)
	})
	ids := make([]string, 0, len(eligible))
	for index, candidate := range eligible {
		ids = append(ids, candidate.GatewayID)
		for d := range decisions {
			if decisions[d].GatewayID == candidate.GatewayID {
				decisions[d].Rank = index + 1
			}
		}
	}
	return ids, decisions, vpnSelectionReason(policy, selection, requested, ids, eligible, now)
}

func vpnSelectionReason(policy domain.VPNSelectionPolicyConfig, selection, requested string, ids []string, eligible []domain.VPNCandidate, now time.Time) string {
	if len(ids) == 0 {
		return "no_eligible_gateway"
	}
	if selection == domain.VPNSelectionManual && ids[0] == requested {
		return "manual_gateway_selected"
	}
	if selection == domain.VPNSelectionManual {
		return "manual_fallback_to_auto"
	}
	if policy.Strategy != domain.VPNStrategyPriority && !freshVPNSample(policy, eligible[0], now) {
		return "measurements_unavailable_priority_fallback"
	}
	return "auto_" + policy.Strategy
}

func vpnCandidateExclusion(policy domain.VPNSelectionPolicyConfig, candidate domain.VPNCandidate, now time.Time) string {
	if !candidate.Available {
		if candidate.ReasonCode != "" {
			return candidate.ReasonCode
		}
		return "gateway_unavailable"
	}
	if policy.ProviderPreference == "require" && !slices.Contains(policy.ProviderOrder, candidate.ProviderCode) {
		return "provider_not_allowed"
	}
	if freshVPNWindow(policy, candidate, now) && candidate.TimeoutPercent != nil && *candidate.TimeoutPercent > float64(policy.MaxTimeoutPercent) {
		return "probe_timeout_limit_exceeded"
	}
	fresh := freshVPNSample(policy, candidate, now)
	if !fresh {
		if policy.MissingMeasurements == "deny" {
			return "fresh_measurements_required"
		}
		return ""
	}
	if *candidate.LatencyMs > float64(policy.MaxLatencyMs) {
		return "latency_limit_exceeded"
	}
	return ""
}

func freshVPNWindow(policy domain.VPNSelectionPolicyConfig, candidate domain.VPNCandidate, now time.Time) bool {
	return candidate.MeasuredAt != nil && !candidate.MeasuredAt.After(now) && !candidate.MeasuredAt.Before(now.Add(-time.Duration(policy.MaxSampleAgeSeconds)*time.Second))
}

func freshVPNSample(policy domain.VPNSelectionPolicyConfig, candidate domain.VPNCandidate, now time.Time) bool {
	return freshVPNWindow(policy, candidate, now) && candidate.SampleCount >= policy.MinSamples && candidate.LatencyMs != nil && !math.IsNaN(*candidate.LatencyMs) && !math.IsInf(*candidate.LatencyMs, 0) && *candidate.LatencyMs >= 0
}

func compareVPNCandidates(policy domain.VPNSelectionPolicyConfig, affinity string, a, b domain.VPNCandidate, now time.Time) int {
	if policy.Strategy == domain.VPNStrategyProvider {
		if order := cmp.Compare(vpnProviderRank(policy.ProviderOrder, a.ProviderCode), vpnProviderRank(policy.ProviderOrder, b.ProviderCode)); order != 0 {
			return order
		}
	}
	if policy.Strategy != domain.VPNStrategyPriority {
		aFresh, bFresh := freshVPNSample(policy, a, now), freshVPNSample(policy, b, now)
		if aFresh != bFresh {
			if aFresh {
				return -1
			}
			return 1
		}
		if aFresh {
			// Millisecond buckets keep comparator ordering transitive while spreading ties.
			if order := cmp.Compare(math.Round(*a.LatencyMs), math.Round(*b.LatencyMs)); order != 0 {
				return order
			}
		}
	}
	if order := cmp.Compare(a.Priority, b.Priority); order != 0 {
		return order
	}
	aHash, bHash := sha256.Sum256([]byte(affinity+"\x00"+a.GatewayID)), sha256.Sum256([]byte(affinity+"\x00"+b.GatewayID))
	return slices.Compare(aHash[:], bHash[:])
}

func vpnProviderRank(order []string, provider string) int {
	rank := slices.Index(order, provider)
	if rank < 0 {
		return len(order)
	}
	return rank
}
