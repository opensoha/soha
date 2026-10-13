package virtualization

import (
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func TestStoragePoolIdentityPreservesProviderReference(t *testing.T) {
	connection := domain.Connection{ID: "pve-connection", Provider: "pve"}
	seen := map[string]bool{}
	for _, node := range []string{"pve-a", "pve-b"} {
		uid := "node/" + node + "/storage/local"
		asset := domain.Asset{Type: "storage", Name: "local", Node: node, Metadata: map[string]string{"uid": uid, "storage": "local"}}
		image := imageFromAsset(connection, asset)
		if image.ExternalID != uid || seen[image.ExternalID] {
			t.Fatal("node storage identity lost before repository upsert")
		}
		seen[image.ExternalID] = true
		if image.Name != "local" || image.Config["storage"] != "local" || image.Config["sourceRef"] != "local" || image.Config["node"] != node {
			t.Fatalf("provider placement reference changed: %#v", image)
		}
	}
}
