package proxypublication

import (
	"context"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

// Transport and upstream UDP support are independently proved by the runtime.
// Authorization uses the same credential, node, current policy and lease facts.
func TestUDPIngressRequiresCurrentCredentialNodeAndDestination(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Bundles, *bundleFake, *api.NetworkProxyIngressCheckInput)
		allowed bool
	}{
		{name: "current grant", allowed: true},
		{name: "wrong secret", mutate: func(_ *Bundles, _ *bundleFake, in *api.NetworkProxyIngressCheckInput) { in.CredentialSecret = "wrong" }},
		{name: "other node", mutate: func(_ *Bundles, _ *bundleFake, in *api.NetworkProxyIngressCheckInput) { in.NodeID = "node-2" }},
		{name: "old epoch", mutate: func(_ *Bundles, _ *bundleFake, in *api.NetworkProxyIngressCheckInput) { in.CredentialEpoch++ }},
		{name: "protected destination", mutate: func(b *Bundles, _ *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			b.destination = destinationFake{err: apperrors.ErrAccessDenied}
		}},
		{name: "expired credential", mutate: func(b *Bundles, f *bundleFake, _ *api.NetworkProxyIngressCheckInput) {
			f.credential.ExpiresAt = b.now()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, f, in := ingressFixture()
			in.Network = "udp"
			if tt.mutate != nil {
				tt.mutate(b, f, &in)
			}
			out, err := b.IngressCheck(context.Background(), networkidentity.Identity{ID: "proxy-1"}, "", in)
			if err != nil || out.Allowed != tt.allowed {
				t.Fatalf("UDP allowed=%v want=%v err=%v", out.Allowed, tt.allowed, err)
			}
			if out.Allowed && (!out.ExpiresAt.After(b.now()) || out.ExpiresAt.After(b.now().Add(5e9))) {
				t.Fatal("UDP grant exceeded normal authorization lease")
			}
		})
	}
}
