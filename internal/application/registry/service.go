package registry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type Service struct {
	repo          domainregistry.Repository
	permissions   *appaccess.PermissionResolver
	credentialKey string
}

type Option func(*Service)

func WithCredentialEncryptionKey(key string) Option {
	return func(s *Service) {
		s.credentialKey = strings.TrimSpace(key)
	}
}

func New(repo domainregistry.Repository, permissions *appaccess.PermissionResolver, opts ...Option) *Service {
	service := &Service{repo: repo, permissions: permissions}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainregistry.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesView); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	s.migrateLegacySecrets(ctx, items)
	return scrubConnections(items), nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, input domainregistry.Input) (domainregistry.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesManage); err != nil {
		return domainregistry.Connection{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainregistry.Connection{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item.ID = normalizeID(item.ID, item.Name)
	item.CreatedAt = now
	item.UpdatedAt = now
	if err := s.encryptSecret(&item); err != nil {
		return domainregistry.Connection{}, err
	}
	created, err := s.repo.Create(ctx, item)
	if err != nil {
		return domainregistry.Connection{}, err
	}
	return scrubConnection(created), nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, id string, input domainregistry.Input) (domainregistry.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesManage); err != nil {
		return domainregistry.Connection{}, err
	}
	item, err := normalizeInput(input)
	if err != nil {
		return domainregistry.Connection{}, err
	}
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.encryptSecret(&item); err != nil {
		return domainregistry.Connection{}, err
	}
	updated, err := s.repo.Update(ctx, strings.TrimSpace(id), item)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return domainregistry.Connection{}, fmt.Errorf("%w: registry connection not found", apperrors.ErrNotFound)
		}
		return domainregistry.Connection{}, err
	}
	return scrubConnection(updated), nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesManage); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: registry connection id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.Delete(ctx, strings.TrimSpace(id)); err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return fmt.Errorf("%w: registry connection not found", apperrors.ErrNotFound)
		}
		return err
	}
	return nil
}

func normalizeInput(input domainregistry.Input) (domainregistry.Connection, error) {
	name := strings.TrimSpace(input.Name)
	endpoint := strings.TrimSpace(input.Endpoint)
	registryType := strings.TrimSpace(input.RegistryType)
	if name == "" {
		return domainregistry.Connection{}, fmt.Errorf("%w: name is required", apperrors.ErrInvalidArgument)
	}
	if endpoint == "" {
		return domainregistry.Connection{}, fmt.Errorf("%w: endpoint is required", apperrors.ErrInvalidArgument)
	}
	if registryType == "" {
		return domainregistry.Connection{}, fmt.Errorf("%w: registry type is required", apperrors.ErrInvalidArgument)
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	return domainregistry.Connection{
		ID:           strings.TrimSpace(input.ID),
		Name:         name,
		RegistryType: registryType,
		Endpoint:     endpoint,
		Namespace:    strings.TrimSpace(input.Namespace),
		Username:     strings.TrimSpace(input.Username),
		Secret:       strings.TrimSpace(input.Secret),
		Insecure:     input.Insecure,
		Metadata:     metadata,
	}, nil
}

func (s *Service) encryptSecret(item *domainregistry.Connection) error {
	if item == nil || strings.TrimSpace(item.Secret) == "" || secretcrypto.Encrypted(item.Secret) {
		return nil
	}
	if strings.TrimSpace(s.credentialKey) == "" {
		return fmt.Errorf("%w: security.credential_encryption_key is required for registry secrets", apperrors.ErrInvalidArgument)
	}
	encrypted, err := secretcrypto.EncryptString(s.credentialKey, item.Secret)
	if err != nil {
		return fmt.Errorf("%w: encrypt registry secret: %v", apperrors.ErrInvalidArgument, err)
	}
	item.Secret = encrypted
	return nil
}

func scrubConnections(items []domainregistry.Connection) []domainregistry.Connection {
	out := make([]domainregistry.Connection, len(items))
	for index, item := range items {
		out[index] = scrubConnection(item)
	}
	return out
}

func scrubConnection(item domainregistry.Connection) domainregistry.Connection {
	secret := strings.TrimSpace(item.Secret)
	secretConfigured := secret != ""
	item.Secret = ""
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	item.Metadata["secretConfigured"] = secretConfigured
	item.Metadata["secretStorage"] = string(secretcrypto.SecretStorageLabel(secret))
	return item
}

func (s *Service) migrateLegacySecrets(ctx context.Context, items []domainregistry.Connection) {
	if s == nil || strings.TrimSpace(s.credentialKey) == "" || len(items) == 0 {
		return
	}
	for index := range items {
		secret := strings.TrimSpace(items[index].Secret)
		if secret == "" || secretcrypto.Encrypted(secret) {
			continue
		}
		encrypted, err := secretcrypto.EncryptString(s.credentialKey, secret)
		if err != nil {
			continue
		}
		updated := items[index]
		updated.Secret = encrypted
		if _, err = s.repo.Update(ctx, updated.ID, updated); err != nil {
			continue
		}
	}
}

func normalizeID(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return strings.ToLower(strings.ReplaceAll(value, " ", "-"))
	}
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		return uuid.NewString()
	}
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(fallback, " ", "-"), "_", "-"))
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}
