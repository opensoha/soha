package proxypublication

import (
	"github.com/google/uuid"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"testing"
)

func TestNodeSecurityMaterialIgnoresAliasButIncludesAuthentication(t *testing.T) {
	base := api.NetworkProxyNode{NodeID: "node-a", Label: "Original", Protocol: "vmess", Server: "upstream.example", Port: 443, Username: "user", Secret: "secret", Cipher: "cipher", UUID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), TLS: &api.NetworkProxyTLS{Enabled: true, ServerName: "tls.example"}, Transport: &api.NetworkProxyTransport{Type: "ws", Path: "/ws"}}
	before := NodeMaterial(IR{Nodes: []api.NetworkProxyNode{base}})
	renamed := base
	renamed.Label = "Renamed"
	if len(InvalidatedNodes(before, NodeMaterial(IR{Nodes: []api.NetworkProxyNode{renamed}}))) != 0 {
		t.Fatal("display alias invalidated stable node")
	}
	for _, change := range []func(*api.NetworkProxyNode){func(n *api.NetworkProxyNode) { n.Secret = "rotated" }, func(n *api.NetworkProxyNode) { n.Username = "different" }, func(n *api.NetworkProxyNode) { n.UUID = uuid.New() }, func(n *api.NetworkProxyNode) { n.Cipher = "new-cipher" }, func(n *api.NetworkProxyNode) {
		n.TLS = &api.NetworkProxyTLS{Enabled: true, ServerName: "other.example"}
	}, func(n *api.NetworkProxyNode) {
		n.Transport = &api.NetworkProxyTransport{Type: "grpc", ServiceName: "service"}
	}, func(n *api.NetworkProxyNode) { n.Secret = "" }} {
		changed := base
		change(&changed)
		ids := InvalidatedNodes(before, NodeMaterial(IR{Nodes: []api.NetworkProxyNode{changed}}))
		if len(ids) != 1 || ids[0] != "node-a" {
			t.Fatal("security mutation omitted from diff")
		}
	}
}
func TestCredentialIdentityExcludesScopeButBindsEpochAndBoot(t *testing.T) {
	a := Credential{EndpointCredentialID: "cert", RuntimeID: "runtime", ProfileID: "profile", ProcessEpoch: "boot", BundleID: "bundle", Revision: 1, Epoch: 1, NodeIDs: []string{"a", "b"}}
	b := a
	b.NodeIDs = []string{"b"}
	if CredentialIdentity(a) != CredentialIdentity(b) {
		t.Fatal("scope shrink rotated identity")
	}
	b.Epoch++
	if CredentialIdentity(a) == CredentialIdentity(b) {
		t.Fatal("epoch not bound")
	}
	b = a
	b.ProcessEpoch = "other"
	if CredentialIdentity(a) == CredentialIdentity(b) {
		t.Fatal("boot not bound")
	}
}
