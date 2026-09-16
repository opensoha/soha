package identityprovider

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) RevealOIDCClientSecret(ctx context.Context, principal domainidentity.Principal, clientID string) (domainprovider.OIDCClientSecretReveal, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermIdentityProvidersManage, "update")); err != nil {
		return domainprovider.OIDCClientSecretReveal{}, err
	}
	client, err := s.repo.GetOIDCClient(ctx, clientID)
	if err != nil {
		return domainprovider.OIDCClientSecretReveal{}, err
	}
	if !oidcClientSecretMaterialAvailable(client) {
		s.recordAudit(ctx, principal, "identity.oidc_client.secret.reveal", "denied", domainprovider.Provider{ID: client.ProviderID, Type: domainprovider.ProviderTypeOIDC}, client, map[string]any{"reason": "secret_unavailable"})
		return domainprovider.OIDCClientSecretReveal{}, fmt.Errorf("%w: oidc client secret is unavailable; set a new secret first", apperrors.ErrConflict)
	}
	plaintext, err := s.decryptOIDCClientSecret(client.ClientSecretCiphertext)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(client.ClientSecretHash), []byte(plaintext)) != nil {
		s.recordAudit(ctx, principal, "identity.oidc_client.secret.reveal", "denied", domainprovider.Provider{ID: client.ProviderID, Type: domainprovider.ProviderTypeOIDC}, client, map[string]any{"reason": "secret_unavailable"})
		return domainprovider.OIDCClientSecretReveal{}, fmt.Errorf("%w: oidc client secret is unavailable", apperrors.ErrConflict)
	}
	provider, err := s.repo.GetProvider(ctx, client.ProviderID)
	if err != nil {
		return domainprovider.OIDCClientSecretReveal{}, err
	}
	s.recordAudit(ctx, principal, "identity.oidc_client.secret.reveal", "success", provider, client, nil)
	return domainprovider.OIDCClientSecretReveal{
		ClientID:     client.ClientID,
		ClientSecret: plaintext,
		RevealedAt:   time.Now().UTC(),
	}, nil
}

func (s *Service) protectOIDCClientSecret(client *domainprovider.OIDCClient, plaintext string) error {
	plaintext = strings.TrimSpace(plaintext)
	if client == nil || plaintext == "" || client.ClientType == domainprovider.OIDCClientTypePublic {
		return nil
	}
	ciphertext, err := s.encryptOIDCClientSecret(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt oidc client secret: %w", err)
	}
	client.ClientSecretCiphertext = ciphertext
	client.ClientSecretHashAtEncryption = client.ClientSecretHash
	return nil
}

func (s *Service) encryptOIDCClientSecret(plaintext string) (string, error) {
	if s.encryptionKeys.Active().ID() != "" {
		return secretcrypto.EncryptStringWithKeyring(s.encryptionKeys, plaintext)
	}
	return secretcrypto.EncryptString(s.encryptionKey, plaintext)
}

func (s *Service) decryptOIDCClientSecret(ciphertext string) (string, error) {
	if !secretcrypto.Encrypted(ciphertext) {
		return "", fmt.Errorf("oidc client secret is not encrypted")
	}
	if s.encryptionKeys.Active().ID() != "" {
		return secretcrypto.DecryptStringWithKeyring(s.encryptionKeys, ciphertext)
	}
	return secretcrypto.DecryptString(s.encryptionKey, ciphertext)
}

func oidcClientsForResponse(items []domainprovider.OIDCClient) []domainprovider.OIDCClient {
	out := make([]domainprovider.OIDCClient, len(items))
	for index, item := range items {
		out[index] = oidcClientForResponse(item)
	}
	return out
}

func oidcClientForResponse(item domainprovider.OIDCClient) domainprovider.OIDCClient {
	item.GrantTypes = append([]string{}, item.AllowedGrantTypes...)
	item.ClientSecretAvailable = oidcClientSecretMaterialAvailable(item)
	return item
}

func oidcClientSecretMaterialAvailable(item domainprovider.OIDCClient) bool {
	return item.ClientType == domainprovider.OIDCClientTypeConfidential &&
		strings.TrimSpace(item.ClientSecretHash) != "" &&
		strings.TrimSpace(item.ClientSecretCiphertext) != "" &&
		subtleHashEqual(item.ClientSecretHashAtEncryption, item.ClientSecretHash)
}

func subtleHashEqual(left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	return left != "" && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
