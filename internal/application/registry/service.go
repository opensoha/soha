package registry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Service struct {
	repo        domainregistry.Repository
	permissions *appaccess.PermissionResolver
}

func New(repo domainregistry.Repository, permissions *appaccess.PermissionResolver) *Service {
	return &Service{repo: repo, permissions: permissions}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainregistry.Connection, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesView); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, limit)
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
	return s.repo.Create(ctx, item)
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
	updated, err := s.repo.Update(ctx, strings.TrimSpace(id), item)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return domainregistry.Connection{}, fmt.Errorf("%w: registry connection not found", apperrors.ErrNotFound)
		}
		return domainregistry.Connection{}, err
	}
	return updated, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermDeliveryRegistriesManage); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: registry connection id is required", apperrors.ErrInvalidArgument)
	}
	if err := s.repo.Delete(ctx, strings.TrimSpace(id)); err != nil {
		if err == gorm.ErrRecordNotFound {
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
