package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

func (s *Service) SetGatewayExecutionAuthorizer(keys keyring.Ring, authorize func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error) {
	s.gatewayMu.Lock()
	defer s.gatewayMu.Unlock()
	s.gatewayKeys, s.authorizeGatewayExecution = keys, authorize
}

func (s *Service) sealDeliveryGatewayAuthorization(ctx context.Context) (string, error) {
	authorization, ok := domainai.ExecutionAuthorizationFrom(ctx)
	if !ok {
		return "", nil
	}
	encoded, err := json.Marshal(authorization)
	if err != nil || len(encoded) > 2<<20 {
		return "", fmt.Errorf("%w: invalid delivery gateway authorization", apperrors.ErrInvalidArgument)
	}
	s.gatewayMu.RLock()
	keys := s.gatewayKeys
	s.gatewayMu.RUnlock()
	sealed, err := secretcrypto.EncryptStringWithKeyring(keys, string(encoded))
	if err != nil {
		return "", fmt.Errorf("%w: delivery gateway execution requires credential encryption", apperrors.ErrInvalidArgument)
	}
	return sealed, nil
}

func (s *Service) restoreDeliveryGatewayAuthorization(ctx context.Context, principal domainidentity.Principal, sealed string) (context.Context, error) {
	if sealed == "" {
		return ctx, nil
	} // Existing and direct API batches.
	s.gatewayMu.RLock()
	keys, authorize := s.gatewayKeys, s.authorizeGatewayExecution
	s.gatewayMu.RUnlock()
	denied := fmt.Errorf("%w: current gateway authorization blocks delivery execution", apperrors.ErrAccessDenied)
	encoded, err := secretcrypto.DecryptStringWithKeyring(keys, sealed)
	var authorization domainai.ExecutionAuthorization
	if err != nil || json.Unmarshal([]byte(encoded), &authorization) != nil || authorization.ActorID != principal.UserID || authorize == nil {
		return ctx, denied
	}
	if err := authorize(ctx, principal, authorization); err != nil {
		return ctx, denied
	}
	return domainai.WithExecutionAuthorization(ctx, authorization), nil
}
