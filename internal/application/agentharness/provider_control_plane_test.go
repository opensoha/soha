package agentharness

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

type mutableExtensionSource struct {
	mu      sync.RWMutex
	records []domainplugin.ExtensionRecord
}

func (s *mutableExtensionSource) List(string) []domainplugin.ExtensionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domainplugin.ExtensionRecord(nil), s.records...)
}

func (s *mutableExtensionSource) replace(records []domainplugin.ExtensionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append([]domainplugin.ExtensionRecord(nil), records...)
}

type providerPermissionStub struct {
	want string
	err  error
}

type providerMemoryStateStore struct {
	mu      sync.Mutex
	state   ProviderPersistedState
	catalog []ProviderCatalog
}

func (s *providerMemoryStateStore) LoadProviderState(_ context.Context, limit int) (ProviderPersistedState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := ProviderPersistedState{Acknowledgements: append([]RegistryAcknowledgement(nil), s.state.Acknowledgements...)}
	if s.state.Catalog != nil {
		catalog := cloneProviderCatalog(*s.state.Catalog)
		state.Catalog = &catalog
	}
	if len(state.Acknowledgements) > limit {
		state.Acknowledgements = state.Acknowledgements[:limit]
	}
	return state, nil
}

func (s *providerMemoryStateStore) SaveProviderCatalog(_ context.Context, catalog ProviderCatalog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := cloneProviderCatalog(catalog)
	s.state.Catalog = &copy
	s.catalog = append(s.catalog, copy)
	return nil
}

func (s *providerMemoryStateStore) SaveRegistryAcknowledgement(_ context.Context, acknowledgement RegistryAcknowledgement, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make(map[string]RegistryAcknowledgement, len(s.state.Acknowledgements)+1)
	for _, item := range s.state.Acknowledgements {
		items[item.RunnerID] = item
	}
	items[acknowledgement.RunnerID] = cloneRegistryAcknowledgement(acknowledgement)
	acks := make([]RegistryAcknowledgement, 0, len(items))
	for _, item := range items {
		acks = append(acks, item)
	}
	for i := 1; i < len(acks); i++ {
		for j := i; j > 0 && (acks[j].ObservedAt.After(acks[j-1].ObservedAt) || (acks[j].ObservedAt.Equal(acks[j-1].ObservedAt) && acks[j].RunnerID < acks[j-1].RunnerID)); j-- {
			acks[j], acks[j-1] = acks[j-1], acks[j]
		}
	}
	if len(acks) > limit {
		acks = acks[:limit]
	}
	s.state.Acknowledgements = acks
	return nil
}

func (s providerPermissionStub) Authorize(_ context.Context, _ domainidentity.Principal, permission string) error {
	if permission != s.want {
		return fmt.Errorf("unexpected permission %q", permission)
	}
	return s.err
}

func providerExtension(version string) domainplugin.ExtensionRecord {
	return domainplugin.ExtensionRecord{
		ID: "hermes", PluginID: "agent.hermes", PluginVersion: version,
		Point: "ai.agentProviders", Scope: "ai", Label: "Hermes", Status: "enabled", Configured: true,
		Metadata: map[string]any{
			"providerVersion": version,
			"adapterProtocol": "opensoha.agent-provider.cli/v1",
			"capabilities":    []any{"root_cause"},
			"runtime": map[string]any{
				"kind": "cli", "command": "hermes", "args": []any{"chat", "-Q"}, "promptArg": "-q",
			},
		},
	}
}

