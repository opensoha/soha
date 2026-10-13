package virtualization

import (
	"context"
	"reflect"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func testManagedCloudImageSync(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	connection, err := repo.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "task51-image-sync", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM virtualization_images WHERE connection_id=?`, connection.ID).Error
		_ = repo.db.Exec(`DELETE FROM virtualization_connections WHERE id=?`, connection.ID).Error
	})
	managed := domain.Image{Provider: "pve", ConnectionID: connection.ID, ExternalID: "catalog:import/checksum.qcow2", Name: "Verified Debian cloud image", OSType: "debian", Architecture: "amd64", Config: map[string]any{"sourceKind": "cloud_image", "cloudInitSupported": true, "description": "operator confirmed"}, Raw: map[string]any{"managedBy": "soha"}}
	stored, err := repo.UpsertImage(ctx, managed)
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC().Truncate(time.Microsecond)
	discovered := domain.Image{Provider: "pve", ConnectionID: connection.ID, ExternalID: managed.ExternalID, Name: managed.ExternalID, Status: "unavailable", SizeBytes: 341508096, Config: map[string]any{"sourceKind": "import", "node": "node-4"}, Raw: map[string]any{"assetType": "storage_content"}, LastSeenAt: &observed}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			discovered.Status = "active"
			discovered.SizeBytes++
			observed = observed.Add(time.Second)
			discovered.Raw["scan"] = "second"
		}
		got, err := repo.UpsertImage(ctx, discovered)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != stored.ID || got.Name != managed.Name || got.OSType != managed.OSType || got.Architecture != managed.Architecture || !reflect.DeepEqual(got.Config, managed.Config) {
			t.Fatalf("discovery overwrote cloud image catalog: %+v", got)
		}
		if got.Status != discovered.Status || got.SizeBytes != discovered.SizeBytes || got.LastSeenAt == nil || !got.LastSeenAt.Equal(observed) || got.Raw["assetType"] != "storage_content" || got.Raw["managedBy"] != "soha" || got.Raw["scan"] != discovered.Raw["scan"] {
			t.Fatalf("provider facts or ownership marker lost: %+v", got)
		}
	}
	testCloudImageCatalogEdit(t, repo, ctx, managed)
	testProviderImageRefresh(t, repo, ctx, discovered)
}

func testCloudImageCatalogEdit(t *testing.T, repo *Repository, ctx context.Context, managed domain.Image) {
	t.Helper()
	managed.Name = "Updated verified image"
	managed.Config["description"] = "explicit edit"
	edited, err := repo.UpsertImage(ctx, managed)
	if err != nil || edited.Name != managed.Name || edited.Config["description"] != "explicit edit" {
		t.Fatalf("explicit catalog edit rejected: %+v %v", edited, err)
	}
}

func testProviderImageRefresh(t *testing.T, repo *Repository, ctx context.Context, discovered domain.Image) {
	t.Helper()
	discovered.ExternalID = "catalog:iso/ordinary.iso"
	first, err := repo.UpsertImage(ctx, discovered)
	if err != nil {
		t.Fatal(err)
	}
	discovered.Name = "refreshed provider image"
	discovered.Config = map[string]any{"sourceKind": "iso"}
	refreshed, err := repo.UpsertImage(ctx, discovered)
	if err != nil || refreshed.ID != first.ID || refreshed.Name != discovered.Name || !reflect.DeepEqual(refreshed.Config, discovered.Config) {
		t.Fatalf("ordinary discovery failed to refresh: %+v %v", refreshed, err)
	}
}
