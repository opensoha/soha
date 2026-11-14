package identityprovider

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const oidcAuthorizationSourceKey = "authorizationSourceIp"

func canonicalAuthorizationSource(raw string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || addr.Zone() != "" {
		return ""
	}
	return addr.Unmap().String()
}

func (s *Service) currentApplicationAccess(ctx context.Context, userID string, application domainportal.Application, sessionID string) (domainidentity.Principal, error) {
	return s.currentApplicationAccessFrom(ctx, userID, application, sessionID, requestctx.FromContext(ctx).SourceIP)
}

// currentApplicationAccessFrom never treats a cached principal or an earlier MFA
// decision as current proof. Missing legacy session bindings carry no MFA proof.
func (s *Service) currentApplicationAccessFrom(ctx context.Context, userID string, application domainportal.Application, sessionID, sourceIP string) (domainidentity.Principal, error) {
	principal, err := s.loadPrincipal(ctx, userID)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	if application.Status != domainportal.ApplicationStatusEnabled {
		return domainidentity.Principal{}, fmt.Errorf("%w: application is disabled", apperrors.ErrAccessDenied)
	}
	access := domainportal.AccessPolicyContext{SourceIP: sourceIP, Now: time.Now().UTC()}
	if strings.TrimSpace(sessionID) != "" {
		session, err := s.users.GetAuthSessionByID(ctx, sessionID)
		if err != nil {
			return domainidentity.Principal{}, fmt.Errorf("%w: platform session is invalid", apperrors.ErrUnauthorized)
		}
		if session.UserID != principal.UserID || session.Status != "active" || !session.ExpiresAt.After(access.Now) {
			return domainidentity.Principal{}, fmt.Errorf("%w: platform session is invalid", apperrors.ErrUnauthorized)
		}
		access.MFAAuthenticated = metadataMFA(session.Metadata)
	}
	if err := applicationPolicyAccessError(principal, application, access); err != nil {
		return domainidentity.Principal{}, err
	}
	return principal, nil
}

func (s *Service) currentOIDCAccess(ctx context.Context, clientID, providerID, userID, platformSessionID, authorizationSourceIP string) (domainidentity.Principal, error) {
	client, err := s.repo.GetOIDCClientByClientID(ctx, clientID)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	if !clientEnabled(client) || client.ProviderID != providerID {
		return domainidentity.Principal{}, fmt.Errorf("%w: oidc client is disabled or changed", apperrors.ErrUnauthorized)
	}
	provider, err := s.requireOIDCProvider(ctx, providerID)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	if !providerEnabled(provider) {
		return domainidentity.Principal{}, fmt.Errorf("%w: oidc provider is disabled", apperrors.ErrUnauthorized)
	}
	application, err := s.repo.GetProviderApplication(ctx, provider.ID)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	return s.currentApplicationAccessFrom(ctx, userID, application, platformSessionID, authorizationSourceIP)
}

func (s *Service) validateCurrentOIDCSession(ctx context.Context, sessionID, clientID, userID string) error {
	session, err := s.repo.GetOIDCSession(ctx, sessionID, time.Now().UTC())
	if err != nil {
		return err
	}
	if session.ClientID != clientID || session.UserID != userID {
		return fmt.Errorf("%w: oidc session is invalid", apperrors.ErrUnauthorized)
	}
	_, err = s.currentOIDCAccess(ctx, clientID, session.ProviderID, userID, session.PlatformSessionID, session.AuthorizationSourceIP)
	return err
}

// A refresh/code exchange cannot perform interactive step-up authentication.
func nonInteractiveAuthorizationError(err error) error {
	if errors.Is(err, apperrors.ErrMFARequired) {
		return fmt.Errorf("%w: %w", apperrors.ErrUnauthorized, err)
	}
	return err
}
