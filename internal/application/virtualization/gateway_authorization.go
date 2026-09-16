package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) SetGatewayExecutionAuthorizer(authorize func(context.Context, domainidentity.Principal, domainaigateway.ExecutionAuthorization) error) {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	s.authorizeGatewayExecution = authorize
}

func (s *Service) sealGatewayExecution(ctx context.Context, payload map[string]any) error {
	authorization, ok := domainaigateway.ExecutionAuthorizationFrom(ctx)
	if !ok {
		return nil
	}
	if s.credentialKeys.Active().ID() == "" && strings.TrimSpace(s.credentialKey) == "" {
		return fmt.Errorf("%w: gateway queue authorization requires credential encryption", apperrors.ErrInvalidArgument)
	}
	encoded, err := json.Marshal(authorization)
	if err != nil || len(encoded) > 2<<20 {
		return fmt.Errorf("%w: invalid gateway queue authorization", apperrors.ErrInvalidArgument)
	}
	sealed, err := s.encryptCredentialJSON(map[string]any{"authorization": string(encoded)})
	if err != nil {
		return fmt.Errorf("%w: cannot seal gateway queue authorization", apperrors.ErrInvalidArgument)
	}
	payload["gatewayAuthorizationCredential"] = sealed
	return nil
}

func (s *Service) checkQueuedGatewayAuthorization(ctx context.Context, principal domainidentity.Principal, task domain.Task) error {
	sealed := payloadString(task.Payload, "gatewayAuthorizationCredential")
	if sealed == "" {
		return nil // Direct domain API and pre-existing queue records.
	}
	values, err := s.decryptCredentialJSON(sealed)
	var authorization domainaigateway.ExecutionAuthorization
	if err != nil || json.Unmarshal([]byte(payloadString(values, "authorization")), &authorization) != nil {
		return fmt.Errorf("%w: queued gateway authorization is unavailable", apperrors.ErrAccessDenied)
	}
	s.workerMu.Lock()
	authorize := s.authorizeGatewayExecution
	s.workerMu.Unlock()
	if authorize == nil {
		return fmt.Errorf("%w: gateway execution authorization is unavailable", apperrors.ErrAccessDenied)
	}
	if err := authorize(ctx, principal, authorization); err != nil {
		// Authorization/provider input must not be reflected into task logs.
		return fmt.Errorf("%w: current gateway authorization blocks VM execution", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) prepareRetryAuthorization(ctx context.Context, principal domainidentity.Principal, task *domain.Task) error {
	if _, fromGateway := domainaigateway.ExecutionAuthorizationFrom(ctx); !fromGateway {
		if err := s.checkQueuedGatewayAuthorization(ctx, principal, *task); err != nil {
			return err
		}
	}
	if err := s.sealGatewayExecution(ctx, task.Payload); err != nil {
		return err
	}
	return s.resetUnstartedWorkerBootstrap(ctx, task)
}
