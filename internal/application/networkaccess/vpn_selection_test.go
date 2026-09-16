package networkaccess

import (
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

func TestVPNSelectionPolicies(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	latency := func(ms float64) *float64 { return &ms }
	candidates := []domain.VPNCandidate{
		{GatewayID: "a", ProviderCode: "telecom", Available: true, LatencyMs: latency(70), MeasuredAt: &now, SampleCount: 3, Priority: 1},
		{GatewayID: "b", ProviderCode: "unicom", Available: true, LatencyMs: latency(20), MeasuredAt: &now, SampleCount: 3, Priority: 2},
		{GatewayID: "c", ProviderCode: "telecom", Available: true, LatencyMs: latency(35), MeasuredAt: &now, SampleCount: 3, Priority: 3},
	}
	base := domain.VPNSelectionPolicyConfig{Strategy: "latency", ProviderOrder: []string{"telecom", "unicom"}, ProviderPreference: "prefer", MaxLatencyMs: 100, MaxTimeoutPercent: 50, MaxSampleAgeSeconds: 60, MinSamples: 2, MissingMeasurements: "priority"}
	tests := []struct {
		name, selection, requested, want string
		change                           func(*domain.VPNSelectionPolicyConfig, []domain.VPNCandidate)
	}{
		{name: "latency", selection: "auto", want: "b"},
		{name: "provider", selection: "auto", want: "c", change: func(p *domain.VPNSelectionPolicyConfig, _ []domain.VPNCandidate) { p.Strategy = "provider" }},
		{name: "provider failure falls back to other healthy provider", selection: "auto", want: "b", change: func(p *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) {
			p.Strategy = "provider"
			p.MaxLatencyMs = 50
			c[2].Available = false
		}},
		{name: "required provider never widens", selection: "auto", want: "", change: func(p *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) {
			p.ProviderPreference = "require"
			p.ProviderOrder = []string{"telecom"}
			p.MaxLatencyMs = 50
			c[2].Available = false
		}},
		{name: "manual stays on slower selected gateway", selection: "manual", requested: "a", want: "a"},
		{name: "manual failure does not silently switch", selection: "manual", requested: "a", want: "", change: func(_ *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) { c[0].Available = false }},
		{name: "explicit manual fallback", selection: "manual", requested: "a", want: "b", change: func(p *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) {
			p.AllowManualFallback = true
			c[0].Available = false
		}},
		{name: "unknown is not zero latency", selection: "auto", want: "c", change: func(_ *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) { c[1].LatencyMs = nil }},
		{name: "stale samples rejected", selection: "auto", want: "", change: func(p *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) {
			p.MissingMeasurements = "deny"
			old := now.Add(-2 * time.Minute)
			for i := range c {
				c[i].MeasuredAt = &old
			}
		}},
		{name: "all unknown fallback to configured priority", selection: "auto", want: "a", change: func(_ *domain.VPNSelectionPolicyConfig, c []domain.VPNCandidate) {
			for i := range c {
				c[i].LatencyMs = nil
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := base
			c := append([]domain.VPNCandidate(nil), candidates...)
			if tt.change != nil {
				tt.change(&p, c)
			}
			ids, decisions, _ := RankVPNCandidates(p, tt.selection, tt.requested, "device-1", c, now)
			got := ""
			if len(ids) > 0 {
				got = ids[0]
			}
			if got != tt.want {
				t.Fatalf("selected %q, want %q: %+v", got, tt.want, decisions)
			}
		})
	}
}
