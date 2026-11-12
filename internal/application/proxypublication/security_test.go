package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"strings"
	"testing"
	"time"
)

type bundleFake struct {
	record     domain.BundleRecord
	credential domain.Credential
	saved      bool
	receipt    api.NetworkProxyProjectionApplied
}

func (f *bundleFake) GetBundle(context.Context, string, string) (domain.BundleRecord, error) {
	return f.record, nil
}
func (f *bundleFake) FindBundle(context.Context, string) (domain.BundleRecord, error) {
	return f.record, nil
}
func (f *bundleFake) ListBundles(context.Context, string) ([]domain.BundleRecord, error) {
	return []domain.BundleRecord{f.record}, nil
}
func (f *bundleFake) SaveBundle(_ context.Context, r domain.BundleRecord, _ int64) error {
	f.record = r
	f.saved = true
	return nil
}
func (f *bundleFake) IssueCredential(_ context.Context, c domain.Credential) (domain.Credential, error) {
	if f.credential.RenewalKey != "" && f.credential.RenewalKey == c.RenewalKey {
		f.credential.NodeIDs = domain.IntersectNodes(f.credential.NodeIDs, c.NodeIDs)
		f.credential.ExpiresAt = c.ExpiresAt
		return f.credential, nil
	}
	f.credential = c
	return c, nil
}
func (f *bundleFake) GetCredential(context.Context, string) (domain.Credential, error) {
	return f.credential, nil
}
func (f *bundleFake) SaveProjectionApplied(_ context.Context, _ domain.EndpointContext, _ string, input api.NetworkProxyProjectionApplied) error {
	f.receipt = input
	return nil
}

type sourceFake struct {
	source  domain.SourceRecord
	allowed []string
}

func (f *sourceFake) GetSource(context.Context, string, string) (domain.SourceRecord, error) {
	return f.source, nil
}
func (f *sourceFake) ListSources(context.Context, string) ([]domain.SourceRecord, error) {
	return []domain.SourceRecord{f.source}, nil
}
func (f *sourceFake) SaveSource(_ context.Context, r domain.SourceRecord, _ int64) error {
	f.source = r
	return nil
}
func (f *sourceFake) DeleteSource(context.Context, string, string, int64) error { return nil }

type ruleFake struct{ rules api.NetworkProxyRules }

func (f *ruleFake) ReadRules(context.Context, string, string) (api.NetworkProxyRules, error) {
	return f.rules, nil
}
func (f *ruleFake) SaveRules(context.Context, string, string, api.NetworkProxyRules, int64, int64) error {
	return nil
}

type poolFake struct{ pool domain.ApprovedPool }

func (f *poolFake) ApprovedPool(context.Context, string) (domain.ApprovedPool, error) {
	return f.pool, nil
}

type runtimeFake struct{ err error }

func (f runtimeFake) AuthorizeProxyRuntime(context.Context, networkidentity.Identity) error {
	return f.err
}

type currentFake struct {
	endpoint domain.EndpointContext
	err      error
}

func (f currentFake) ProjectionContext(context.Context, string, string, time.Time) (domain.EndpointContext, error) {
	return f.endpoint, f.err
}

type destinationFake struct{ err error }

func (f destinationFake) AuthorizeDestination(context.Context, domain.EndpointContext, domain.BundleRecord, string, string, int) error {
	return f.err
}

type desiredFake struct {
	states []proxy.MemberConfigurationState
}