func TestProviderControlPlaneReconcilesPluginChangesAndKeepsMonotonicRevision(t *testing.T) {
	source := &mutableExtensionSource{}
	source.replace([]domainplugin.ExtensionRecord{providerExtension("1.0.0")})
	service, err := NewProviderControlPlane(
		NewProviderReconciler(source),
		providerPermissionStub{want: appaccess.PermAIAgentProvidersView},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 2 || len(first.Providers) != 1 || first.Providers[0].Runtime.PromptArg != "-q" {
		t.Fatalf("first catalog = %#v", first)
	}
	source.replace([]domainplugin.ExtensionRecord{providerExtension("2.0.0")})
	second, err := service.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 3 || second.Providers[0].ProviderVersion != "2.0.0" {
		t.Fatalf("second catalog = %#v", second)
	}
	snapshot, err := service.RegistrySnapshot("runner-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != second.Revision || snapshot.Digest == "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestProviderControlPlaneAcknowledgementsAreIdempotentAndRejectFutureRevision(t *testing.T) {
	source := &mutableExtensionSource{}
	service, err := NewProviderControlPlane(NewProviderReconciler(source), providerPermissionStub{want: appaccess.PermAIAgentProvidersView})
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Now().UTC()
	accepted, err := service.Acknowledge(RegistryAcknowledgement{RunnerID: "runner-1", Revision: 1, ActiveRevision: 1, Accepted: true, ObservedAt: observedAt})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.RunnerID != "runner-1" {
		t.Fatalf("acknowledgement = %#v", accepted)
	}
	stale, err := service.Acknowledge(RegistryAcknowledgement{RunnerID: "runner-1", Revision: 0})
	if err == nil || stale.RunnerID != "" {
		t.Fatalf("invalid acknowledgement result=%#v err=%v", stale, err)
	}
	if _, err := service.Acknowledge(RegistryAcknowledgement{RunnerID: "runner-1", Revision: 2, ActiveRevision: 1}); err == nil {
		t.Fatal("future acknowledgement was accepted")
	}
	status, err := service.RuntimeStatus(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if status.RunnerCount != 1 || len(status.Acknowledgements) != 1 || !status.Acknowledgements[0].ObservedAt.Equal(observedAt) {
		t.Fatalf("runtime status = %#v", status)
	}
}

func TestProviderControlPlaneAcknowledgementNormalizesRunnerStatus(t *testing.T) {
	service, err := NewProviderControlPlane(
		NewProviderReconciler(&mutableExtensionSource{}),
		providerPermissionStub{want: appaccess.PermAIAgentProvidersView},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixedNow := time.Date(2026, 7, 17, 8, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedNow }
	statusObservedAt := time.Date(2026, 7, 17, 16, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	acknowledgement, err := service.Acknowledge(RegistryAcknowledgement{
		RunnerID:     " runner-1 ",
		Revision:     1,
		RolloutState: " active ",
		Reason:       " ready ",
		ConformanceChecks: []ProviderConformanceResult{{
			ProviderID: " hermes ",
			Status:     " passed ",
			Reason:     " compatible ",
		}},
		ProviderStatuses: []RunnerProviderStatus{{
			ProviderID:      " hermes ",
			ProviderVersion: " 1.0.0 ",
			Health:          " healthy ",
			Reason:          " ready ",
			ObservedAt:      statusObservedAt,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if acknowledgement.RunnerID != "runner-1" || acknowledgement.RolloutState != "active" || acknowledgement.Reason != "ready" {
		t.Fatalf("acknowledgement was not normalized: %#v", acknowledgement)
	}
	if !acknowledgement.ObservedAt.Equal(fixedNow) || acknowledgement.ObservedAt.Location() != time.UTC {
		t.Fatalf("observedAt = %v, want %v in UTC", acknowledgement.ObservedAt, fixedNow)
	}
	check := acknowledgement.ConformanceChecks[0]
	if check.ProviderID != "hermes" || check.Status != "passed" || check.Reason != "compatible" {
		t.Fatalf("conformance check was not normalized: %#v", check)
	}
	status := acknowledgement.ProviderStatuses[0]
	if status.ProviderID != "hermes" || status.ProviderVersion != "1.0.0" || status.Health != "healthy" || status.Reason != "ready" {
		t.Fatalf("provider status was not normalized: %#v", status)
	}
	if !status.ObservedAt.Equal(statusObservedAt) || status.ObservedAt.Location() != time.UTC {
		t.Fatalf("provider observedAt = %v, want %v in UTC", status.ObservedAt, statusObservedAt)
	}
}

func TestProviderControlPlaneAcknowledgementRejectsInvalidNestedStatus(t *testing.T) {
	service, err := NewProviderControlPlane(
		NewProviderReconciler(&mutableExtensionSource{}),
		providerPermissionStub{want: appaccess.PermAIAgentProvidersView},
	)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		input RegistryAcknowledgement
	}{
		{
			name: "conformance status",
			input: RegistryAcknowledgement{
				RunnerID:          "runner-1",
				Revision:          1,
				ConformanceChecks: []ProviderConformanceResult{{ProviderID: "hermes", Status: "unknown"}},
			},
		},
		{
			name: "provider health",
			input: RegistryAcknowledgement{
				RunnerID:         "runner-1",
				Revision:         1,
				ProviderStatuses: []RunnerProviderStatus{{ProviderID: "hermes", Health: "degraded"}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Acknowledge(test.input); err == nil {
				t.Fatal("Acknowledge() error = nil")
			}
		})
	}
}

func TestProviderControlPlaneSupportsConcurrentCatalogAndStatusReads(t *testing.T) {
	source := &mutableExtensionSource{}
	source.replace([]domainplugin.ExtensionRecord{providerExtension("1.0.0")})
	service, err := NewProviderControlPlane(NewProviderReconciler(source), providerPermissionStub{want: appaccess.PermAIAgentProvidersView})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			if _, err := service.Catalog(t.Context(), domainidentity.Principal{}); err != nil {
				t.Errorf("Catalog() error = %v", err)
			}
			if _, err := service.RuntimeStatus(t.Context(), domainidentity.Principal{}); err != nil {
				t.Errorf("RuntimeStatus() error = %v", err)
			}
		})
	}
	wg.Wait()
}

func TestProviderControlPlaneRestoresCatalogAndAcknowledgementsAcrossRestart(t *testing.T) {
	source := &mutableExtensionSource{}
	source.replace([]domainplugin.ExtensionRecord{providerExtension("1.0.0")})
	store := &providerMemoryStateStore{}
	options := []ProviderControlPlaneOption{WithProviderStateStore(store), WithProviderAcknowledgementLimit(2)}
	first, err := NewProviderControlPlane(NewProviderReconciler(source), providerPermissionStub{want: appaccess.PermAIAgentProvidersView}, options...)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := first.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	baseTime := time.Now().UTC()
	for i, runnerID := range []string{"runner-1", "runner-2", "runner-3"} {
		if _, err := first.Acknowledge(RegistryAcknowledgement{RunnerID: runnerID, Revision: catalog.Revision, ActiveRevision: catalog.Revision, Accepted: true, ObservedAt: baseTime.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}

	restarted, err := NewProviderControlPlane(NewProviderReconciler(source), providerPermissionStub{want: appaccess.PermAIAgentProvidersView}, options...)
	if err != nil {
		t.Fatal(err)
	}
	restoredCatalog, err := restarted.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if restoredCatalog.Revision != catalog.Revision || restoredCatalog.Digest != catalog.Digest {
		t.Fatalf("restored catalog = %#v, want revision=%d digest=%s", restoredCatalog, catalog.Revision, catalog.Digest)
	}
	status, err := restarted.RuntimeStatus(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if status.RunnerCount != 2 || status.Acknowledgements[0].RunnerID != "runner-2" || status.Acknowledgements[1].RunnerID != "runner-3" {
		t.Fatalf("restored status = %#v", status)
	}

	source.replace([]domainplugin.ExtensionRecord{providerExtension("2.0.0")})
	next, err := restarted.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision != catalog.Revision+1 || next.Digest == catalog.Digest {
		t.Fatalf("next catalog = %#v", next)
	}
}

func TestProviderControlPlaneKeepsLastKnownGoodCatalogForInvalidExtension(t *testing.T) {
	source := &mutableExtensionSource{}
	source.replace([]domainplugin.ExtensionRecord{providerExtension("1.0.0")})
	service, err := NewProviderControlPlane(NewProviderReconciler(source), providerPermissionStub{want: appaccess.PermAIAgentProvidersView})
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := providerExtension("2.0.0")
	invalid.Metadata["adapterProtocol"] = ""
	source.replace([]domainplugin.ExtensionRecord{invalid})
	after, err := service.Catalog(t.Context(), domainidentity.Principal{})
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision || after.Digest != before.Digest || after.Providers[0].ProviderVersion != "1.0.0" {
		t.Fatalf("last-known-good catalog was displaced: before=%#v after=%#v", before, after)
	}
}
