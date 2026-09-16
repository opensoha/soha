package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type operationAuthorization struct {
	ActorID string                           `json:"actorId"`
	TokenID string                           `json:"tokenId,omitempty"`
	Gateway *domainai.ExecutionAuthorization `json:"gateway,omitempty"`
}

func (s *Service) SetGatewayExecutionAuthorizer(resolve func(context.Context, string, string) (domainidentity.Principal, error), authorize func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error) {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	s.executionPrincipals, s.authorizeGatewayExecution = resolve, authorize
}

func (s *Service) sealOperationAuthorization(ctx context.Context, principal domainidentity.Principal) (string, error) {
	gateway, fromGateway := domainai.ExecutionAuthorizationFrom(ctx)
	s.executionMu.RLock()
	configured := s.executionPrincipals != nil
	s.executionMu.RUnlock()
	if !configured && !fromGateway {
		return "", nil
	} // Legacy domain-only deployments.
	if principal.UserID == "" || fromGateway && gateway.ActorID != principal.UserID {
		return "", fmt.Errorf("%w: Docker execution actor is unavailable", apperrors.ErrAccessDenied)
	}
	authorization := operationAuthorization{ActorID: principal.UserID, TokenID: principal.AccessTokenID}
	if fromGateway {
		authorization.Gateway = &gateway
	}
	encoded, err := json.Marshal(authorization)
	if err != nil || len(encoded) > 2<<20 {
		return "", fmt.Errorf("%w: invalid Docker execution authorization", apperrors.ErrInvalidArgument)
	}
	sealed, err := secretcrypto.EncryptStringWithKeyring(s.credentialEncryptionKeys, string(encoded))
	if err != nil {
		return "", fmt.Errorf("%w: Docker execution requires credential encryption", apperrors.ErrInvalidArgument)
	}
	return sealed, nil
}

func (s *Service) authorizeQueuedOperation(ctx context.Context, item domain.Operation) error {
	if item.ExecutionAuthorization == "" {
		return nil
	} // Existing queue records have no recoverable token evidence.
	denied := fmt.Errorf("%w: current authorization blocks Docker execution", apperrors.ErrAccessDenied)
	encoded, err := secretcrypto.DecryptStringWithKeyring(s.credentialEncryptionKeys, item.ExecutionAuthorization)
	var authorization operationAuthorization
	if err != nil || json.Unmarshal([]byte(encoded), &authorization) != nil || authorization.ActorID == "" || authorization.ActorID != item.RequestedBy {
		return denied
	}
	s.executionMu.RLock()
	resolve, checkGateway := s.executionPrincipals, s.authorizeGatewayExecution
	s.executionMu.RUnlock()
	if resolve == nil {
		return denied
	}
	principal, err := resolve(ctx, authorization.ActorID, authorization.TokenID)
	if err != nil || principal.UserID != authorization.ActorID {
		return denied
	}
	permission := operationExecutionPermission(item)
	if permission == "" || s.authorize(ctx, principal, permission) != nil {
		return denied
	}
	if authorization.Gateway != nil && (checkGateway == nil || checkGateway(ctx, principal, *authorization.Gateway) != nil) {
		return denied
	}
	return nil
}

func operationExecutionPermission(item domain.Operation) string {
	switch item.OperationKind {
	case OperationKindHostProvision:
		return appaccess.ManagedActionPermission(appaccess.PermDockerHostsManage, "create")
	case OperationKindProjectDeploy:
		return appaccess.PermDockerProjectsDeploy
	case OperationKindContainerStart:
		return appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, "start")
	case OperationKindServiceAction:
		return appaccess.ManagedActionPermission(appaccess.PermDockerServicesManage, stringValue(item.Payload, "action"))
	case OperationKindPortReserve:
		return appaccess.ManagedActionPermission(appaccess.PermDockerPortsManage, "create")
	case OperationKindHostSync:
		return appaccess.ManagedActionPermission(appaccess.PermDockerHostsManage, "update")
	default:
		return ""
	}
}

func (s *Service) denyClaimedOperation(ctx context.Context, item domain.Operation) error {
	now := time.Now().UTC()
	item.Status, item.FinishedAt = OperationStatusFailed, &now
	item.Result = mergeMap(item.Result, map[string]any{"executionDeniedBeforeDispatch": true, "message": "current authorization blocks Docker runner dispatch"})
	_, err := s.repo.UpdateOperation(ctx, item)
	return err
}

func (s *Service) stopUnauthorizedOperation(ctx context.Context, item domain.Operation) (domain.Operation, error) {
	if item.Status != OperationStatusRunning || s.authorizeQueuedOperation(ctx, item) == nil {
		return item, nil
	}
	// Keep cancellation pending until the original runner confirms command exit.
	item.Status, item.FinishedAt = OperationStatusCanceling, nil
	item.Result = mergeMap(item.Result, map[string]any{"cancellationRequestedAt": time.Now().UTC().Format(time.RFC3339), "cancelReason": "current authorization blocks Docker execution"})
	return s.repo.UpdateOperation(ctx, item)
}
