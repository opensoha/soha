package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appgateway "github.com/opensoha/soha/internal/application/aigateway"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/appconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	gatewayrepo "github.com/opensoha/soha/internal/repository/aigateway"
	identitymfarepo "github.com/opensoha/soha/internal/repository/identitymfa"
	policyrepo "github.com/opensoha/soha/internal/repository/policy"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	"gorm.io/gorm"
)

type cliIdentityFixture struct {
	ctx               context.Context
	db                *gorm.DB
	service, replica  *appidentity.Service
	users             *userrepo.Repository
	owner, other      string
	cfg               appconfig.Auth
	serviceWithConfig func(appconfig.Auth) *appidentity.Service
}

func newCLIIdentityFixture(t *testing.T, auditors ...appidentity.AuditRecorder) cliIdentityFixture {
	t.Helper()
	_, store, ctx := newCopilotPostgresTestRepository(t)
	users := userrepo.New(store.DB())
	policies := policyrepo.New(store.DB())
	owner, other := uuid.NewString(), uuid.NewString()
	createAuthzTestUsers(t, ctx, users, policies, owner, other, "cli-test-only-password")
	key, err := keyring.NewKey("task115-receipt-test", uuid.NewString(), time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := appconfig.Auth{CLI: appconfig.CLIAuth{Enabled: true, AllowPassword: true, PublicURL: "https://soha.example.test"}, JWT: appconfig.JWT{Secret: uuid.NewString(), Issuer: "task115-cli-test", AccessTTL: time.Minute, RefreshTTL: time.Hour}}
	var auditor appidentity.AuditRecorder
	if len(auditors) > 0 {
		auditor = auditors[0]
	}
	start := func(cfg appconfig.Auth) *appidentity.Service {
		repo := userrepo.New(store.DB())
		s, err := appidentity.New(appidentity.Dependencies{Config: cfg, Accounts: repo, Passwords: repo, Authorization: repo, RoleBindings: repo, TeamBindings: repo, Identities: repo, Sessions: repo, SessionAdmin: repo, EphemeralTokens: repo, CLIState: repo, CLISessions: repo, CLIMFA: identitymfarepo.New(store.DB()), CLIReceiptKeys: keys, Permissions: appaccess.NewPermissionResolver(policies), Audit: auditor})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	return cliIdentityFixture{ctx: ctx, db: store.DB(), service: start(cfg), replica: start(cfg), users: users, owner: owner, other: other, cfg: cfg, serviceWithConfig: start}
}

type failingCLIAudit struct {
	mu      sync.Mutex
	entries []domainaudit.Entry
}

func (a *failingCLIAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, entry)
	return errors.New("synthetic audit write failure")
}
func (f cliIdentityFixture) browser(t *testing.T, userID string) (domainidentity.Principal, domainidentity.AccessContext, domainidentity.AuthResult) {
	t.Helper()
	r, err := f.service.LoginWithPassword(f.ctx, userID, "cli-test-only-password")
	if err != nil {
		t.Fatal(err)
	}
	p, a, err := f.service.ParseAccessToken(f.ctx, r.Tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return p, a, r
}
func (f cliIdentityFixture) grant(t *testing.T, source string) (domainidentity.CLIDeviceAuthorization, string, string) {
	t.Helper()
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	grant, err := f.service.CreateCLIDeviceAuthorization(f.ctx, domainidentity.CLIDeviceCreate{ClientID: "soha-cli", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256", DeviceName: "Isolated terminal", ClientVersion: "test"}, source)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(grant.VerificationURI)
	if err != nil {
		t.Fatal(err)
	}
	handle := strings.TrimPrefix(u.Fragment, "handle=")
	if len(grant.UserCode) != 6 || len(handle) != 43 {
		t.Fatal("invalid device authorization boundary")
	}
	return grant, handle, verifier
}
func (f cliIdentityFixture) approve(t *testing.T, g domainidentity.CLIDeviceAuthorization, handle string) {
	t.Helper()
	p, a, _ := f.browser(t, f.owner)
	c, err := f.service.InspectCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", domainidentity.CLIDeviceInspect{VerificationHandle: handle, UserCode: g.UserCode})
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.replica.DecideCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", domainidentity.CLIDeviceDecision{VerificationHandle: handle, UserCode: g.UserCode, ConfirmationID: c.ConfirmationID, Decision: "approve"})
	if err != nil || r.Status != "approved" {
		t.Fatal("device approval failed", err)
	}
}
func (f cliIdentityFixture) claim(t *testing.T, g domainidentity.CLIDeviceAuthorization, verifier, id string) domainidentity.AuthResult {
	t.Helper()
	r, err := f.replica.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier, ClaimRequestID: id})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestCLIAuthenticationWithPostgres(t *testing.T) {
	t.Run("device_claim_refresh_replay", testCLIDeviceClaimRefreshReplay)
	t.Run("request_bound_code_and_attempts", testCLIRequestBoundCodeAndAttempts)
	t.Run("browser_subject_origin_and_confirmation", testCLIBrowserSubjectOriginAndConfirmation)
	t.Run("claim_pkce_and_encrypted_receipt", testCLIClaimPkceAndEncryptedReceipt)
	t.Run("cancel_claim_race", testCLICancelClaimRace)
	t.Run("shared_poll_and_admission", testCLISharedPollAndAdmission)
	t.Run("password_account_admission", testCLIPasswordAccountAdmission)
	t.Run("active_capacity_hot_requests", testCLIActiveCapacityHotRequests)
	t.Run("issuance_atomic_rollback", testCLIIssuanceAtomicRollback)
	t.Run("issued_grant_admission", testCLIIssuedGrantAdmission)
	t.Run("device_decision_recovery", testCLIDeviceDecisionRecovery)
	t.Run("atomic_key_rotation", testCLIAtomicKeyRotation)
	t.Run("key_rotation_audit_failure_recovery", testCLIKeyRotationAuditFailureRecovery)
	t.Run("audit_events_redacted_and_best_effort", testCLIAuditEventsRedactedAndBestEffort)
	t.Run("session_policy_and_authority", testCLISessionPolicyAndAuthority)
	t.Run("login_disabled_existing_sessions", testCLILoginDisabledExistingSessions)
	t.Run("v1_compatibility", testCLIV1Compatibility)
	t.Run("governed_approval_persistence", testGovernedApprovalPersistence)
}

func testGovernedApprovalPersistence(t *testing.T) {
	f := newCLIIdentityFixture(t)
	repo := gatewayrepo.New(f.db)
	for _, token := range []string{"", uuid.NewString()} {
		item := domainaigateway.ApprovalRequest{ID: uuid.NewString(), Status: "pending", Strategy: "human_confirm", ActorType: "user", ActorID: f.owner, ActorTokenID: token, ToolName: "test.read", RiskLevel: "read", RequiresApproval: true, Summary: "isolated persistence acceptance"}
		if token != "" {
			item.ActorPermissionCeiling = []string{"governance.aiGateway.invoke", "platform.pods.view"}
			ciphertext, err := secretcrypto.EncryptString("task115-isolated-approval-storage-key", "SENSITIVE_YAML_MARKER")
			if err != nil {
				t.Fatal(err)
			}
			item.ProtectedToolInput = ciphertext
			item.ToolInput = map[string]any{"content": "[PROTECTED]"}
		}
		if _, err := repo.CreateApprovalRequest(f.ctx, item); err != nil {
			t.Fatal(err)
		}
		loaded, err := gatewayrepo.New(f.db).GetApprovalRequest(f.ctx, item.ID)
		if err != nil || loaded.ActorTokenID != token || !slices.Equal(loaded.ActorPermissionCeiling, item.ActorPermissionCeiling) || loaded.ProtectedToolInput != item.ProtectedToolInput {
			t.Fatalf("approval execution identity did not survive repository reconstruction: %+v %v", loaded, err)
		}
		listed, err := repo.ListApprovalRequests(f.ctx, domainaigateway.ApprovalRequestFilter{ID: item.ID})
		if err != nil || len(listed) != 1 || listed[0].ActorTokenID != token || !slices.Equal(listed[0].ActorPermissionCeiling, item.ActorPermissionCeiling) || listed[0].ProtectedToolInput != item.ProtectedToolInput {
			t.Fatalf("approval list identity mismatch: %+v %v", listed, err)
		}
		if token != "" {
			plain, err := secretcrypto.DecryptString("task115-isolated-approval-storage-key", loaded.ProtectedToolInput)
			if err != nil || plain != "SENSITIVE_YAML_MARKER" {
				t.Fatal("protected approval input did not survive persistence", err)
			}
			raw, err := json.Marshal(listed)
			if err != nil || strings.Contains(string(raw), "SENSITIVE_YAML_MARKER") || strings.Contains(string(raw), item.ProtectedToolInput) {
				t.Fatal("approval API projection exposed protected input", err)
			}
		}
	}
}

func testCLILoginDisabledExistingSessions(t *testing.T) {
	f := newCLIIdentityFixture(t)
	issued, issuedHandle, issuedVerifier := f.grant(t, "127.0.0.1")
	f.approve(t, issued, issuedHandle)
	claimID := uuid.NewString()
	first := f.claim(t, issued, issuedVerifier, claimID)
	pending, pendingHandle, pendingVerifier := f.grant(t, "127.0.0.2")
	f.approve(t, pending, pendingHandle)
	cfg := f.cfg
	cfg.CLI.Enabled = false
	disabled := f.serviceWithConfig(cfg)
	if _, err := disabled.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: pending.DeviceCode, CodeVerifier: pendingVerifier, ClaimRequestID: uuid.NewString()}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("disabled login issued a session from an approved device grant", err)
	}
	if _, err := disabled.LoginCLIWithPassword(f.ctx, f.owner, "cli-test-only-password", "127.0.0.1"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("disabled login issued a password session", err)
	}
	recovered, err := disabled.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: issued.DeviceCode, CodeVerifier: issuedVerifier, ClaimRequestID: claimID})
	if err != nil || recovered.Tokens.SessionID != first.Tokens.SessionID || recovered.Tokens.RefreshToken != first.Tokens.RefreshToken {
		t.Fatal("disabled login prevented recovery of an already-issued session", err)
	}
	if _, _, err = disabled.ParseAccessToken(f.ctx, first.Tokens.AccessToken); err != nil {
		t.Fatal("disabled login invalidated existing access", err)
	}
	renewed, err := disabled.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: first.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()})
	if err != nil || renewed.Tokens.SessionID != first.Tokens.SessionID {
		t.Fatal("disabled login prevented existing session refresh", err)
	}
	if err = disabled.LogoutCLISession(f.ctx, renewed.Tokens.RefreshToken); err != nil {
		t.Fatal("disabled login prevented logout", err)
	}
	if _, _, err = disabled.ParseAccessToken(f.ctx, renewed.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("disabled login prevented logout from revoking access", err)
	}
}

