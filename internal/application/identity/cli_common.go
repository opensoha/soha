package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const cliReceiptWindow = 30 * time.Second
const cliSessionTokenPrefix = "cli_session:"
const cliRefreshPrefix = "soha_cli_refresh_"

var cliSecretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var cliVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var cliUserCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

type CLIPollError struct {
	Code    string
	Seconds int
	Pending bool
}

func (e *CLIPollError) Error() string              { return e.Code }
func (e *CLIPollError) RetryAfterSeconds() int     { return e.Seconds }
func (e *CLIPollError) PublicCode() string         { return e.Code }
func (e *CLIPollError) AuthorizationPending() bool { return e.Pending }

func cliError(kind error, code, message string) error {
	return apperrors.NewBusiness(kind, code, message, "")
}
func (s *Service) cliAvailable(creation bool) error {
	if isNilDependency(s.cliState) || isNilDependency(s.cliSessions) || s.cliReceiptKeys.Active().ID() == "" {
		return cliError(apperrors.ErrServiceUnavailable, "cli_auth_unavailable", "CLI authentication is unavailable")
	}
	if creation && !s.cfg.CLI.Enabled {
		return cliError(apperrors.ErrAccessDenied, "cli_auth_disabled", "CLI device and password login are disabled; use an existing Key or contact the administrator")
	}
	return nil
}
func cliRandomSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func cliRandomCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}
func cliHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func cliEqual(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func cliRequestHash(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || len(id) != 36 {
		return "", cliError(apperrors.ErrInvalidArgument, "invalid_request_id", "A UUID request ID is required")
	}
	return cliHash(parsed.String()), nil
}
func (s *Service) cliCodeHash(keyID, handle, code string) (string, error) {
	ring, err := s.jwtKeyring()
	if err != nil {
		return "", err
	}
	key, ok := ring.Find(keyID, s.cliNow().UTC())
	if !ok {
		return "", apperrors.ErrUnauthorized
	}
	mac := hmac.New(sha256.New, []byte(key.Secret()))
	_, _ = mac.Write([]byte("soha-cli-user-code-v2:" + handle + ":" + code))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) cliSourceHash(source string) (string, error) {
	ring, err := s.jwtKeyring()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(ring.Active().Secret()))
	_, _ = mac.Write([]byte("soha-cli-source:" + source))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) cliBudget(ctx context.Context, bucket string, limit int) error {
	allowed, err := s.cliState.ConsumeCLIBudget(ctx, bucket, limit, 5*time.Minute)
	if err != nil {
		return err
	}
	if !allowed {
		return &CLIPollError{Code: "rate_limited", Seconds: 300}
	}
	return nil
}
func cliPKCE(grant domainidentity.CLIGrant, verifier string) bool {
	if !cliVerifierPattern.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return cliEqual(grant.CodeChallenge, base64.RawURLEncoding.EncodeToString(sum[:]))
}
func cliDisplay(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if len(value) > 128 || strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return "", cliError(apperrors.ErrInvalidArgument, "invalid_device_metadata", "Device metadata must be at most 128 bytes without control characters")
	}
	return value, nil
}
func (s *Service) cliPermissions(ctx context.Context, p domainidentity.Principal, requested []string) ([]string, error) {
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, p)
	if err != nil {
		return nil, err
	}
	if len(requested) > 256 {
		return nil, apperrors.ErrInvalidArgument
	}
	if len(requested) > 0 {
		for _, key := range requested {
			if len(key) == 0 || len(key) > 128 || !slices.Contains(keys, key) {
				return nil, cliError(apperrors.ErrAccessDenied, "cli_scope_denied", "Requested permissions exceed the account permissions")
			}
		}
		keys = slices.Clone(requested)
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	if len(keys) == 0 {
		return nil, cliError(apperrors.ErrAccessDenied, "cli_scope_denied", "No permissions are available for this CLI session")
	}
	return keys, nil
}
func (s *Service) requireCLIBrowser(ctx context.Context, p domainidentity.Principal, a domainidentity.AccessContext, origin string) error {
	public, err := url.Parse(s.cfg.CLI.PublicURL)
	if err != nil || canonicalCLIOrigin(origin) == "" || canonicalCLIOrigin(origin) != canonicalCLIOrigin(public.Scheme+"://"+public.Host) {
		return cliError(apperrors.ErrAccessDenied, "invalid_origin", "Open the device link from the configured Soha site")
	}
	if a.TokenKind != "session_access" || a.SessionID == "" || p.UserID == "" {
		return cliError(apperrors.ErrAccessDenied, "browser_session_required", "Sign in with a browser account to authorize a device")
	}
	session, err := s.cliSessions.GetCLIBrowserSession(ctx, a.SessionID)
	if err != nil || session.Status != "active" || session.ProviderType == "cli_v2" || session.UserID != p.UserID || !session.ExpiresAt.After(s.cliNow().UTC()) {
		return cliError(apperrors.ErrUnauthorized, "browser_session_required", "A current browser session is required")
	}
	return s.requireCLIMFA(ctx, p.UserID, a.SessionID)
}
func (s *Service) requireCLIMFA(ctx context.Context, userID, sessionID string) error {
	if isNilDependency(s.cliMFA) {
		return cliError(apperrors.ErrServiceUnavailable, "cli_auth_unavailable", "Additional authentication policy is unavailable")
	}
	credentials, err := s.cliMFA.ListCredentials(ctx, userID)
	if err != nil {
		return err
	}
	if len(credentials) == 0 {
		return nil
	}
	recent := false
	if sessionID != "" {
		recent, err = s.cliMFA.HasRecentStepUp(ctx, sessionID, userID, s.cliNow().UTC().Add(-10*time.Minute))
		if err != nil {
			return err
		}
	}
	if !recent {
		return cliError(apperrors.ErrMFARequired, "interactive_auth_required", "Complete recent MFA verification in the browser before authorizing a CLI session")
	}
	return nil
}
func (s *Service) sealCLIReceipt(binding string, result domainidentity.AuthResult) (string, error) {
	raw, err := json.Marshal(struct {
		Binding string
		Result  domainidentity.AuthResult
	}{binding, result})
	if err != nil {
		return "", err
	}
	return secretcrypto.EncryptStringWithKeyring(s.cliReceiptKeys, string(raw))
}
func (s *Service) openCLIReceipt(ctx context.Context, binding, ciphertext string, until time.Time) (domainidentity.AuthResult, error) {
	if ciphertext == "" || !until.After(s.cliNow().UTC()) {
		return domainidentity.AuthResult{}, cliError(apperrors.ErrUnauthorized, "refresh_recovery_expired", "The response recovery window expired; log in again")
	}
	if !secretcrypto.Encrypted(ciphertext) {
		return domainidentity.AuthResult{}, apperrors.ErrUnauthorized
	}
	raw, err := secretcrypto.DecryptStringWithKeyring(s.cliReceiptKeys, ciphertext)
	if err != nil {
		return domainidentity.AuthResult{}, err
	}
	var receipt struct {
		Binding string
		Result  domainidentity.AuthResult
	}
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return domainidentity.AuthResult{}, err
	}
	if !cliEqual(binding, receipt.Binding) {
		return domainidentity.AuthResult{}, apperrors.ErrUnauthorized
	}
	session, err := s.sessions.GetAuthSessionByID(ctx, receipt.Result.Tokens.SessionID)
	if err != nil || session.ProviderType != "cli_v2" || session.Status != "active" || !session.ExpiresAt.After(s.cliNow().UTC()) {
		return domainidentity.AuthResult{}, apperrors.ErrUnauthorized
	}
	if _, err := s.requireActiveAuthzState(ctx, session.UserID); err != nil {
		return domainidentity.AuthResult{}, err
	}
	return receipt.Result, nil
}
func cliMetadataStrings(value any) []string {
	switch list := value.(type) {
	case []string:
		return slices.Clone(list)
	case []any:
		out := make([]string, 0, len(list))
		for _, v := range list {
			if text, ok := v.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
func cliAbsoluteExpiry(session domainidentity.Session) (time.Time, error) {
	value, _ := session.Metadata["cliAbsoluteExpiresAt"].(string)
	expiry, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || expiry.IsZero() {
		return time.Time{}, apperrors.ErrUnauthorized
	}
	return expiry, nil
}
func (s *Service) cliSessionPrincipal(ctx context.Context, session domainidentity.Session) (domainidentity.Principal, error) {
	if session.ProviderType != "cli_v2" || session.Status != "active" || !session.ExpiresAt.After(s.cliNow().UTC()) {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	expiry, err := cliAbsoluteExpiry(session)
	if err != nil || !expiry.After(s.cliNow().UTC()) {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	p, err := s.CurrentPrincipal(ctx, session.UserID)
	if err != nil {
		return p, err
	}
	p, err = appaccess.RuntimePrincipalWithin(ctx, s.permissions, p, cliMetadataStrings(session.Metadata["cliPermissionKeys"]))
	if err != nil {
		return p, err
	}
	p.AccessTokenID = cliSessionTokenPrefix + session.ID
	return p, nil
}

func canonicalCLIOrigin(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	host := strings.ToLower(u.Host)
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		host = strings.TrimSuffix(host, ":"+u.Port())
	}
	return strings.ToLower(u.Scheme) + "://" + host
}
