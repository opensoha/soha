package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	desktopAuthAttemptKind  = "desktop_auth_attempt"
	desktopAuthCallbackKind = "desktop_auth_callback"
)

var (
	desktopCallbackPathPattern = regexp.MustCompile(`^/callback/[A-Za-z0-9_-]{16,128}$`)
	desktopVerifierPattern     = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

type desktopAuthCompletion struct {
	ReturnTo      string
	AttemptID     string
	RedirectURI   string
	CodeChallenge string
}

type desktopAuthCallbackPayload struct {
	AttemptID     string                   `json:"attemptId"`
	CodeChallenge string                   `json:"codeChallenge"`
	Principal     domainidentity.Principal `json:"principal"`
	ProviderID    string                   `json:"providerId"`
	ProviderType  string                   `json:"providerType"`
}

func (s *Service) CreateDesktopAuthAttempt(ctx context.Context, input domainidentity.DesktopAuthAttemptCreate) (domainidentity.DesktopAuthAttempt, error) {
	providerID := strings.TrimSpace(input.ProviderID)
	provider, err := s.resolveLoginProvider(ctx, providerID)
	if err != nil || !provider.Enabled || provider.Type == "password" || provider.Type == "saml" && s.saml == nil {
		return domainidentity.DesktopAuthAttempt{}, desktopAuthBusinessError(
			apperrors.ErrNotFound,
			"desktop_auth_provider_unavailable",
			"desktop login provider is unavailable",
			"桌面登录提供商不可用",
		)
	}
	switch provider.Type {
	case "oidc", "oauth2", "feishu", "dingtalk", "wecom", "saml":
	default:
		return domainidentity.DesktopAuthAttempt{}, desktopAuthBusinessError(
			apperrors.ErrInvalidArgument,
			"desktop_auth_provider_unsupported",
			"desktop login provider is not supported",
			"桌面登录不支持此提供商",
		)
	}
	redirectURI, err := normalizeDesktopRedirectURI(input.RedirectURI)
	if err != nil {
		return domainidentity.DesktopAuthAttempt{}, err
	}
	if input.CodeChallengeMethod != "S256" || !validDesktopCodeChallenge(input.CodeChallenge) {
		return domainidentity.DesktopAuthAttempt{}, desktopAuthBusinessError(
			apperrors.ErrInvalidArgument,
			"desktop_auth_invalid_challenge",
			"desktop login requires a valid S256 code challenge",
			"桌面登录需要有效的 S256 验证挑战",
		)
	}

	attemptID := uuid.NewString()
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	if err := s.ephemeralTokens.CreateEphemeralToken(ctx, domainidentity.EphemeralToken{
		Token: attemptID,
		Kind:  desktopAuthAttemptKind,
		Payload: map[string]any{
			"providerId":    provider.ID,
			"redirectUri":   redirectURI,
			"codeChallenge": input.CodeChallenge,
		},
		ExpiresAt: expiresAt,
	}); err != nil {
		return domainidentity.DesktopAuthAttempt{}, fmt.Errorf("store desktop auth attempt: %w", err)
	}
	return domainidentity.DesktopAuthAttempt{AttemptID: attemptID, ExpiresAt: expiresAt}, nil
}

func (s *Service) BeginDesktopAuthAttempt(ctx context.Context, attemptID string) (string, error) {
	attemptID = strings.TrimSpace(attemptID)
	if attemptID == "" {
		return "", desktopAuthBusinessError(apperrors.ErrInvalidArgument, "desktop_auth_invalid_attempt", "desktop auth attempt is required", "缺少桌面登录尝试")
	}
	token, err := s.ephemeralTokens.ConsumeEphemeralToken(ctx, attemptID, desktopAuthAttemptKind)
	if err != nil {
		return "", desktopAuthBusinessError(apperrors.ErrNotFound, "desktop_auth_attempt_expired", "desktop auth attempt is unknown or expired", "桌面登录尝试不存在或已过期")
	}
	providerID, _ := token.Payload["providerId"].(string)
	redirectURI, _ := token.Payload["redirectUri"].(string)
	codeChallenge, _ := token.Payload["codeChallenge"].(string)
	redirectURI, err = normalizeDesktopRedirectURI(redirectURI)
	if err != nil || !validDesktopCodeChallenge(codeChallenge) || strings.TrimSpace(providerID) == "" {
		return "", desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_attempt_invalid", "desktop auth attempt is invalid", "桌面登录尝试无效")
	}
	return s.beginProviderAuthorization(ctx, providerID, "", "", &desktopAuthCompletion{
		AttemptID:     attemptID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
	})
}

func (s *Service) ConsumeDesktopAuthAttempt(ctx context.Context, attemptID, code, codeVerifier string) (domainidentity.AuthResult, error) {
	attemptID = strings.TrimSpace(attemptID)
	code = strings.TrimSpace(code)
	if attemptID == "" || code == "" || !desktopVerifierPattern.MatchString(codeVerifier) {
		return domainidentity.AuthResult{}, desktopAuthBusinessError(apperrors.ErrInvalidArgument, "desktop_auth_invalid_exchange", "desktop auth exchange payload is invalid", "桌面登录交换参数无效")
	}
	token, err := s.ephemeralTokens.ConsumeEphemeralToken(ctx, code, desktopAuthCallbackKind)
	if err != nil {
		return domainidentity.AuthResult{}, desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_code_expired", "desktop auth code is unknown, expired, or consumed", "桌面登录代码不存在、已过期或已使用")
	}
	var payload desktopAuthCallbackPayload
	rawPayload, err := json.Marshal(token.Payload)
	if err != nil || json.Unmarshal(rawPayload, &payload) != nil {
		return domainidentity.AuthResult{}, desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_code_invalid", "desktop auth code is invalid", "桌面登录代码无效")
	}
	expectedChallenge := desktopCodeChallenge(codeVerifier)
	if subtle.ConstantTimeCompare([]byte(payload.AttemptID), []byte(attemptID)) != 1 ||
		subtle.ConstantTimeCompare([]byte(payload.CodeChallenge), []byte(expectedChallenge)) != 1 {
		return domainidentity.AuthResult{}, desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_verification_failed", "desktop auth verification failed", "桌面登录验证失败")
	}
	result, err := s.issueAuthResult(ctx, payload.Principal, payload.ProviderType)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	_ = s.recordAudit(ctx, payload.Principal, "login", "success", "desktop login succeeded", map[string]any{
		"provider": payload.ProviderID, "providerType": payload.ProviderType,
	})
	return result, nil
}

func (s *Service) completeFederatedLogin(
	ctx context.Context,
	principal domainidentity.Principal,
	provider domainsettings.LoginProviderSettings,
	completion desktopAuthCompletion,
) (string, bool, error) {
	if completion.AttemptID != "" {
		redirectURL, err := s.storeDesktopAuthCallback(ctx, principal, provider, completion)
		return redirectURL, true, err
	}
	result, err := s.issueAuthResult(ctx, principal, provider.Type)
	if err != nil {
		return "", false, err
	}
	exchangeCode, err := s.storeOIDCExchange(ctx, result)
	if err != nil {
		return "", false, err
	}
	redirectURL, err := addQueryValue(provider.FrontendRedirectURL, "code", exchangeCode)
	if err != nil {
		return "", false, err
	}
	redirectURL, err = addReturnToQuery(redirectURL, completion.ReturnTo)
	return redirectURL, false, err
}

func (s *Service) storeDesktopAuthCallback(
	ctx context.Context,
	principal domainidentity.Principal,
	provider domainsettings.LoginProviderSettings,
	completion desktopAuthCompletion,
) (string, error) {
	redirectURI, err := normalizeDesktopRedirectURI(completion.RedirectURI)
	if err != nil || !validDesktopCodeChallenge(completion.CodeChallenge) || strings.TrimSpace(completion.AttemptID) == "" {
		return "", desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_attempt_invalid", "desktop auth attempt is invalid", "桌面登录尝试无效")
	}
	code := uuid.NewString()
	payload := desktopAuthCallbackPayload{
		AttemptID:     completion.AttemptID,
		CodeChallenge: completion.CodeChallenge,
		Principal:     principal,
		ProviderID:    provider.ID,
		ProviderType:  provider.Type,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal desktop auth callback: %w", err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(rawPayload, &payloadMap); err != nil {
		return "", fmt.Errorf("decode desktop auth callback: %w", err)
	}
	if err := s.ephemeralTokens.CreateEphemeralToken(ctx, domainidentity.EphemeralToken{
		Token: code, Kind: desktopAuthCallbackKind, Payload: payloadMap,
		ExpiresAt: time.Now().UTC().Add(2 * time.Minute),
	}); err != nil {
		return "", fmt.Errorf("store desktop auth callback: %w", err)
	}
	redirectURL, err := addQueryValue(redirectURI, "attempt", completion.AttemptID)
	if err != nil {
		return "", err
	}
	return addQueryValue(redirectURL, "code", code)
}

func desktopCompletionFromState(payload map[string]any) (desktopAuthCompletion, error) {
	returnTo, err := stateReturnTo(payload)
	if err != nil {
		return desktopAuthCompletion{}, err
	}
	completion := desktopAuthCompletion{ReturnTo: returnTo}
	completion.AttemptID, _ = payload["desktopAttemptId"].(string)
	if completion.AttemptID == "" {
		return completion, nil
	}
	completion.RedirectURI, _ = payload["desktopRedirectUri"].(string)
	completion.CodeChallenge, _ = payload["desktopCodeChallenge"].(string)
	if completion.ReturnTo != "" {
		return desktopAuthCompletion{}, desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_attempt_invalid", "desktop auth attempt is invalid", "桌面登录尝试无效")
	}
	if _, err := normalizeDesktopRedirectURI(completion.RedirectURI); err != nil || !validDesktopCodeChallenge(completion.CodeChallenge) {
		return desktopAuthCompletion{}, desktopAuthBusinessError(apperrors.ErrUnauthorized, "desktop_auth_attempt_invalid", "desktop auth attempt is invalid", "桌面登录尝试无效")
	}
	return completion, nil
}

func addDesktopCompletion(payload map[string]any, completion *desktopAuthCompletion) map[string]any {
	if completion == nil {
		return payload
	}
	payload["desktopAttemptId"] = completion.AttemptID
	payload["desktopRedirectUri"] = completion.RedirectURI
	payload["desktopCodeChallenge"] = completion.CodeChallenge
	return payload
}

func normalizeDesktopRedirectURI(raw string) (string, error) {
	if raw == "" || raw != strings.TrimSpace(raw) {
		return "", desktopAuthBusinessError(apperrors.ErrInvalidArgument, "desktop_auth_invalid_redirect", "desktop redirect URI is invalid", "桌面回调地址无效")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		!desktopCallbackPathPattern.MatchString(parsed.Path) {
		return "", desktopAuthBusinessError(apperrors.ErrInvalidArgument, "desktop_auth_invalid_redirect", "desktop redirect URI is invalid", "桌面回调地址无效")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1024 || port > 65535 || parsed.Host != "127.0.0.1:"+strconv.Itoa(port) {
		return "", desktopAuthBusinessError(apperrors.ErrInvalidArgument, "desktop_auth_invalid_redirect", "desktop redirect URI is invalid", "桌面回调地址无效")
	}
	return parsed.String(), nil
}

func validDesktopCodeChallenge(challenge string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(decoded) == sha256.Size && base64.RawURLEncoding.EncodeToString(decoded) == challenge
}

func desktopCodeChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func desktopAuthBusinessError(kind error, code, english, chinese string) error {
	return apperrors.NewBusiness(kind, code, english, chinese)
}
