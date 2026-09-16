package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) deliveryWorkflowCreationDigest(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryWorkflowInput) (string, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return "", err
	}
	if principal.UserID == "" || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.IdempotencyKey) == "" || input.ExpectedVersion != nil {
		return "", fmt.Errorf("%w: workflow creation requires an actor, an 8..128 character key and no expectedVersion", apperrors.ErrInvalidArgument)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// Recover the original create response, never the current, possibly edited
// definition. A subsequent batch must still pin and validate that version.
func (s *Service) FindDeliveryWorkflowCreation(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error) {
	digest, err := s.deliveryWorkflowCreationDigest(ctx, principal, input)
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	item, err := repo.FindDeliveryWorkflowCreation(ctx, principal.UserID, input.IdempotencyKey, digest)
	if err != nil {
		return item, err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, item.Definition.Targets, domainaccess.ActionTrigger); err != nil {
		return item, err
	}
	return s.scopedDeliveryWorkflow(ctx, principal, item)
}

func (s *Service) createDeliveryWorkflowIdempotent(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error) {
	existing, err := s.FindDeliveryWorkflowCreation(ctx, principal, input)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	digest, err := s.deliveryWorkflowCreationDigest(ctx, principal, input)
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	prepared, err := s.PrepareDeliveryWorkflow(ctx, principal, "", input)
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	item, err := s.scopedDeliveryWorkflow(ctx, principal, domainworkflow.DeliveryWorkflow{ID: uuid.NewString(), Definition: prepared.Definition, CreatedBy: principal.UserID})
	if err != nil {
		return item, err
	}
	saved, err := repo.CreateDeliveryWorkflowIdempotent(ctx, item, input.IdempotencyKey, digest)
	if err != nil {
		return saved, err
	}
	return s.scopedDeliveryWorkflow(ctx, principal, saved)
}
