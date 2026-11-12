package proxypublication

import (
	"context"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"strings"
	"testing"
	"time"
)

func sourceFlowFixture(t *testing.T) (*Bundles, *bundleFake, api.NetworkProxyIngressCheckInput) {
	t.Helper()
	b, f, input := ingressFixture()
	b.keys = testKeys(t)
	f.record.SourceNodeIDs = []string{"node-a", "node-b"}
	f.record.Scope.NodeIDs = []string{"node-a", "node-b"}
	f.record.Nodes = []string{"node-a", "node-b"}
	f.record.Public.DeliveryMode = "mapped_nodes"
	f.record.Pool.Endpoints = []api.NetworkProxyEmployeeEndpoint{{EndpointID: "internal", URL: "https://internal.example"}}
	f.record.Groups = []api.NetworkProxyGroup{{GroupID: "group", Type: "select", Label: "Group", NodeIDs: []string{"node-a", "node-b"}, SelectedNodeID: "node-a"}}
	f.record.Rules = []api.NetworkProxyRule{{RuleID: "required-a", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain", Value: "blocked.example"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: "node-a"}}}
	f.credential.NodeIDs = []string{"node-a", "node-b"}
	f.credential.ProcessEpoch = "boot-1"
	f.credential.RenewalKey = domain.CredentialIdentity(f.credential)
	f.credential.EncryptedSecret, _ = secretcrypto.EncryptStringWithKeyring(b.keys, "synthetic-secret")
	b.endpoint = endpointFake{value: domain.EndpointContext{UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", CredentialID: f.credential.EndpointCredentialID, ProfileID: "profile-1", BundleID: "bundle-1", DeliveryMode: "mapped_nodes", OrgRevision: 1, SourceRevision: 1, ExpiresAt: b.now().Add(time.Hour), ConfigurationValidUntil: b.now().Add(time.Hour)}}
	input.NodeID = "node-b"
	return b, f, input
}
func TestSourceCompatibilityKeepsBAndNeverAddsC(t *testing.T) {
	b, _, input := sourceFlowFixture(t)
	source := fixtureValue[*sourceFake](t, b.sources)
	if source == nil {
		t.Fatal("missing source fixture")
	}
	source.source.Public.Revision = 2
	source.allowed = []string{"node-a", "node-b", "node-c"}
	out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || !out.Allowed {
		t.Fatal("rename/add interrupted B", err)
	}
	r, err := b.currentBundle(context.Background(), "", "bundle-1")
	if err != nil || len(r.Scope.NodeIDs) != 2 {
		t.Fatal("C joined old scope", err)
	}
	input.NodeID = "node-c"
	out, err = b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || out.Allowed {
		t.Fatal("unpublished C authorized", err)
	}
}
func TestSourceRevocationNarrowsCredentialWithoutRotatingB(t *testing.T) {
	b, f, input := sourceFlowFixture(t)
	before, err := b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	source := fixtureValue[*sourceFake](t, b.sources)
	if source == nil {
		t.Fatal("missing source fixture")
	}
	source.source.Public.Revision = 2
	source.allowed = []string{"node-b"}
	after, err := b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	if before.Credential.CredentialID != after.Credential.CredentialID || before.Credential.Secret != after.Credential.Secret || before.ProjectionHash != after.ProjectionHash || len(after.Nodes) != 1 || after.Nodes[0].NodeID != "node-b" || len(after.Rules) != 1 || after.Rules[0].Action.Type != "reject" || after.Groups[0].SelectedNodeID != "" {
		t.Fatal("B continuity or mandatory narrowing failed")
	}
	out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || !out.Allowed {
		t.Fatal("B stopped", err)
	}
	input.NodeID = "node-a"
	out, err = b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || out.Allowed {
		t.Fatal("A survived revocation", err)
	}
	source.source.Public.Revision = 3
	source.allowed = []string{"node-a", "node-b"}
	again, err := b.Projection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "", "profile-1", "boot-1")
	if err != nil || len(again.Nodes) != 1 || again.Nodes[0].NodeID != "node-b" || len(f.credential.NodeIDs) != 1 {
		t.Fatal("credential ceiling widened after restore", err)
	}
}
func TestGatewayInvalidatesWholeOriginalComposition(t *testing.T) {
	b, f, input := sourceFlowFixture(t)
	f.record.Public.DeliveryMode = "gateway"
	f.record.Scope.NodeIDs = []string{"gateway"}
	f.record.Nodes = []string{"gateway"}
	f.credential.NodeIDs = []string{"gateway"}
	input.NodeID = "gateway"
	source := fixtureValue[*sourceFake](t, b.sources)
	if source == nil {
		t.Fatal("missing source fixture")
	}
	source.source.Public.Revision = 2
	source.allowed = []string{"node-b"}
	out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || out.Allowed {
		t.Fatal("gateway silently replaced withdrawn constituent", err)
	}
}
func TestSourceFullDeletionStopsBWithoutFallback(t *testing.T) {
	b, _, input := sourceFlowFixture(t)
	source := fixtureValue[*sourceFake](t, b.sources)
	if source == nil {
		t.Fatal("missing source fixture")
	}
	source.source.Deleted = true
	out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", input)
	if err != nil || out.Allowed {
		t.Fatal("deleted source served B", err)
	}
}
func TestDirectUsesActualIssuedCeilingBeforeExport(t *testing.T) {
	b, f := directScopeFixture(t)
	f.record.Scope.NodeIDs = []string{"node-a", "node-b"}
	f.record.SourceNodeIDs = []string{"node-a", "node-b"}
	f.record.Rules = nil
	f.credential = domain.Credential{ID: "existing", EndpointCredentialID: "endpoint-cert", RuntimeID: "endpoint-1", ProfileID: "profile-1", ProcessEpoch: "boot-1", BundleID: "bundle-1", Revision: 1, Epoch: 1, NodeIDs: []string{"node-b"}}
	f.credential.RenewalKey = domain.CredentialIdentity(f.credential)
	out, err := b.DirectProjection(context.Background(), networkidentity.Identity{ID: "endpoint-1"}, "profile-1", "boot-1")
	if err != nil || strings.Contains(out.Content, "allowed-a-secret") || !strings.Contains(out.Content, "hidden-b-secret") {
		t.Fatal("direct exported pre-transaction wider scope", err)
	}
}