func testCLIDeviceClaimRefreshReplay(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, handle, verifier := f.grant(t, "127.0.0.1")
	f.approve(t, g, handle)
	claimID := uuid.NewString()
	r := f.claim(t, g, verifier, claimID)
	if r.Tokens.ProtocolVersion != 2 || r.Tokens.CredentialKind != "session" || r.Tokens.SessionID == "" || !strings.HasPrefix(r.Tokens.RefreshToken, "soha_cli_refresh_") {
		t.Fatal("v2 session metadata missing")
	}
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(r.Tokens.AccessToken, claims); err != nil || claims["token_type"] != "cli_access" {
		t.Fatal("v2 access must be rejected by an older Core token-type validator")
	}
	p, a, err := f.service.ParseAccessToken(f.ctx, r.Tokens.AccessToken)
	if err != nil || a.SessionID != r.Tokens.SessionID || p.UserID != f.owner {
		t.Fatal("issued access rejected", err)
	}
	repeated := f.claim(t, g, verifier, claimID)
	if repeated.Tokens.RefreshToken != r.Tokens.RefreshToken {
		t.Fatal("claim recovery issued a new refresh credential")
	}
	refreshID := uuid.NewString()
	next, err := f.service.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: r.Tokens.RefreshToken, RefreshRequestID: refreshID})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := f.replica.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: r.Tokens.RefreshToken, RefreshRequestID: refreshID})
	if err != nil || recovered.Tokens.RefreshToken != next.Tokens.RefreshToken {
		t.Fatal("refresh recovery diverged", err)
	}
	if !next.Tokens.SessionAbsoluteExpiresAt.Equal(*r.Tokens.SessionAbsoluteExpiresAt) || next.Tokens.RefreshToken == r.Tokens.RefreshToken {
		t.Fatal("rotation changed absolute expiry or reused credential")
	}
	if _, err := f.service.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: r.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("replay was accepted")
	}
	if _, _, err := f.service.ParseAccessToken(f.ctx, next.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("replay revocation did not persist")
	}
	if _, err := f.replica.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: next.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("a family branch survived replay revocation")
	}
}

