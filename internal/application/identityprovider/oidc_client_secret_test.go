package identityprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"golang.org/x/crypto/bcrypt"
)

func TestCreateOIDCClientGeneratesRevealableCredentials(t *testing.T) {
	repo := newMemoryRepo(t)
	service := newOIDCSecretTestService(t, repo)

	created, err := service.CreateOIDCClient(t.Context(), (&memoryUsers{}).principal(), repo.provider.ID, domainprovider.OIDCClientInput{
		ClientType:        domainprovider.OIDCClientTypeConfidential,
		RedirectURIs:      []string{"https://app.example.com/oauth/callback"},
		AllowedScopes:     []string{"openid", "profile", "email"},
		AllowedGrantTypes: []string{"authorization_code"},
		RequirePKCE:       true,
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Client.ClientID == "" {
		t.Fatal("generated Client ID is empty")
	}
	if created.ClientSecret == "" {
		t.Fatal("generated Client Secret is empty")
	}
	if !created.Client.ClientSecretAvailable {
		t.Fatal("created Client Secret should be revealable")
	}
	if repo.client.ClientSecretCiphertext == "" || repo.client.ClientSecretHashAtEncryption == "" {
		t.Fatal("encrypted Client Secret material was not persisted")
	}

	revealed, err := service.RevealOIDCClientSecret(t.Context(), (&memoryUsers{}).principal(), created.Client.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revealed.ClientID != created.Client.ClientID || revealed.ClientSecret != created.ClientSecret {
		t.Fatalf("revealed credentials = %#v, want Client ID %q and original secret", revealed, created.Client.ClientID)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repo.client.ClientSecretHash), []byte(revealed.ClientSecret)); err != nil {
		t.Fatalf("revealed secret does not match authentication hash: %v", err)
	}
}

func TestUpdateOIDCClientPreservesGeneratedCredentialsWhenBlank(t *testing.T) {
	repo := newMemoryRepo(t)
	service := newOIDCSecretTestService(t, repo)
	created, err := service.CreateOIDCClient(t.Context(), (&memoryUsers{}).principal(), repo.provider.ID, domainprovider.OIDCClientInput{
		ClientType:        domainprovider.OIDCClientTypeConfidential,
		RedirectURIs:      []string{"https://app.example.com/oauth/callback"},
		AllowedScopes:     []string{"openid"},
		AllowedGrantTypes: []string{"authorization_code"},
		RequirePKCE:       true,
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	original, err := service.RevealOIDCClientSecret(t.Context(), (&memoryUsers{}).principal(), created.Client.ID)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := service.UpdateOIDCClient(t.Context(), (&memoryUsers{}).principal(), created.Client.ID, domainprovider.OIDCClientInput{
		ProviderID:        repo.provider.ID,
		ClientType:        domainprovider.OIDCClientTypeConfidential,
		RedirectURIs:      []string{"https://app.example.com/oauth/callback"},
		AllowedScopes:     []string{"openid", "profile"},
		AllowedGrantTypes: []string{"authorization_code"},
		RequirePKCE:       true,
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ClientID != created.Client.ClientID {
		t.Fatalf("Client ID = %q, want preserved %q", updated.ClientID, created.Client.ClientID)
	}
	revealed, err := service.RevealOIDCClientSecret(t.Context(), (&memoryUsers{}).principal(), created.Client.ID)
	if err != nil {
		t.Fatal(err)
	}
	if revealed.ClientSecret != original.ClientSecret {
		t.Fatal("blank update changed the Client Secret")
	}
}

func TestRevealOIDCClientSecretRejectsHistoricalHashOnlyRecord(t *testing.T) {
	repo := newMemoryRepo(t)
	audit := &oidcSecretAuditRecorder{}
	service := NewWithEncryptionKeys(repo, &memoryUsers{}, identityProviderTestPermissions(), audit, oidcSecretTestKeyring(t))

	_, err := service.RevealOIDCClientSecret(t.Context(), (&memoryUsers{}).principal(), repo.client.ID)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("RevealOIDCClientSecret error = %v, want conflict", err)
	}
	if len(audit.entries) != 1 || audit.entries[0].Result != "denied" || audit.entries[0].Action != "identity.oidc_client.secret.reveal" {
		t.Fatalf("audit entries = %#v, want one denied reveal", audit.entries)
	}
}

func newOIDCSecretTestService(t *testing.T, repo *memoryRepo) *Service {
	t.Helper()
	return NewWithEncryptionKeys(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, oidcSecretTestKeyring(t))
}

func oidcSecretTestKeyring(t *testing.T) keyring.Ring {
	t.Helper()
	key, err := keyring.NewKey("test", "test-encryption-key-32-bytes-long", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

type oidcSecretAuditRecorder struct{ entries []domainaudit.Entry }

func (r *oidcSecretAuditRecorder) Record(_ context.Context, entry domainaudit.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}
