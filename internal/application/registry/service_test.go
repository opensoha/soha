package registry

import (
	"context"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type captureRegistryRepository struct {
	items       []domainregistry.Connection
	created     domainregistry.Connection
	updated     domainregistry.Connection
	updateCalls int
}

func (r *captureRegistryRepository) List(context.Context, int) ([]domainregistry.Connection, error) {
	return append([]domainregistry.Connection(nil), r.items...), nil
}

func (r *captureRegistryRepository) Create(_ context.Context, item domainregistry.Connection) (domainregistry.Connection, error) {
	r.created = item
	return item, nil
}

func (r *captureRegistryRepository) Update(_ context.Context, _ string, item domainregistry.Connection) (domainregistry.Connection, error) {
	r.updateCalls++
	r.updated = item
	return item, nil
}

func (r *captureRegistryRepository) Delete(context.Context, string) error {
	return nil
}

type registryRoleReader map[string][]string

func (r registryRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	out := make(map[string][]string, len(r))
	for role, permissions := range r {
		out[role] = append([]string(nil), permissions...)
	}
	return out, nil
}

func TestCreateEncryptsRegistrySecretAndScrubsResponse(t *testing.T) {
	repo := &captureRegistryRepository{}
	service := New(repo, appaccess.NewPermissionResolver(registryRoleReader{
		"admin": {appaccess.PermDeliveryRegistriesManage},
	}), WithCredentialEncryptionKey("stable-test-key-32-bytes-or-more"))

	item, err := service.Create(context.Background(), domainidentity.Principal{Roles: []string{"admin"}}, domainregistry.Input{
		Name:         "Docker Hub",
		RegistryType: "docker",
		Endpoint:     "https://registry-1.docker.io",
		Secret:       "raw-registry-token",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if repo.created.Secret == "" || strings.Contains(repo.created.Secret, "raw-registry-token") || !secretcrypto.Encrypted(repo.created.Secret) {
		t.Fatalf("stored secret was not encrypted: %q", repo.created.Secret)
	}
	if item.Secret != "" || item.Metadata["secretConfigured"] != true || item.Metadata["secretStorage"] != "encrypted" {
		t.Fatalf("response was not scrubbed: %#v", item)
	}
}

func TestCreateRegistrySecretRequiresEncryptionKey(t *testing.T) {
	service := New(&captureRegistryRepository{}, appaccess.NewPermissionResolver(registryRoleReader{
		"admin": {appaccess.PermDeliveryRegistriesManage},
	}))

	_, err := service.Create(context.Background(), domainidentity.Principal{Roles: []string{"admin"}}, domainregistry.Input{
		Name:         "Docker Hub",
		RegistryType: "docker",
		Endpoint:     "https://registry-1.docker.io",
		Secret:       "raw-registry-token",
	})
	if err == nil || !strings.Contains(err.Error(), "credential_encryption_key") {
		t.Fatalf("Create error = %v, want credential_encryption_key requirement", err)
	}
}

func TestListScrubsRegistrySecrets(t *testing.T) {
	service := New(&captureRegistryRepository{items: []domainregistry.Connection{
		{ID: "encrypted", Name: "Encrypted", RegistryType: "docker", Endpoint: "https://registry.example.com", Secret: secretcrypto.Prefix + "payload"},
		{ID: "legacy", Name: "Legacy", RegistryType: "docker", Endpoint: "https://legacy.example.com", Secret: "legacy-token"},
	}}, appaccess.NewPermissionResolver(registryRoleReader{
		"viewer": {appaccess.PermDeliveryRegistriesView},
	}))

	items, err := service.List(context.Background(), domainidentity.Principal{Roles: []string{"viewer"}}, 10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Secret != "" || items[0].Metadata["secretStorage"] != "encrypted" {
		t.Fatalf("encrypted item leaked secret: %#v", items[0])
	}
	if items[1].Secret != "" || items[1].Metadata["secretStorage"] != "legacy_plaintext" {
		t.Fatalf("legacy item not marked correctly: %#v", items[1])
	}
}

func TestListMigratesLegacyRegistrySecrets(t *testing.T) {
	repo := &captureRegistryRepository{
		items: []domainregistry.Connection{
			{ID: "legacy", Name: "Legacy", RegistryType: "docker", Endpoint: "https://legacy.example.com", Secret: "legacy-token"},
		},
	}
	service := New(repo, appaccess.NewPermissionResolver(registryRoleReader{
		"viewer": {appaccess.PermDeliveryRegistriesView},
	}), WithCredentialEncryptionKey("stable-test-key-32-bytes-or-more"))

	items, err := service.List(context.Background(), domainidentity.Principal{Roles: []string{"viewer"}}, 10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", repo.updateCalls)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Secret != "" || items[0].Metadata["secretStorage"] != "legacy_plaintext" {
		t.Fatalf("response should remain scrubbed while migrating: %#v", items[0])
	}
	if !secretcrypto.Encrypted(repo.updated.Secret) {
		t.Fatalf("stored secret should be encrypted, got %q", repo.updated.Secret)
	}
}
