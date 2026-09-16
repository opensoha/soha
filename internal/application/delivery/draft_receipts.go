package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func deliveryDraftCreationDigest(input domaindelivery.DeliveryDraftInput) (string, error) {
	if input.IdempotencyKey == "" {
		return "", nil
	}
	if len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.IdempotencyKey) == "" {
		return "", fmt.Errorf("%w: invalid draft creation key", apperrors.ErrInvalidArgument)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (s *Service) FindDeliveryDraftCreation(ctx context.Context, principal domainidentity.Principal, input domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	digest, err := deliveryDraftCreationDigest(input)
	if err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	if digest == "" || principal.UserID == "" {
		return domaindelivery.DeliveryDraft{}, apperrors.ErrInvalidArgument
	}
	draft, err := s.repository.FindDeliveryDraftCreation(ctx, principal.UserID, input.IdempotencyKey, digest)
	if err == nil { err = domaindelivery.CheckDraftScope(ctx, draft) }
	return draft, err
}

func (s *Service) GetDeliveryDraftApplication(ctx context.Context, principal domainidentity.Principal, id string) (domainapp.App, error) {
	return s.applications.Get(ctx, principal, id)
}

func (s *Service) GetDeliveryDraftConfirmation(ctx context.Context, principal domainidentity.Principal, id string) (domaindelivery.DeliveryDraftConfirmResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationsUpdate); err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	receipt, err := s.repository.GetDeliveryDraftConfirmation(ctx, strings.TrimSpace(id))
	if err != nil {
		return receipt, err
	}
	if err := domaindelivery.CheckDraftScope(ctx, receipt.Draft); err != nil { return receipt, err }
	return receipt, s.authorizeDraftReceipt(ctx, principal, receipt)
}

func (s *Service) authorizeDraftReceipt(ctx context.Context, principal domainidentity.Principal, receipt domaindelivery.DeliveryDraftConfirmResult) error {
	if receipt.Application.ID == "" {
		return apperrors.ErrConflict
	}
	if _, err := s.applications.Get(ctx, principal, receipt.Application.ID); err != nil {
		return err
	}
	if len(receipt.Services) > 0 {
		if _, err := s.applications.ListServices(ctx, principal, receipt.Application.ID); err != nil {
			return err
		}
	}
	for _, binding := range receipt.EnvironmentBindings {
		if _, err := s.catalog.GetApplicationEnvironment(ctx, principal, binding.ID); err != nil {
			return err
		}
	}
	return nil
}