func (f desiredFake) ObserveMemberConfigurations(context.Context, []string) ([]proxy.MemberConfigurationState, error) {
	return f.states, nil
}
func (f desiredFake) CommitDesiredBatch(context.Context, identity.Principal, proxy.DesiredBatch) error {
	return nil
}
func ingressFixture() (*Bundles, *bundleFake, api.NetworkProxyIngressCheckInput) {
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	configUntil := now.Add(10 * time.Minute)
	pool := domain.ApprovedPool{MasterID: "master", MemberSetHash: "members", OwnerEpoch: 1, Members: []domain.PoolMember{{RuntimeID: "proxy-1", Running: true, Serving: true}}}
	record := domain.BundleRecord{InstanceID: "master", SourceNodeIDs: []string{"node-1"}, Pool: pool, Scope: api.NetworkProxyScope{UserIDs: []string{"user-1"}, DeviceIDs: []string{"device-1"}, NodeIDs: []string{"node-1"}}, Public: api.NetworkProxyBundle{BundleID: "bundle-1", Revision: 1, ActiveRevision: 1, State: "activated", CredentialEpoch: 1, SourceID: "source-1", SourceRevision: 1, OrgRevision: 1, ValidUntil: now.Add(time.Minute)}, Readbacks: map[string]api.NetworkProxyBundleReadback{"proxy-1": {ProcessEpoch: "server-epoch", Revision: 1}}}
	f := &bundleFake{record: record, credential: domain.Credential{ID: "credential", InstanceID: "master", EndpointCredentialID: "endpoint-credential", BundleID: "bundle-1", Revision: 1, Epoch: 1, UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", ProfileID: "profile-1", NodeIDs: []string{"node-1"}, SecretHash: secretHash("synthetic-secret"), ExpiresAt: now.Add(time.Minute)}} // #nosec G101 -- Fixed synthetic credential used only by the in-memory authorization fixture.
	endpoint := domain.EndpointContext{UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", ProfileID: "profile-1", BundleID: "bundle-1", OrgRevision: 1, SourceRevision: 1}
	b := &Bundles{store: f, sources: &sourceFake{source: domain.SourceRecord{Public: api.NetworkProxySource{Revision: 1}}}, rules: &ruleFake{rules: api.NetworkProxyRules{OrgRevision: 1}}, pool: &poolFake{pool: pool}, runtime: runtimeFake{}, current: currentFake{endpoint: endpoint}, destination: destinationFake{}, desired: desiredFake{states: []proxy.MemberConfigurationState{{InstanceID: "proxy-1", BundleID: "bundle-1", BundleRevision: 1, DesiredRevision: 1, AppliedRevision: 1, DesiredHash: "digest", AppliedHash: "digest", PolicyVersion: 1, ObservedPolicyVersion: 1, ProcessEpoch: "server-epoch", LastSeenAt: &now, ConfigurationValidUntil: &configUntil}}}, now: func() time.Time { return now }}
	input := api.NetworkProxyIngressCheckInput{BundleID: "bundle-1", Revision: 1, CredentialEpoch: 1, CredentialID: "credential", CredentialSecret: "synthetic-secret", NodeID: "node-1", ProcessEpoch: "server-epoch", DestinationHost: "public.example", DestinationPort: 443, Network: "tcp"}
	return b, f, input
}
func TestIngressCurrentScopeAndSecretFailures(t *testing.T) {
	cases := map[string]func(*Bundles, *bundleFake, *api.NetworkProxyIngressCheckInput){
		"wrong_secret": func(_ *Bundles, _ *bundleFake, i *api.NetworkProxyIngressCheckInput) { i.CredentialSecret = "wrong" },
		"cross_node":   func(_ *Bundles, _ *bundleFake, i *api.NetworkProxyIngressCheckInput) { i.NodeID = "node-2" },
		"old_epoch":    func(_ *Bundles, _ *bundleFake, i *api.NetworkProxyIngressCheckInput) { i.CredentialEpoch++ },
		"server_restart": func(_ *Bundles, _ *bundleFake, i *api.NetworkProxyIngressCheckInput) {
			i.ProcessEpoch = "different-epoch"
		},
		"unknown_network": func(_ *Bundles, _ *bundleFake, i *api.NetworkProxyIngressCheckInput) { i.Network = "icmp" },
		"revoked_source": func(b *Bundles, _ *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			fixtureValue[*sourceFake](t, b.sources).source.Public.Revision++
			fixtureValue[*sourceFake](t, b.sources).allowed = []string{}
		},
		"new_org": func(b *Bundles, _ *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			fixtureValue[*ruleFake](t, b.rules).rules.OrgRevision++
		},
		"revoked_user": func(b *Bundles, _ *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			b.current = currentFake{err: apperrors.ErrAccessDenied}
		},
		"protected_destination": func(b *Bundles, _ *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			b.destination = destinationFake{err: apperrors.ErrAccessDenied}
		},
		"expired": func(b *Bundles, f *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			f.credential.ExpiresAt = b.now()
		},
		"pool_changed": func(b *Bundles, _ *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			fixtureValue[*poolFake](t, b.pool).pool.OwnerEpoch++
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b, f, input := ingressFixture()
			mutate(b, f, &input)
			out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
			if err != nil || out.Allowed {
				t.Fatalf("not denied: %+v %v", out, err)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "synthetic-secret") {
				t.Fatal("credential leaked")
			}
		})
	}
	b, _, input := ingressFixture()
	out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || !out.Allowed || out.ExpiresAt.Sub(b.now()) > 5*time.Second {
		t.Fatal("current authorization rejected", out, err)
	}
}
func TestAdministratorCannotAssertReadiness(t *testing.T) {
	b, f, _ := ingressFixture()
	b.runtime = runtimeFake{err: apperrors.ErrUnauthorized}
	err := b.ServerReady(context.Background(), networkidentity.Identity{}, "master", api.NetworkProxyBundleReadback{})
	if !errors.Is(err, apperrors.ErrUnauthorized) || f.saved {
		t.Fatal("untrusted ready accepted", err)
	}
}

func (f desiredFake) GetDesiredBatchReceipt(context.Context, string) (proxy.DesiredBatchReceipt, error) {
	return proxy.DesiredBatchReceipt{}, apperrors.ErrNotFound
}

type authFake struct{ err error }

func (a authFake) Authorize(context.Context, identity.Principal, string, string) error { return a.err }

type auditFake struct{}

func (auditFake) RecordPublication(context.Context, identity.Principal, string, string, string, int64) error {
	return nil
}

type compilerFake struct{ err error }

func (c compilerFake) Compile(api.NetworkProxySourceInput) (domain.IR, error) {
	return domain.IR{Nodes: []api.NetworkProxyNode{{NodeID: "node-1", Label: "Node 1", Protocol: "http", Server: "synthetic-upstream.example", Port: 443, Secret: "synthetic-upstream-secret"}}}, c.err
}

type fetchFake struct{}

func (fetchFake) Fetch(context.Context, string) (string, error) { return "source-content", nil }
func testKeys(t *testing.T) keyring.Ring {
	t.Helper()
	key, err := keyring.NewKey("source-key", "synthetic-encryption-key", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}
func TestSourceEncryptionAndPublicSeparation(t *testing.T) {
	store := &sourceFake{}
	keys := testKeys(t)
	service, err := NewSources(store, authFake{}, auditFake{}, compilerFake{}, fetchFake{}, keys)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Put(context.Background(), identity.Principal{UserID: "user"}, "instance", "source", api.NetworkProxySourceInput{Name: "Source", Format: "mihomo_yaml", Content: "synthetic-original-content", DeliveryMode: "mapped_nodes"})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"synthetic-original-content", "synthetic-upstream.example", "synthetic-upstream-secret"} {
		if strings.Contains(store.source.EncryptedInput, secret) || strings.Contains(store.source.EncryptedIR, secret) {
			t.Fatal("plaintext storage")
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), secret) {
			t.Fatal("public source leaked")
		}
	}
	plain, err := secretcrypto.DecryptStringWithKeyring(keys, store.source.EncryptedIR)
	if err != nil || !strings.Contains(plain, "synthetic-upstream-secret") {
		t.Fatal("encrypted IR lost content", err)
	}
}
func TestPreviewRejectsAtomicallyWithCanonicalSafeError(t *testing.T) {
	service, err := NewSources(&sourceFake{}, authFake{}, auditFake{}, compilerFake{err: domain.Invalid("unsupported_tls", "nodes[0]")}, fetchFake{}, testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Preview(context.Background(), identity.Principal{}, "instance", api.NetworkProxySourceInput{Content: "synthetic-secret"})
	if err != nil || out.Supported || len(out.Nodes) != 0 || len(out.Errors) != 1 || !out.Errors[0].Code.Valid() {
		t.Fatal("invalid typed preview", out, err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "synthetic-secret") {
		t.Fatal("preview leaked secret")
	}
}

type engineFake struct{ ir domain.IR }

func (e *engineFake) Export(ir domain.IR) (string, error) {
	e.ir = ir
	return "compiled-safe-outbound-config", nil
}
func TestGatewayAggregationIsExplicitAndMappedNodesStaySeparate(t *testing.T) {
	for _, mode := range []api.NetworkProxyDeliveryMode{"mapped_nodes", "gateway"} {
		t.Run(string(mode), func(t *testing.T) {
			engine := &engineFake{}
			b := &Bundles{engine: engine}
			input := api.NetworkProxyBundleInput{DeliveryMode: mode, SourceID: "source"}
			ir := domain.IR{Nodes: []api.NetworkProxyNode{{NodeID: "node-a"}, {NodeID: "node-b"}}}
			_, compiled, nodes, err := b.compileBundle(&input, ir)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "mapped_nodes" {
				if len(nodes) != 2 || len(compiled.Groups) != 0 {
					t.Fatal("mapped topology lost")
				}
			} else {
				if len(nodes) != 1 || !strings.HasPrefix(nodes[0], "gateway-") || len(compiled.Groups) != 1 || len(compiled.Groups[0].NodeIDs) != 2 || !strings.HasPrefix(compiled.Groups[0].GroupID, "aggregate-") {
					t.Fatal("gateway lacks explicit aggregation")
				}
				if len(input.Scope.NodeIDs) != 1 || input.Scope.NodeIDs[0] != nodes[0] {
					t.Fatal("scope not bound to aggregate")
				}
			}
		})
	}
}

func TestOmittedCredentialsPreserveOnlySameNodeIdentity(t *testing.T) {
	old := []api.NetworkProxyNode{{Protocol: "trojan", Server: "origin.example", Port: 443, Secret: "synthetic-secret"}}
	nodes := []api.NetworkProxyNode{{Protocol: "trojan", Server: "origin.example", Port: 443, Label: "Renamed"}, {Protocol: "trojan", Server: "different.example", Port: 443}}
	preserveNodeCredentials(nodes, old)
	if nodes[0].Secret != "synthetic-secret" || nodes[1].Secret != "" {
		t.Fatal("credential preserve crossed node identity")
	}
}

func (e *engineFake) ExportDirect(ir domain.IR) (string, error) { return e.Export(ir) }

type endpointFake struct {
	value domain.EndpointContext
	err   error
}

func (e endpointFake) AuthorizeProjection(context.Context, networkidentity.Identity, string) (domain.EndpointContext, error) {
	return e.value, e.err
}
func TestDirectDeliveryUsesDedicatedAuthorizedPath(t *testing.T) {
	b, f, _ := ingressFixture()
	b.keys = testKeys(t)
	b.pool = nil
	b.desired = nil
	f.record.Public.DeliveryMode = "direct_subscription"
	f.record.Pool = domain.ApprovedPool{}
	raw, _ := json.Marshal(domain.IR{Nodes: []api.NetworkProxyNode{{NodeID: "node-1", Server: "upstream.example", Protocol: "http", Port: 443}}})
	f.record.EncryptedIR, _ = secretcrypto.EncryptStringWithKeyring(b.keys, string(raw))
	endpoint := domain.EndpointContext{UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", CredentialID: "endpoint-credential", ProfileID: "profile-1", BundleID: "bundle-1", DeliveryMode: "direct_subscription", OrgRevision: 1, SourceRevision: 1, ExpiresAt: b.now().Add(time.Minute), ConfigurationValidUntil: b.now().Add(time.Minute)} // #nosec G101 -- Fixed synthetic credential used only by the in-memory projection fixture.
	b.endpoint = endpointFake{value: endpoint}
	b.engine = &engineFake{}
	out, err := b.DirectProjection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "profile-1", "app-boot")
	if err != nil || out.DeliveryMode != "direct_subscription" || out.UpstreamDisclosure != "upstream_parameters" || out.ProcessEpoch != "app-boot" {
		t.Fatal("direct projection rejected", err)
	}
	if _, err = b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "app-boot"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("direct mode crossed mapped path", err)
	}
	endpoint.DeviceID = "other-device"
	b.endpoint = endpointFake{value: endpoint}
	if _, err = b.DirectProjection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "profile-1", "app-boot"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("wrong device received upstream", err)
	}
}

func (f destinationFake) CurrentProtection(context.Context) (*access.ProtectedDestinations, error) {
	return &access.ProtectedDestinations{PolicyVersion: 1}, nil
}

func TestActivatedLeaseRenewalPreservesScopedCredential(t *testing.T) {
	b, f, _ := ingressFixture()
	b.keys = testKeys(t)
	now := b.now()
	endpoint := domain.EndpointContext{UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", CredentialID: "endpoint-cert", ProfileID: "profile-1", BundleID: "bundle-1", DeliveryMode: "mapped_nodes", OrgRevision: 1, SourceRevision: 1, ExpiresAt: now.Add(time.Hour), ConfigurationValidUntil: now.Add(time.Hour)}
	b.endpoint = endpointFake{value: endpoint}
	f.record.Pool.Endpoints = []api.NetworkProxyEmployeeEndpoint{{EndpointID: "internal", URL: "https://internal.example:443"}}
	f.record.Nodes = []string{"node-1"}
	f.record.Public.DeliveryMode = "mapped_nodes"
	f.record.Public.ValidUntil = now.Add(-time.Minute)
	first, err := b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Minute)
	b.now = func() time.Time { return later }
	state := fixtureValue[desiredFake](t, b.desired)
	state.states[0].LastSeenAt = &later
	b.desired = state
	second, err := b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Credential.CredentialID != second.Credential.CredentialID || first.Credential.Secret != second.Credential.Secret || first.CredentialEpoch != second.CredentialEpoch || first.Revision != second.Revision || !second.ExpiresAt.After(first.ExpiresAt) || second.ExpiresAt.After(later.Add(5*time.Minute)) {
		t.Fatal("same-scope lease rotated immutable identity")
	}
	fixtureValue[*ruleFake](t, b.rules).rules.OrgRevision++
	if _, err = b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "boot-1"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("revoked revision renewed", err)
	}
}
func TestServerLeaseRespectsCertificateBoundAndPreparedExpiry(t *testing.T) {
	b, f, _ := ingressFixture()
	f.record.Public.ValidUntil = b.now().Add(-time.Minute)
	until := b.now().Add(20 * time.Second)
	out, err := b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, until)
	if err != nil || out.ValidUntil != until {
		t.Fatal("activated renewal failed or exceeded cert", err)
	}
	f.record.Public.State = "prepare"
	if _, err = b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, until); !errors.Is(err, apperrors.ErrGone) {
		t.Fatal("stale prepare renewed", err)
	}
	f.record.Public.State = "activated"
	fixtureValue[*sourceFake](t, b.sources).source.Public.Revision++
	fixtureValue[*sourceFake](t, b.sources).allowed = []string{}
	if _, err = b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, until); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("old source renewed", err)
	}
}

