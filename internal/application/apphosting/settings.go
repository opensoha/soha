package apphosting

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) GetSettings(ctx context.Context, principal domainidentity.Principal) (sohaapi.AppHostingSettings, error) {
	if err := s.authorizeAny(ctx, principal, appaccess.PermAppHostingAdminView, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.AppHostingSettings{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return sohaapi.AppHostingSettings{}, err
	}
	return settings.ToAPI(s.fallbackGatewayTokenSHA256 != ""), nil
}

func (s *Service) UpdateSettings(ctx context.Context, principal domainidentity.Principal, req sohaapi.AppHostingSettingsUpdateRequest) (sohaapi.AppHostingSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.AppHostingSettings{}, err
	}
	now := s.now()
	actor := firstNonEmpty(principal.UserID, principal.UserName)

	updated, err := s.repo.SaveSettings(ctx, func(current domainapphosting.Settings) (domainapphosting.Settings, error) {
		next := mergeSettingsUpdate(current.WithDefaults(), req)
		next.UpdatedBy = actor
		next.UpdatedAt = &now
		if err := next.Validate(); err != nil {
			return domainapphosting.Settings{}, err
		}
		return next, nil
	})
	if err != nil {
		return sohaapi.AppHostingSettings{}, err
	}
	s.recordAudit(ctx, principal, "settings.update", "success", "default", "Updated workspace App Hosting settings", map[string]any{
		"enabled":                     updated.Enabled,
		"appsDomain":                  updated.AppsDomain,
		"storageIntegrationId":        updated.StorageIntegrationID,
		"allowOrganizationVisibility": updated.AllowOrganizationVisibility,
	})
	return updated.ToAPI(s.fallbackGatewayTokenSHA256 != ""), nil
}

func (s *Service) RotateGatewayToken(ctx context.Context, principal domainidentity.Principal, reason string) (sohaapi.AppHostingGatewayTokenSecret, error) {
	if err := s.authorize(ctx, principal, appaccess.PermAppHostingAdminManage); err != nil {
		return sohaapi.AppHostingGatewayTokenSecret{}, err
	}
	trimmedReason := strings.TrimSpace(reason)
	if len(trimmedReason) > 512 {
		return sohaapi.AppHostingGatewayTokenSecret{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_reason", "reason must not exceed 512 characters", "轮换原因长度不能超过 512 个字符")
	}
	rawToken, err := generateOpaqueToken("soha_ahgw_", 32)
	if err != nil {
		return sohaapi.AppHostingGatewayTokenSecret{}, err
	}
	tokenHash := sha256HexString(rawToken)
	now := s.now()
	actor := firstNonEmpty(principal.UserID, principal.UserName)

	_, err = s.repo.SaveSettings(ctx, func(current domainapphosting.Settings) (domainapphosting.Settings, error) {
		next := current.WithDefaults()
		next.GatewayTokenSHA256 = tokenHash
		next.GatewayTokenRotatedAt = &now
		next.UpdatedBy = actor
		next.UpdatedAt = &now
		return next, nil
	})
	if err != nil {
		return sohaapi.AppHostingGatewayTokenSecret{}, err
	}
	s.recordAudit(ctx, principal, "settings.gateway_token.rotate", "success", "default", "Rotated App Hosting gateway bearer token", map[string]any{
		"reason": trimmedReason,
	})
	return sohaapi.AppHostingGatewayTokenSecret{
		Token:     rawToken,
		RotatedAt: now,
	}, nil
}

func (s *Service) verifyGatewayToken(ctx context.Context, rawBearerToken string) (domainapphosting.Settings, error) {
	trimmed := strings.TrimSpace(rawBearerToken)
	if trimmed == "" {
		return domainapphosting.Settings{}, apperrors.ErrUnauthorized
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return domainapphosting.Settings{}, err
	}
	settings = settings.WithDefaults()
	candidateHash := sha256HexString(trimmed)
	if constantTimeHexEqual(candidateHash, settings.GatewayTokenSHA256) {
		return settings, nil
	}
	if settings.GatewayTokenSHA256 == "" && constantTimeHexEqual(candidateHash, s.fallbackGatewayTokenSHA256) {
		return settings, nil
	}
	return domainapphosting.Settings{}, apperrors.ErrUnauthorized
}

func mergeSettingsUpdate(base domainapphosting.Settings, req sohaapi.AppHostingSettingsUpdateRequest) domainapphosting.Settings {
	next := base
	next.Enabled = req.Enabled
	if trimmed := strings.TrimSpace(req.AppsDomain); trimmed != "" {
		next.AppsDomain = domainapphosting.NormalizeAppsDomain(trimmed)
	}
	if trimmed := strings.TrimSpace(req.PublicScheme); trimmed != "" {
		next.PublicScheme = strings.ToLower(trimmed)
	}
	if trimmed := strings.TrimSpace(req.StorageIntegrationID); trimmed != "" {
		next.StorageIntegrationID = trimmed
	}
	if trimmed := strings.TrimSpace(req.StoragePrefix); trimmed != "" {
		next.StoragePrefix = strings.Trim(trimmed, "/")
	}
	if req.AllowOrganizationVisibility != nil {
		next.AllowOrganizationVisibility = *req.AllowOrganizationVisibility
	}
	if req.MaxBundleCompressedBytes > 0 {
		next.MaxBundleCompressedBytes = req.MaxBundleCompressedBytes
	}
	if req.MaxBundleUncompressedBytes > 0 {
		next.MaxBundleUncompressedBytes = req.MaxBundleUncompressedBytes
	}
	if req.MaxBundleFileCount > 0 {
		next.MaxBundleFileCount = req.MaxBundleFileCount
	}
	if req.MaxAppsPerOwner > 0 {
		next.MaxAppsPerOwner = req.MaxAppsPerOwner
	}
	if req.RetainedRevisionsPerApp > 0 {
		next.RetainedRevisionsPerApp = req.RetainedRevisionsPerApp
	}
	return next.WithDefaults()
}

func generateOpaqueToken(prefix string, numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return prefix + hex.EncodeToString(buf), nil
}

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func constantTimeHexEqual(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