func testCLIRequestBoundCodeAndAttempts(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, handle, _ := f.grant(t, "127.0.0.1")
	p, a, _ := f.browser(t, f.owner)
	wrong := "000042"
	if wrong == g.UserCode {
		wrong = "000043"
	}
	for i := 0; i < 5; i++ {
		if _, err := f.replica.InspectCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", domainidentity.CLIDeviceInspect{VerificationHandle: handle, UserCode: wrong}); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	var state string
	if err := f.db.Raw(`SELECT state FROM cli_device_authorizations WHERE id=?`, g.GrantID).Row().Scan(&state); err != nil || state != "denied" {
		t.Fatal("wrong-code attempts did not persist across instances")
	}
	other, otherAccess, _ := f.browser(t, f.other)
	if _, err := f.service.InspectCLIDeviceAuthorization(f.ctx, other, otherAccess, "https://soha.example.test", "127.0.0.2", domainidentity.CLIDeviceInspect{VerificationHandle: strings.Repeat("x", 43), UserCode: g.UserCode}); err == nil {
		t.Fatal("global six digit lookup allowed")
	}
}

func testCLIBrowserSubjectOriginAndConfirmation(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, handle, _ := f.grant(t, "127.0.0.1")
	p, a, _ := f.browser(t, f.owner)
	input := domainidentity.CLIDeviceInspect{VerificationHandle: handle, UserCode: g.UserCode}
	for _, origin := range []string{"", "https://evil.example.test", "https://soha.example.test/"} {
		if _, err := f.service.InspectCLIDeviceAuthorization(f.ctx, p, a, origin, "127.0.0.1", input); err == nil {
			t.Fatal("invalid origin accepted")
		}
	}
	keyAccess := a
	keyAccess.TokenKind = "personal_access_token"
	if _, err := f.service.InspectCLIDeviceAuthorization(f.ctx, p, keyAccess, "https://soha.example.test", "127.0.0.1", input); err == nil {
		t.Fatal("Key approved a browser request")
	}
	c, err := f.service.InspectCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", input)
	if err != nil {
		t.Fatal(err)
	}
	other, otherAccess, _ := f.browser(t, f.other)
	if _, err := f.replica.DecideCLIDeviceAuthorization(f.ctx, other, otherAccess, "https://soha.example.test", "127.0.0.2", domainidentity.CLIDeviceDecision{VerificationHandle: handle, UserCode: g.UserCode, ConfirmationID: c.ConfirmationID, Decision: "approve"}); err == nil {
		t.Fatal("confirmation crossed accounts")
	}
}

func testCLIClaimPkceAndEncryptedReceipt(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, handle, verifier := f.grant(t, "127.0.0.1")
	f.approve(t, g, handle)
	id := uuid.NewString()
	r := f.claim(t, g, verifier, id)
	if _, err := f.service.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: g.DeviceCode, CodeVerifier: strings.Repeat("w", 43), ClaimRequestID: id}); err == nil {
		t.Fatal("receipt bypassed original PKCE")
	}
	var payload string
	if err := f.db.Raw(`SELECT payload::text FROM cli_device_authorizations WHERE id=?`, g.GrantID).Row().Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{g.DeviceCode, handle, verifier, r.Tokens.AccessToken, r.Tokens.RefreshToken} {
		if strings.Contains(payload, secret) {
			t.Fatal("grant persisted a raw credential")
		}
	}
	if err := f.db.Exec(`UPDATE cli_device_authorizations SET receipt_expires_at=NOW()-INTERVAL '1 second',payload=jsonb_set(payload,'{ReceiptExpiresAt}',to_jsonb((NOW()-INTERVAL '1 second')::text)) WHERE id=?`, g.GrantID).Error; err != nil {
		t.Fatal(err)
	}
	// Use JSON timestamp format accepted by time.Time, independent of SQL output formatting.
	if err := f.db.Exec(`UPDATE cli_device_authorizations SET payload=jsonb_set(payload,'{ReceiptExpiresAt}',to_jsonb(?::text)) WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), g.GrantID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.replica.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier, ClaimRequestID: id}); err == nil {
		t.Fatal("expired claim receipt replayed")
	}
}

func testCLICancelClaimRace(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, handle, verifier := f.grant(t, "127.0.0.1")
	f.approve(t, g, handle)
	var wg sync.WaitGroup
	wg.Add(2)
	var result domainidentity.AuthResult
	var cancelErr error
	go func() {
		defer wg.Done()
		result, _ = f.service.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier, ClaimRequestID: uuid.NewString()})
	}()
	go func() {
		defer wg.Done()
		_, cancelErr = f.replica.CancelCLIDeviceAuthorization(f.ctx, domainidentity.CLIDeviceCancelRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier})
	}()
	wg.Wait()
	if cancelErr != nil {
		t.Fatal(cancelErr)
	}
	if result.Tokens.AccessToken != "" {
		if _, _, err := f.service.ParseAccessToken(f.ctx, result.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("cancelled grant left access active")
		}
	}
	var count int
	if err := f.db.Raw(`SELECT COUNT(*) FROM sessions WHERE provider_type='cli_v2' AND status='active'`).Row().Scan(&count); err != nil || count != 0 {
		t.Fatal("cancel race left an active session")
	}
}

func testCLISharedPollAndAdmission(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, _, verifier := f.grant(t, "127.0.0.1")
	req := domainidentity.CLIDeviceTokenRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier, ClaimRequestID: uuid.NewString()}
	_, err := f.service.ExchangeCLIDeviceToken(f.ctx, req)
	var pending *appidentity.CLIPollError
	if !errors.As(err, &pending) || !pending.Pending || pending.Seconds != 5 {
		t.Fatal("pending response missing")
	}
	_, err = f.replica.ExchangeCLIDeviceToken(f.ctx, req)
	var slow *appidentity.CLIPollError
	if !errors.As(err, &slow) || slow.Code != "slow_down" || slow.Seconds != 10 {
		t.Fatal("shared polling interval missing")
	}
	for i := 0; i < 4; i++ {
		f.grant(t, "127.0.0.1")
	}
	sum := sha256.Sum256([]byte(verifier))
	_, err = f.replica.CreateCLIDeviceAuthorization(f.ctx, domainidentity.CLIDeviceCreate{ClientID: "soha-cli", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256"}, "127.0.0.1")
	var capacity *appidentity.CLIPollError
	if !errors.As(err, &capacity) || capacity.Code != "device_capacity_limited" {
		t.Fatal("shared pending admission exceeded")
	}
	var sourceCount int
	if err := f.db.Raw(`SELECT count FROM cli_auth_budgets WHERE bucket LIKE 'create:%' AND bucket <> 'create:global'`).Row().Scan(&sourceCount); err != nil || sourceCount != 6 {
		t.Fatal("capacity rejection rolled back the source attempt budget")
	}
	input := domainidentity.CLIDeviceCreate{ClientID: "soha-cli", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256"}
	for i := 0; i < 4; i++ {
		if _, err := f.service.CreateCLIDeviceAuthorization(f.ctx, input, "127.0.0.1"); !errors.As(err, &capacity) || capacity.Code != "device_capacity_limited" {
			t.Fatal("capacity rejection did not retain its public outcome")
		}
	}
	_, err = f.replica.CreateCLIDeviceAuthorization(f.ctx, input, "127.0.0.1")
	if !errors.As(err, &capacity) || capacity.Code != "rate_limited" {
		t.Fatal("repeated capacity rejection bypassed shared source throttling")
	}
	var globalCount int
	if err := f.db.Raw(`SELECT count FROM cli_auth_budgets WHERE bucket='create:global'`).Row().Scan(&globalCount); err != nil || globalCount != 10 {
		t.Fatal("source-limited retries consumed additional global budget")
	}
	f.grant(t, "127.0.0.2")
}

func testCLIPasswordAccountAdmission(t *testing.T) {
	f := newCLIIdentityFixture(t)
	for i := 0; i < 10; i++ {
		login, service := f.owner, f.service
		if i%2 == 1 {
			login, service = "  "+strings.ToUpper(f.owner+"@example.test")+"  ", f.replica
		}
		if _, err := service.LoginCLIWithPassword(f.ctx, login, "wrong-test-password", fmt.Sprintf("198.18.0.%d", i+1)); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("allowed password attempt did not reach ordinary authentication")
		}
	}
	_, err := f.replica.LoginCLIWithPassword(f.ctx, f.owner, "cli-test-only-password", "198.18.0.100")
	var limited *appidentity.CLIPollError
	if !errors.As(err, &limited) || limited.Code != "rate_limited" {
		t.Fatal("distributed sources or login aliases bypassed account budget")
	}
	if _, err := f.service.LoginCLIWithPassword(f.ctx, f.other, "cli-test-only-password", "198.18.0.100"); err != nil {
		t.Fatal("account rejection blocked an unrelated account")
	}
	var before, after int64
	if err := f.db.Table("cli_auth_budgets").Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`UPDATE cli_auth_budgets SET count=1000 WHERE bucket='password:global'`).Error; err != nil {
		t.Fatal(err)
	}
	_, err = f.replica.LoginCLIWithPassword(f.ctx, "unknown-test-account", "wrong-test-password", "198.18.0.101")
	if !errors.As(err, &limited) || limited.Code != "rate_limited" {
		t.Fatal("global password admission was bypassed")
	}
	if err := f.db.Table("cli_auth_budgets").Count(&after).Error; err != nil || before != after {
		t.Fatal("global rejection retained new source/account budget rows")
	}
	if err := f.db.Exec(`UPDATE cli_auth_budgets SET expires_at=CURRENT_TIMESTAMP - INTERVAL '1 second'`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.replica.LoginCLIWithPassword(f.ctx, f.owner+"@example.test", "cli-test-only-password", "198.18.0.1"); err != nil {
		t.Fatal("expired password budgets did not recover")
	}
}

func testCLIActiveCapacityHotRequests(t *testing.T) {
	f := newCLIIdentityFixture(t)
	started := time.Now()
	for i := 0; i < 1000; i++ {
		f.grant(t, fmt.Sprintf("198.18.%d.%d", i/250, i%250+1))
	}
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	input := domainidentity.CLIDeviceCreate{ClientID: "soha-cli", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256"}
	jobs, outcomes := make(chan int, 128), make(chan error, 128)
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range jobs {
				service := f.service
				if id%2 == 1 {
					service = f.replica
				}
				_, err := service.CreateCLIDeviceAuthorization(f.ctx, input, fmt.Sprintf("198.19.0.%d", id+1))
				outcomes <- err
			}
		}()
	}
	for i := 0; i < 128; i++ {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	close(outcomes)
	for err := range outcomes {
		var limited *appidentity.CLIPollError
		if !errors.As(err, &limited) || limited.Code != "rate_limited" {
			t.Fatal("hot traffic bypassed global admission at the active capacity")
		}
	}
	var grants, budgets int64
	if err := f.db.Table("cli_device_authorizations").Count(&grants).Error; err != nil || grants != 1000 {
		t.Fatal("active capacity or rejected grant storage grew")
	}
	if err := f.db.Table("cli_auth_budgets").Count(&budgets).Error; err != nil || budgets != 1001 {
		t.Fatal("globally rejected sources grew the budget table")
	}
	t.Logf("1000 active grants and 128 hot requests across 16 workers/two services completed in %s; all workers joined", time.Since(started))
}

func testCLIIssuanceAtomicRollback(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g, handle, verifier := f.grant(t, "127.0.0.1")
	f.approve(t, g, handle)
	if err := f.db.Exec(`CREATE FUNCTION task115_reject_refresh() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test-only issuance failure'; END $$; CREATE TRIGGER task115_reject_refresh BEFORE INSERT ON cli_refresh_credentials FOR EACH ROW EXECUTE FUNCTION task115_reject_refresh()`).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err := f.service.ExchangeCLIDeviceToken(f.ctx, domainidentity.CLIDeviceTokenRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier, ClaimRequestID: id}); err == nil {
		t.Fatal("injected persistence failure was accepted")
	}
	var count int
	var state string
	if err := f.db.Raw(`SELECT COUNT(*) FROM sessions WHERE provider_type='cli_v2'`).Row().Scan(&count); err != nil || count != 0 {
		t.Fatal("failed issuance left a session")
	}
	if err := f.db.Raw(`SELECT state FROM cli_device_authorizations WHERE id=?`, g.GrantID).Row().Scan(&state); err != nil || state != "approved" {
		t.Fatal("failed issuance consumed the grant")
	}
	if err := f.db.Exec(`DROP TRIGGER task115_reject_refresh ON cli_refresh_credentials; DROP FUNCTION task115_reject_refresh()`).Error; err != nil {
		t.Fatal(err)
	}
	f.claim(t, g, verifier, id)
}