func TestRenewedServerReadbackDoesNotDowngradeActivation(t *testing.T) {
	b, f, _ := ingressFixture()
	f.record.Public.ValidUntil = b.now().Add(-time.Minute)
	err := b.ServerReady(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "master", api.NetworkProxyBundleReadback{BundleID: "bundle-1", Revision: 1, ConfigurationRevision: 1, ServerHash: "", ProcessEpoch: "server-epoch", ObservedAt: b.now(), IngressReady: true, ProtectionReady: true})
	if err != nil || !f.saved || f.record.Public.State != "activated" || f.record.Public.ActiveRevision != 1 {
		t.Fatal("readback downgraded active lease", err)
	}
}

func TestDrainServingMemberKeepsAuthorizedFlowsAndUnknownDenies(t *testing.T) {
	for _, test := range []struct {
		name                      string
		serving, running, allowed bool
	}{
		{name: "normal_drain", serving: true, running: false, allowed: true},
		{name: "unknown_target_still_running", serving: false, running: true, allowed: false},
		{name: "actually_stopped", serving: false, running: false, allowed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			b, _, input := ingressFixture()
			pool := fixtureValue[*poolFake](t, b.pool)
			pool.pool.Members[0].Running = test.running
			pool.pool.Members[0].Serving = test.serving
			if test.serving && !test.running {
				deadline := b.now().Add(time.Minute)
				pool.pool.Members[0].ServingUntil = &deadline
			}
			out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
			if err != nil || out.Allowed != test.allowed {
				t.Fatal("unexpected serving decision", out.Allowed, err)
			}
			if test.allowed {
				fixtureValue[*sourceFake](t, b.sources).source.Public.Revision++
				fixtureValue[*sourceFake](t, b.sources).allowed = []string{}
				out, err = b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
				if err != nil || out.Allowed {
					t.Fatal("security revocation waited for drain", err)
				}
			}
		})
	}
}
func TestDrainPublicationCannotExtendIssuedLease(t *testing.T) {
	b, _, _ := ingressFixture()
	pool := fixtureValue[*poolFake](t, b.pool)
	pool.pool.Members[0].Running = false
	pool.pool.Members[0].Serving = true
	drainBound := b.now().Add(40 * time.Second)
	pool.pool.Members[0].ServingUntil = &drainBound
	issued := b.now().Add(20 * time.Second)
	states := fixtureValue[desiredFake](t, b.desired)
	states.states[0].ConfigurationValidUntil = &issued
	b.desired = states
	out, err := b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, b.now().Add(5*time.Minute))
	if err != nil || out.ValidUntil != issued {
		t.Fatal("drain extended existing grant", out.ValidUntil, err)
	}
	deadline := b.now().Add(10 * time.Second)
	pool.pool.Members[0].ServingUntil = &deadline
	out, err = b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, b.now().Add(5*time.Minute))
	if err != nil || out.ValidUntil != deadline {
		t.Fatal("drain deadline exceeded", out.ValidUntil, err)
	}
	pool.pool.Members[0].ServingUntil = nil
	if _, err = b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, b.now().Add(5*time.Minute)); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("missing drain bound minted grant", err)
	}
	pool.pool.Members[0].ServingUntil = &deadline
	states.states[0].ConfigurationValidUntil = nil
	b.desired = states
	if _, err = b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, b.now().Add(5*time.Minute)); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("drain minted first grant", err)
	}
	pool.pool.Members[0].Serving = false
	pool.pool.Members[0].Running = true
	if _, err = b.ServerPublication(context.Background(), "proxy-1", "bundle-1", 1, b.now().Add(5*time.Minute)); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("unknown serving minted grant", err)
	}
}
func TestDrainServingLeaseHonorsTrustedDeadline(t *testing.T) {
	b, _, _ := ingressFixture()
	r, err := b.currentBundle(context.Background(), "master", "bundle-1")
	if err != nil {
		t.Fatal(err)
	}
	deadline := b.now().Add(2 * time.Second)
	r.Pool.Members[0].Running = false
	r.Pool.Members[0].Serving = true
	r.Pool.Members[0].ServingUntil = &deadline
	until, err := b.servingLease(context.Background(), r, "proxy-1", b.now().Add(5*time.Second), 1)
	if err != nil || until != deadline {
		t.Fatal("active flow lease exceeded deadline", until, err)
	}
}

func (f *sourceFake) AllowedSourceNodes(_ context.Context, _ string, _ string, revision int64, original []string) ([]string, error) {
	if f.source.Deleted || revision < f.source.CompatibilityFloor || revision > f.source.Public.Revision {
		return nil, apperrors.ErrAccessDenied
	}
	if f.allowed == nil {
		return append([]string(nil), original...), nil
	}
	return domain.IntersectNodes(original, f.allowed), nil
}
