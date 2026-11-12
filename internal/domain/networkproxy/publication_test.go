package networkproxy

import (
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"testing"
	"time"
)

func TestPublicationDigestBindsIdentityAndAllowsLeaseRenewal(t *testing.T) {
	config := Configuration{ContentHash: "sha256:content", Enabled: true}
	if config.Digest() != config.ContentHash {
		t.Fatal("legacy digest changed")
	}
	config.Publication = &api.NetworkProxyServerPublication{BundleID: "bundle-1", Revision: 1, ValidUntil: time.Now().Add(time.Minute)}
	original := config.Digest()
	config.ValidUntil = time.Now().Add(2 * time.Minute)
	config.Publication.ValidUntil = config.ValidUntil
	if config.Digest() != original {
		t.Fatal("lease renewal changed enforcement digest")
	}
	config.Publication.Revision = 2
	if config.Digest() == original {
		t.Fatal("publication revision not bound")
	}
	config.Publication.Revision = 1
	config.Publication.BundleID = "bundle-2"
	if config.Digest() == original {
		t.Fatal("publication identity not bound")
	}
}
