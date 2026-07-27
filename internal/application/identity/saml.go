package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type SAMLAuthnRequest struct {
	ID          string
	RedirectURL string
}

type SAMLAssertion struct {
	ID         string
	Subject    string
	Attributes map[string][]string
}

type SAMLLoginRuntime interface {
	Begin(context.Context, domainsettings.LoginProviderSettings, string) (SAMLAuthnRequest, error)
	Validate(context.Context, domainsettings.LoginProviderSettings, string, string) (SAMLAssertion, error)
	Metadata(context.Context, domainsettings.LoginProviderSettings) ([]byte, error)
}

func (s *Service) beginSAMLLogin(ctx context.Context, provider domainsettings.LoginProviderSettings, returnTo, linkUserID string) (string, error) {
	if s.saml == nil {
		return "", fmt.Errorf("%w: saml login runtime is not enabled", apperrors.ErrUnsupportedOperation)
	}
	state := uuid.NewString()
	request, err := s.saml.Begin(ctx, provider, state)
	if err != nil {
		return "", fmt.Errorf("build saml authn request: %w", err)
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.RedirectURL) == "" {
		return "", fmt.Errorf("%w: saml runtime returned an invalid authn request", apperrors.ErrInvalidArgument)
	}
	if err := s.ephemeralTokens.CreateEphemeralToken(ctx, domainidentity.EphemeralToken{
		Token: state,
		Kind:  samlStateKind,
		Payload: loginStatePayload(map[string]any{
			"providerId": provider.ID,
			"type":       provider.Type,
			"linkUserId": linkUserID,
			"requestId":  request.ID,
		}, returnTo),
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}); err != nil {
		return "", fmt.Errorf("store saml state: %w", err)
	}
	return request.RedirectURL, nil
}

func (s *Service) HandleSAMLResponse(ctx context.Context, providerID, encodedResponse, relayState string) (string, error) {
	if s.saml == nil {
		return "", fmt.Errorf("%w: saml login runtime is not enabled", apperrors.ErrUnsupportedOperation)
	}
	if strings.TrimSpace(encodedResponse) == "" || strings.TrimSpace(relayState) == "" {
		return "", fmt.Errorf("%w: SAMLResponse and RelayState are required", apperrors.ErrInvalidArgument)
	}
	state, err := s.ephemeralTokens.ConsumeEphemeralToken(ctx, relayState, samlStateKind)
	if err != nil {
		return "", fmt.Errorf("%w: saml request is unknown or expired", apperrors.ErrUnauthorized)
	}
	if stateProviderID, _ := state.Payload["providerId"].(string); stateProviderID != providerID {
		return "", fmt.Errorf("%w: saml provider mismatch", apperrors.ErrUnauthorized)
	}
	provider, err := s.resolveLoginProvider(ctx, providerID)
	if err != nil || !provider.Enabled || provider.Type != "saml" {
		return "", fmt.Errorf("%w: saml login provider is unavailable", apperrors.ErrNotFound)
	}
	requestID, _ := state.Payload["requestId"].(string)
	assertion, err := s.saml.Validate(ctx, provider, encodedResponse, requestID)
	if err != nil {
		return "", fmt.Errorf("%w: invalid saml response", apperrors.ErrUnauthorized)
	}
	if strings.TrimSpace(assertion.ID) == "" || strings.TrimSpace(assertion.Subject) == "" {
		return "", fmt.Errorf("%w: saml assertion subject is missing", apperrors.ErrUnauthorized)
	}
	if err := s.ephemeralTokens.CreateEphemeralToken(ctx, domainidentity.EphemeralToken{
		Token: assertion.ID, Kind: samlAssertionKind,
		Payload: map[string]any{"providerId": provider.ID}, ExpiresAt: time.Now().UTC().Add(12 * time.Hour),
	}); err != nil {
		return "", fmt.Errorf("%w: saml assertion was already consumed", apperrors.ErrUnauthorized)
	}
	returnTo, err := stateReturnTo(state.Payload)
	if err != nil {
		return "", err
	}
	profile := samlProfile(provider, assertion)
	if linkUserID, _ := state.Payload["linkUserId"].(string); strings.TrimSpace(linkUserID) != "" {
		if err := s.linkExternalIdentity(ctx, linkUserID, provider, profile); err != nil {
			return "", err
		}
		return linkedIdentityRedirect(returnTo, provider.ID)
	}
	principal, err := s.reconcileExternalUser(ctx, provider, profile)
	if err != nil {
		return "", err
	}
	return s.completeExternalLogin(ctx, principal, provider, returnTo)
}

func (s *Service) SAMLMetadata(ctx context.Context, providerID string) ([]byte, error) {
	if s.saml == nil {
		return nil, fmt.Errorf("%w: saml login runtime is not enabled", apperrors.ErrUnsupportedOperation)
	}
	provider, err := s.resolveLoginProvider(ctx, providerID)
	if err != nil || !provider.Enabled || provider.Type != "saml" {
		return nil, fmt.Errorf("%w: saml login provider is unavailable", apperrors.ErrNotFound)
	}
	return s.saml.Metadata(ctx, provider)
}

func samlProfile(provider domainsettings.LoginProviderSettings, assertion SAMLAssertion) genericProfile {
	first := func(name string) string {
		values := assertion.Attributes[name]
		if len(values) == 0 {
			return ""
		}
		return strings.TrimSpace(values[0])
	}
	emailField := firstNonEmpty(provider.EmailField, "email")
	nameField := firstNonEmpty(provider.UserNameField, "displayName", "name")
	raw := make(map[string]any, len(assertion.Attributes)+1)
	raw["subject"] = assertion.Subject
	for name, values := range assertion.Attributes {
		raw[name] = append([]string(nil), values...)
	}
	return genericProfile{ID: assertion.Subject, Email: first(emailField), Name: first(nameField), Raw: raw, Provider: provider.ID}
}

func (s *Service) completeExternalLogin(ctx context.Context, principal domainidentity.Principal, provider domainsettings.LoginProviderSettings, returnTo string) (string, error) {
	result, err := s.issueAuthResult(ctx, principal, provider.Type)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(oidcExchangePayload{Result: result})
	if err != nil {
		return "", fmt.Errorf("marshal external login exchange payload: %w", err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return "", fmt.Errorf("decode external login exchange payload: %w", err)
	}
	exchangeCode := uuid.NewString()
	if err := s.ephemeralTokens.CreateEphemeralToken(ctx, domainidentity.EphemeralToken{
		Token: exchangeCode, Kind: oidcExchangeKind, Payload: payloadMap,
		ExpiresAt: time.Now().UTC().Add(2 * time.Minute),
	}); err != nil {
		return "", fmt.Errorf("store external login exchange payload: %w", err)
	}
	redirectURL, err := addQueryValue(provider.FrontendRedirectURL, "code", exchangeCode)
	if err != nil {
		return "", err
	}
	return addReturnToQuery(redirectURL, returnTo)
}

const (
	samlStateKind     = "saml_state"
	samlAssertionKind = "saml_assertion"
)