func testCLIIssuedGrantAdmission(t *testing.T) {
	f := newCLIIdentityFixture(t)
	g := domainidentity.CLIGrant{ID: uuid.NewString(), DeviceHash: uuid.NewString(), HandleHash: uuid.NewString(), SourceHash: "source-test", State: "issued", ExpiresAt: time.Now().UTC().Add(time.Minute)}
	if err := f.users.WithinCLITransaction(f.ctx, func(ctx context.Context) error {
		ok, e := f.users.CreateCLIGrant(ctx, g, 5, 1)
		if e != nil {
			return e
		}
		if !ok {
			t.Fatal("first grant was not admitted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	g.ID = uuid.NewString()
	g.DeviceHash = uuid.NewString()
	g.HandleHash = uuid.NewString()
	g.State = "pending"
	if err := f.users.WithinCLITransaction(f.ctx, func(ctx context.Context) error {
		ok, e := f.users.CreateCLIGrant(ctx, g, 5, 1)
		if ok {
			t.Fatal("issuing a grant released its retention admission budget")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func testCLIDeviceDecisionRecovery(t *testing.T) {
	for _, decision := range []string{"approve", "deny"} {
		t.Run(decision, func(t *testing.T) {
			f := newCLIIdentityFixture(t)
			g, handle, verifier := f.grant(t, "127.0.0.1")
			p, a, _ := f.browser(t, f.owner)
			confirmation, err := f.service.InspectCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", domainidentity.CLIDeviceInspect{VerificationHandle: handle, UserCode: g.UserCode})
			if err != nil {
				t.Fatal(err)
			}
			input := domainidentity.CLIDeviceDecision{VerificationHandle: handle, UserCode: g.UserCode, ConfirmationID: confirmation.ConfirmationID, Decision: decision}
			first, err := f.service.DecideCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", input)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := f.replica.DecideCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", input)
			if err != nil || first.Status != repeated.Status || first.GrantID != repeated.GrantID {
				t.Fatal("lost decision response was not recoverable", err)
			}
			if decision == "approve" {
				f.claim(t, g, verifier, uuid.NewString())
				recovered, e := f.service.DecideCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", input)
				if e != nil || recovered.Status != "issued" {
					t.Fatal("issued decision status was not recoverable", e)
				}
			}
			input.ConfirmationID = strings.Repeat("x", 43)
			if _, e := f.service.DecideCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.1", input); e == nil {
				t.Fatal("another confirmation retrieved the closed decision")
			}
		})
	}
}

func testCLIAtomicKeyRotation(t *testing.T) {
	f := newCLIIdentityFixture(t)
	repo := gatewayrepo.New(f.db)
	old := domainaigateway.PersonalAccessToken{ID: uuid.NewString(), UserID: f.owner, Name: "task115-test", TokenHash: uuid.NewString(), TokenPrefix: "test", CreatedBy: f.owner}
	if _, err := repo.CreatePersonalAccessToken(f.ctx, old); err != nil {
		t.Fatal(err)
	}
	replacement := old
	replacement.ID = uuid.NewString()
	replacement.TokenHash = uuid.NewString()
	id := uuid.NewString()
	record := domainaigateway.TokenRotationRecord{RotationRequestID: id, ActorID: f.owner, OwnerID: f.owner, TokenKind: "personal_access_token", PreviousTokenID: old.ID, ReplacementTokenID: replacement.ID, RotatedAt: time.Now().UTC()}
	if err := f.db.Exec(`CREATE FUNCTION task115_reject_rotation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test-only rotation failure'; END $$; CREATE TRIGGER task115_reject_rotation BEFORE INSERT ON gateway_token_rotations FOR EACH ROW EXECUTE FUNCTION task115_reject_rotation()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RotatePersonalToken(f.ctx, old.ID, replacement, record); err == nil {
		t.Fatal("rotation ignored injected persistence failure")
	}
	before, err := repo.GetPersonalAccessTokenByID(f.ctx, old.ID)
	if err != nil || before.RevokedAt != nil {
		t.Fatal("failed rotation revoked the previous Key", err)
	}
	if _, err = repo.GetPersonalAccessTokenByID(f.ctx, replacement.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("failed rotation left a replacement", err)
	}
	if err = f.db.Exec(`DROP TRIGGER task115_reject_rotation ON gateway_token_rotations; DROP FUNCTION task115_reject_rotation()`).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := repo.RotatePersonalToken(f.ctx, old.ID, replacement, record)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		} else if !errors.Is(e, apperrors.ErrConflict) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent rotation must commit exactly once")
	}
	previous, err := repo.GetPersonalAccessTokenByID(f.ctx, old.ID)
	if err != nil || previous.RevokedAt == nil {
		t.Fatal("successful rotation did not revoke previous Key", err)
	}
	saved, err := gatewayrepo.New(f.db).GetTokenRotation(f.ctx, id)
	if err != nil || saved.ReplacementTokenID != replacement.ID || saved.NewValueRetrievable {
		t.Fatal("durable rotation recovery failed", err)
	}
}

func testCLIKeyRotationAuditFailureRecovery(t *testing.T) {
	f := newCLIIdentityFixture(t)
	policies := policyrepo.New(f.db)
	role := "cli-key-audit-" + uuid.NewString()
	if _, err := policies.CreateRole(f.ctx, domainaccess.RoleInput{ID: role, Name: role, Scope: "global", PermissionKeys: []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke}}); err != nil {
		t.Fatal(err)
	}
	if err := f.users.ReplaceRoleBindings(f.ctx, f.owner, []string{role}); err != nil {
		t.Fatal(err)
	}
	principal, _, _ := f.browser(t, f.owner)
	auditor := &failingCLIAudit{}
	repo := gatewayrepo.New(f.db)
	service := appgateway.NewWithDeps(appgateway.ServiceDeps{Permissions: appaccess.NewPermissionResolver(policies), PersonalTokens: repo, TokenRotations: repo, Audit: auditor})
	old := domainaigateway.PersonalAccessToken{ID: uuid.NewString(), UserID: f.owner, Name: "task115-audit-fault", TokenHash: uuid.NewString(), TokenPrefix: "test", CreatedBy: f.owner}
	if _, err := repo.CreatePersonalAccessToken(f.ctx, old); err != nil {
		t.Fatal(err)
	}
	input := domainaigateway.TokenRotationInput{RotationRequestID: uuid.NewString()}
	created, err := service.RotatePersonalAccessToken(f.ctx, principal, old.ID, input)
	if err != nil || created.Value == "" {
		t.Fatal("Audit failure changed committed Key rotation")
	}
	if len(auditor.entries) != 1 || strings.Contains(fmt.Sprint(auditor.entries), created.Value) {
		t.Fatal("Missing failed audit attempt or plaintext Key entered audit")
	}
	previous, err := repo.GetPersonalAccessTokenByID(f.ctx, old.ID)
	if err != nil || previous.RevokedAt == nil {
		t.Fatal("Audit failure restored the previous Key")
	}
	before, err := repo.ListPersonalAccessTokens(f.ctx, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	// Discard the one-time response and recover through an independent session
	// and service/repository instance; no audit persistence is available.
	independent, _, _ := f.browser(t, f.owner)
	recoveryRepo := gatewayrepo.New(f.db)
	recovery := appgateway.NewWithDeps(appgateway.ServiceDeps{Permissions: appaccess.NewPermissionResolver(policies), PersonalTokens: recoveryRepo, TokenRotations: recoveryRepo, Audit: auditor})
	record, err := recovery.GetTokenRotation(f.ctx, independent, input.RotationRequestID)
	if err != nil || record.ReplacementTokenID != created.Token.ID || record.NewValueRetrievable {
		t.Fatal("Rotation outcome depended on successful audit or the lost response")
	}
	replayed, err := recovery.RotatePersonalAccessToken(f.ctx, independent, old.ID, input)
	var business *apperrors.BusinessError
	if !errors.As(err, &business) || business.Code() != "token_rotation_completed" || replayed.Value != "" {
		t.Fatal("Repeated rotation replayed a plaintext Key or reminted it")
	}
	after, err := repo.ListPersonalAccessTokens(f.ctx, f.owner)
	if err != nil || len(after) != len(before) {
		t.Fatal("Repeated rotation created another Key")
	}
}

func testCLIAuditEventsRedactedAndBestEffort(t *testing.T) {
	auditor := &failingCLIAudit{}
	f := newCLIIdentityFixture(t, auditor)
	g, handle, verifier := f.grant(t, "127.0.0.1")
	f.approve(t, g, handle)
	claimID := uuid.NewString()
	first := f.claim(t, g, verifier, claimID)
	f.claim(t, g, verifier, claimID)
	refreshID := uuid.NewString()
	req := domainidentity.CLIRefreshRequest{RefreshToken: first.Tokens.RefreshToken, RefreshRequestID: refreshID}
	next, err := f.service.RefreshCLISession(f.ctx, req)
	if err != nil {
		t.Fatal("audit failure changed successful refresh")
	}
	if _, err := f.replica.RefreshCLISession(f.ctx, req); err != nil {
		t.Fatal("audit failure changed receipt recovery")
	}
	req.RefreshRequestID = uuid.NewString()
	if _, err := f.replica.RefreshCLISession(f.ctx, req); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("audit failure changed replay revocation")
	}
	if err := f.service.LogoutCLISession(f.ctx, next.Tokens.RefreshToken); err != nil {
		t.Fatal("audit failure changed logout")
	}
	if _, err := f.replica.CancelCLIDeviceAuthorization(f.ctx, domainidentity.CLIDeviceCancelRequest{DeviceCode: g.DeviceCode, CodeVerifier: verifier}); err != nil {
		t.Fatal("audit failure changed cancellation")
	}
	bad, badHandle, _ := f.grant(t, "127.0.0.2")
	p, a, _ := f.browser(t, f.other)
	wrong := "000000"
	if bad.UserCode == wrong {
		wrong = "999999"
	}
	for i := 0; i < 5; i++ {
		if _, err := f.service.InspectCLIDeviceAuthorization(f.ctx, p, a, "https://soha.example.test", "127.0.0.2", domainidentity.CLIDeviceInspect{VerificationHandle: badHandle, UserCode: wrong}); err == nil {
			t.Fatal("invalid device code accepted")
		}
	}
	counts := map[string]int{}
	for _, entry := range auditor.entries {
		counts[entry.Action]++
		for _, key := range []string{"accessToken", "refreshToken", "deviceCode", "userCode", "verificationHandle", "codeVerifier", "confirmationId", "receipt"} {
			if _, exists := entry.Metadata[key]; exists {
				t.Fatal("sensitive authentication field entered audit metadata")
			}
		}
	}
	for _, action := range []string{"cli.device.create", "cli.device.approve", "cli.device.claim", "cli.refresh", "cli.refresh.recover", "cli.refresh.replay", "cli.logout", "cli.device.cancel", "cli.device.code_lock"} {
		if counts[action] == 0 {
			t.Fatal("missing required CLI lifecycle audit action", action)
		}
	}
	if counts["cli.device.claim"] != 1 || counts["cli.device.code_lock"] != 1 {
		t.Fatal("receipt recovery or closed grant duplicated a lifecycle transition")
	}
	text := fmt.Sprint(auditor.entries)
	for _, secret := range []string{g.DeviceCode, handle, verifier, first.Tokens.AccessToken, first.Tokens.RefreshToken, next.Tokens.RefreshToken, "cli-test-only-password"} {
		if strings.Contains(text, secret) {
			t.Fatal("authentication secret entered audit output")
		}
	}
}

func testCLISessionPolicyAndAuthority(t *testing.T) {
	for _, expiry := range []string{"idle", "absolute"} {
		t.Run(expiry, func(t *testing.T) { testCLISessionExpiry(t, expiry) })
	}
	t.Run("current_permissions_and_disabled_user", testCLICurrentPermissionsAndDisabledUser)
	t.Run("sliding_idle_respects_absolute", testCLISlidingIdleRespectsAbsolute)
}

func testCLISlidingIdleRespectsAbsolute(t *testing.T) {
	f := newCLIIdentityFixture(t)
	first, err := f.service.LoginCLIWithPassword(f.ctx, f.owner, "cli-test-only-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Exec(`UPDATE sessions SET expires_at=NOW()+INTERVAL '2 minutes' WHERE id=?`, first.Tokens.SessionID).Error; err != nil {
		t.Fatal(err)
	}
	before := readCLISessionIdleDeadline(t, f.db, first.Tokens.SessionID)
	next, err := f.replica.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: first.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()})
	if err != nil || next.Tokens.RefreshExpiresAt == nil || !next.Tokens.RefreshExpiresAt.After(time.Now().Add(6*24*time.Hour)) || next.Tokens.SessionAbsoluteExpiresAt == nil || !next.Tokens.SessionAbsoluteExpiresAt.Equal(*first.Tokens.SessionAbsoluteExpiresAt) {
		t.Fatal("Activity failed to slide idle expiry or changed absolute lifetime")
	}
	persisted := readCLISessionIdleDeadline(t, f.db, first.Tokens.SessionID)
	if !persisted.After(before) || persisted.Sub(*next.Tokens.RefreshExpiresAt).Abs() > time.Microsecond {
		t.Fatal("Sliding idle expiry was not persisted at the returned deadline")
	}
	// Move only the persisted absolute deadline to a near-boundary fixture.
	absolute := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	if err = f.db.Exec(`UPDATE sessions SET metadata=jsonb_set(metadata::jsonb,'{cliAbsoluteExpiresAt}',to_jsonb(?::text)) WHERE id=?`, absolute.Format(time.RFC3339Nano), first.Tokens.SessionID).Error; err != nil {
		t.Fatal(err)
	}
	bounded, err := f.service.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: next.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()})
	if err != nil || bounded.Tokens.RefreshExpiresAt == nil || bounded.Tokens.SessionAbsoluteExpiresAt == nil || !bounded.Tokens.RefreshExpiresAt.Equal(absolute) || !bounded.Tokens.SessionAbsoluteExpiresAt.Equal(absolute) || bounded.Tokens.ExpiresAt.After(absolute) {
		t.Fatal("Sliding renewal exceeded the persistent absolute deadline")
	}
}

func readCLISessionIdleDeadline(t *testing.T, db *gorm.DB, sessionID string) time.Time {
	t.Helper()
	var row struct{ ExpiresAt time.Time }
	if err := db.Table("sessions").Select("expires_at").Where("id = ?", sessionID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ExpiresAt
}

func testCLISessionExpiry(t *testing.T, expiry string) {
	f := newCLIIdentityFixture(t)
	r, err := f.service.LoginCLIWithPassword(f.ctx, f.owner, "cli-test-only-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Tokens.RefreshExpiresAt == nil || time.Until(*r.Tokens.RefreshExpiresAt) < 7*24*time.Hour-time.Minute || r.Tokens.SessionAbsoluteExpiresAt == nil || time.Until(*r.Tokens.SessionAbsoluteExpiresAt) < 30*24*time.Hour-time.Minute {
		t.Fatal("default idle/absolute policy was not applied")
	}
	query := `UPDATE sessions SET expires_at=NOW()-INTERVAL '1 second' WHERE id=?`
	arguments := []any{r.Tokens.SessionID}
	if expiry == "absolute" {
		query = `UPDATE sessions SET metadata=jsonb_set(metadata::jsonb,'{cliAbsoluteExpiresAt}',to_jsonb(?::text)) WHERE id=?`
		arguments = []any{time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano), r.Tokens.SessionID}
	}
	if err := f.db.Exec(query, arguments...).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.replica.ParseAccessToken(f.ctx, r.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("expired persistent session still authorized access")
	}
	if _, err := f.replica.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: r.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("expired session was renewed")
	}
	if err := f.service.LogoutCLISession(f.ctx, r.Tokens.RefreshToken); err != nil {
		t.Fatal("expiry prevented opaque-proof logout")
	}
}

func testCLICurrentPermissionsAndDisabledUser(t *testing.T) {
	f := newCLIIdentityFixture(t)
	roles := policyrepo.New(f.db)
	wide, narrow := "cli-wide-"+uuid.NewString(), "cli-narrow-"+uuid.NewString()
	if _, err := roles.CreateRole(f.ctx, domainaccess.RoleInput{ID: wide, Name: wide, Scope: "global", PermissionKeys: []string{appaccess.PermObserveAIChatUse, "platform.clusters.view"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := roles.CreateRole(f.ctx, domainaccess.RoleInput{ID: narrow, Name: narrow, Scope: "global", PermissionKeys: []string{appaccess.PermObserveAIChatUse}}); err != nil {
		t.Fatal(err)
	}
	if err := f.users.ReplaceRoleBindings(f.ctx, f.owner, []string{wide}); err != nil {
		t.Fatal(err)
	}
	r, err := f.service.LoginCLIWithPassword(f.ctx, f.owner, "cli-test-only-password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.users.ReplaceRoleBindings(f.ctx, f.owner, []string{narrow}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.replica.ParseAccessToken(f.ctx, r.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("old authorization version remained valid")
	}
	next, err := f.replica.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: r.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()})
	if err != nil || len(next.User.PermissionKeys) != 1 || next.User.PermissionKeys[0] != appaccess.PermObserveAIChatUse {
		t.Fatal("refresh retained removed permissions")
	}
	if err := f.users.ReplaceRoleBindings(f.ctx, f.owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: next.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("empty permission intersection did not fail closed")
	}
	enabled, err := f.service.LoginCLIWithPassword(f.ctx, f.other, "cli-test-only-password", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`UPDATE users SET status='disabled',authz_version=authz_version+1 WHERE id=?`, f.other).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.service.ParseAccessToken(f.ctx, enabled.Tokens.AccessToken); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("disabled account retained access")
	}
	if _, err := f.service.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: enabled.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()}); err == nil {
		t.Fatal("disabled account refreshed its session")
	}
}

func testCLIV1Compatibility(t *testing.T) {
	f := newCLIIdentityFixture(t)
	_, _, r := f.browser(t, f.owner)
	first, err := f.service.RefreshSession(f.ctx, r.Tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.replica.RefreshSession(f.ctx, r.Tokens.RefreshToken)
	if err != nil || first.Tokens.RefreshToken != second.Tokens.RefreshToken || second.Tokens.ProtocolVersion != 0 {
		t.Fatal("v1 refresh changed", err)
	}
	if _, err := f.service.RefreshCLISession(f.ctx, domainidentity.CLIRefreshRequest{RefreshToken: r.Tokens.RefreshToken, RefreshRequestID: uuid.NewString()}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("v1 JWT was treated as v2 refresh")
	}
}
