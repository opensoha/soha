package virtualization

import (
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"testing"
)

func TestNetworkIdentityPreservesProviderReference(t *testing.T) {
	connection := domain.Connection{ID: "pve-connection", Provider: "pve"}
	seen := map[string]bool{}
	for _, node := range []string{"pve-a", "pve-b"} {
		uid := "node/" + node + "/network/vmbr0"
		image := imageFromAsset(connection, domain.Asset{Type: "network", Name: "vmbr0", Node: node, Metadata: map[string]string{"uid": uid, "network": "vmbr0", "sourceRef": "vmbr0"}})
		if image.ExternalID != uid || seen[image.ExternalID] {
			t.Fatal("node network identity lost before upsert")
		}
		seen[image.ExternalID] = true
		if image.Name != "vmbr0" || image.Config["network"] != "vmbr0" || image.Config["sourceRef"] != "vmbr0" || image.Config["node"] != node {
			t.Fatalf("provider network reference changed: %#v", image)
		}
	}
}
