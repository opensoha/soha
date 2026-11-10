package aigateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) prepareTokenRotation(ctx context.Context, principal domainidentity.Principal, kind, previousID, requestID string) (string, error) {
	if s.tokenRotations == nil {
		return "", apperrors.ErrServiceUnavailable
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return uuid.NewString(), nil
	}
	parsed, err := uuid.Parse(requestID)
	if err != nil || len(requestID) != 36 {
		return "", apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_request_id", "A UUID rotationRequestId is required", "")
	}
	requestID = parsed.String()
	record, err := s.GetTokenRotation(ctx, principal, requestID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return requestID, nil
	}
	if err != nil {
		return "", err
	}
	if record.TokenKind != kind || record.PreviousTokenID != previousID || record.ActorID != principal.UserID {
		return "", apperrors.ErrConflict
	}
	return "", apperrors.NewBusiness(apperrors.ErrConflict, "token_rotation_completed", "This Key was already rotated; query the rotation record using an independent credential", "")
}
func newTokenRotation(principal domainidentity.Principal, id, kind, owner, previous, replacement string, at time.Time) domainaigateway.TokenRotationRecord {
	return domainaigateway.TokenRotationRecord{RotationRequestID: id, TokenKind: kind, OwnerID: owner, ActorID: principal.UserID, PreviousTokenID: previous, ReplacementTokenID: replacement, Status: "completed", RotatedAt: at}
}
func (s *Service) GetTokenRotation(ctx context.Context, principal domainidentity.Principal, id string) (domainaigateway.TokenRotationRecord, error) {
	var empty domainaigateway.TokenRotationRecord
	if s.tokenRotations == nil {
		return empty, apperrors.ErrServiceUnavailable
	}
	parsed, err := uuid.Parse(id)
	if err != nil || len(id) != 36 {
		return empty, apperrors.ErrInvalidArgument
	}
	record, err := s.tokenRotations.GetTokenRotation(ctx, parsed.String())
	if err != nil {
		return empty, err
	}
	permission := "ai.gateway.tokens.view"
	if record.TokenKind == "personal_access_token" && record.OwnerID == principal.UserID {
		permission = appaccess.PermAIGatewayInvoke
	} else if record.TokenKind == "service_account_token" && record.ActorID == principal.UserID {
		permission = appaccess.ManagedActionPermission(appaccess.PermAIGatewayTokensManage, "rotate")
	}
	allowed, err := s.hasRuntimePermission(ctx, principal, permission)
	if err != nil {
		return empty, err
	}
	if !allowed {
		return empty, apperrors.ErrNotFound
	}
	return record, nil
}
