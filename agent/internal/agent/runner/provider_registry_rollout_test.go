package runner

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

type conformanceProbeFunc func(context.Context, AgentProviderDefinition) error

func (fn conformanceProbeFunc) Check(ctx context.Context, provider AgentProviderDefinition) error {
	return fn(ctx, provider)
}

func TestAgentFleetTargetMatchesNormalizedIdentity(t *testing.T) {
	target := AgentFleetTarget{
		Environments: []string{" Production "}, Platforms: []string{runtime.GOOS},
		Architectures: []string{runtime.GOARCH}, Labels: map[string]string{"Pool": "GPU"},
	}
	identity := AgentFleetIdentity{
		Environment: "production", Platform: strings.ToUpper(runtime.GOOS),
		Architecture: runtime.GOARCH, Labels: map[string]string{"pool": "gpu", "zone": "a"},
	}
	if !target.Matches(identity) {
		t.Fatal("Matches() = false, want true")
	}
	identity.Labels["pool"] = "cpu"
	if target.Matches(identity) {
		t.Fatal("Matches() = true for a mismatched required label")
	}
}

func TestApplyDesiredSkipsNonTargetedSnapshot(t *testing.T) {
	registry, err := NewDynamicAgentProviderRegistry(DefaultAgentProviderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	next := nextRegistry(t, registry.Snapshot(), "codex-v2")
	next.FleetTarget.Environments = []string{"production"}
	next.Digest, err = agentProviderRegistryDigest(next)
	if err != nil {
		t.Fatal(err)
	}
	result := registry.ApplyDesired(t.Context(), next, AgentFleetIdentity{Environment: "staging"}, nil, time.Second, time.Now())
	if !result.Accepted || result.Targeted || result.RolloutState != "skipped_not_targeted" {
		t.Fatalf("ApplyDesired() = %#v", result)
	}
	if got := registry.Snapshot().Revision; got != 1 {
		t.Fatalf("active revision = %d, want 1", got)
	}
}

func TestApplyDesiredConformanceFailureKeepsLastKnownGood(t *testing.T) {
	registry, err := NewDynamicAgentProviderRegistry(DefaultAgentProviderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	next := nextRegistry(t, registry.Snapshot(), "codex-v2")
	probe := conformanceProbeFunc(func(context.Context, AgentProviderDefinition) error {
		return errors.New("secret stderr must not be returned")
	})
	result := registry.ApplyDesired(t.Context(), next, AgentFleetIdentity{}, probe, time.Second, time.Now())
	if result.Accepted || result.RolloutState != "conformance_failed" || result.LKGRevision != 1 {
		t.Fatalf("ApplyDesired() = %#v", result)
	}
	if strings.Contains(result.Reason, "secret") || len(result.ConformanceChecks) != 1 || result.ConformanceChecks[0].Reason != "conformance check failed" {
		t.Fatalf("conformance result leaked probe details: %#v", result)
	}
}

func TestApplyDesiredConformanceHonorsCancellation(t *testing.T) {
	registry, err := NewDynamicAgentProviderRegistry(DefaultAgentProviderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	next := nextRegistry(t, registry.Snapshot(), "codex-v2")
	probe := conformanceProbeFunc(func(ctx context.Context, _ AgentProviderDefinition) error {
		<-ctx.Done()
		return ctx.Err()
	})
	started := time.Now()
	result := registry.ApplyDesired(t.Context(), next, AgentFleetIdentity{}, probe, 20*time.Millisecond, time.Now())
	if result.Accepted || time.Since(started) > time.Second {
		t.Fatalf("bounded conformance result = %#v, duration = %v", result, time.Since(started))
	}
}

func TestDynamicAgentProviderRegistryRollbackUsesPreviousValidatedSnapshot(t *testing.T) {
	registry, err := NewDynamicAgentProviderRegistry(DefaultAgentProviderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	next := nextRegistry(t, registry.Snapshot(), "codex-v2")
	result := registry.ApplyDesired(t.Context(), next, AgentFleetIdentity{}, conformanceProbeFunc(func(context.Context, AgentProviderDefinition) error { return nil }), time.Second, time.Now())
	if !result.Accepted || result.ActiveRevision != 2 || result.PreviousRevision != 1 {
		t.Fatalf("ApplyDesired() = %#v", result)
	}
	rollback := registry.Rollback(time.Now())
	if !rollback.Accepted || !rollback.RolledBack || rollback.RolloutState != "rolled_back" || rollback.ActiveRevision != 1 || rollback.PreviousRevision != 2 {
		t.Fatalf("Rollback() = %#v", rollback)
	}
	if got := registry.Snapshot().Providers[0].ProviderVersion; got != "builtin-v1" {
		t.Fatalf("rolled back provider version = %q", got)
	}
}

func TestApplyDesiredFailsClosedWithoutConformanceProbe(t *testing.T) {
	registry, err := NewDynamicAgentProviderRegistry(DefaultAgentProviderRegistry())
	if err != nil {
		t.Fatal(err)
	}
	next := nextRegistry(t, registry.Snapshot(), "codex-v2")
	result := registry.ApplyDesired(t.Context(), next, AgentFleetIdentity{}, nil, time.Second, time.Now())
	if result.Accepted || result.RolloutState != "conformance_failed" || len(result.ConformanceChecks) != 1 {
		t.Fatalf("ApplyDesired() = %#v", result)
	}
	if result.ConformanceChecks[0].Reason != "conformance probe unavailable" {
		t.Fatalf("conformance checks = %#v", result.ConformanceChecks)
	}
}

func nextRegistry(t *testing.T, current AgentProviderRegistry, providerVersion string) AgentProviderRegistry {
	t.Helper()
	next := cloneAgentProviderRegistry(current)
	next.Revision++
	next.Providers[0].ProviderVersion = providerVersion
	var err error
	next.Digest, err = agentProviderRegistryDigest(next)
	if err != nil {
		t.Fatalf("agentProviderRegistryDigest() error = %v", err)
	}
	return next
}
