package aigateway

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *memoryGatewayRepository) GetTokenRotation(_ context.Context, id string) (domainaigateway.TokenRotationRecord, error) {
	record, ok := r.tokenRotations[id]
	if !ok {
		return record, apperrors.ErrNotFound
	}
	return record, nil
}
func (r *memoryGatewayRepository) saveRotation(record domainaigateway.TokenRotationRecord) {
	if r.tokenRotations == nil {
		r.tokenRotations = map[string]domainaigateway.TokenRotationRecord{}
	}
	r.tokenRotations[record.RotationRequestID] = record
}
func (r *memoryGatewayRepository) RotatePersonalToken(ctx context.Context, old string, replacement domainaigateway.PersonalAccessToken, record domainaigateway.TokenRotationRecord) (domainaigateway.PersonalAccessToken, error) {
	if _, err := r.GetTokenRotation(ctx, record.RotationRequestID); err == nil {
		return replacement, apperrors.ErrConflict
	}
	if err := r.RevokePersonalAccessToken(ctx, replacement.UserID, old); err != nil {
		return replacement, err
	}
	created, err := r.CreatePersonalAccessToken(ctx, replacement)
	if err == nil {
		r.saveRotation(record)
	}
	return created, err
}
func (r *memoryGatewayRepository) RotateServiceToken(ctx context.Context, old string, replacement domainaigateway.ServiceAccountToken, record domainaigateway.TokenRotationRecord) (domainaigateway.ServiceAccountToken, error) {
	if _, err := r.GetTokenRotation(ctx, record.RotationRequestID); err == nil {
		return replacement, apperrors.ErrConflict
	}
	if err := r.RevokeServiceAccountToken(ctx, old); err != nil {
		return replacement, err
	}
	created, err := r.CreateServiceAccountToken(ctx, replacement)
	if err == nil {
		r.saveRotation(record)
	}
	return created, err
}
func TestTokenRotationRecoveryNeverReplaysValue(t *testing.T) {
	ctx := context.Background()
	repo := &memoryGatewayRepository{personalTokens: []domainaigateway.PersonalAccessToken{{ID: "old", UserID: "user-1", Name: "terminal"}}}
	permissions := appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermAIGatewayInvoke}}})
	service := newTestService(permissions, nil, repo)
	id := uuid.NewString()
	created, err := service.RotatePersonalAccessToken(ctx, testPrincipal("developer"), "old", domainaigateway.TokenRotationInput{RotationRequestID: id})
	if err != nil || created.Value == "" {
		t.Fatal("rotation failed", err)
	}
	record, err := service.GetTokenRotation(ctx, testPrincipal("developer"), id)
	if err != nil || record.ReplacementTokenID != created.Token.ID || record.NewValueRetrievable {
		t.Fatal("invalid recovery record", err)
	}
	_, err = service.RotatePersonalAccessToken(ctx, testPrincipal("developer"), "old", domainaigateway.TokenRotationInput{RotationRequestID: id})
	var business *apperrors.BusinessError
	if !errors.As(err, &business) || business.Code() != "token_rotation_completed" || len(repo.personalTokens) != 2 {
		t.Fatal("repeat rotation must not mint another value", err)
	}
	outsider := testPrincipal("developer")
	outsider.UserID = "other"
	if _, err = service.GetTokenRotation(ctx, outsider, id); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("cross-owner recovery leaked record", err)
	}
}
