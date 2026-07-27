package mfa

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"golang.org/x/crypto/bcrypt"
)

func TestTOTPCodeRFC6238Vector(t *testing.T) {
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := totpCode(secret, time.Unix(59, 0).Unix()/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" || !verifyTOTP(secret, code, time.Unix(59, 0)) {
		t.Fatalf("code = %q, want 287082", code)
	}
	if verifyTOTP(secret, code, time.Unix(59+2*totpPeriod, 0)) {
		t.Fatal("code outside the one-step clock skew window was accepted")
	}
}

func TestTOTPEnrollmentChallengeCanOnlyCompleteOnce(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := testService(t, repo, now)
	principal := domainidentity.Principal{UserID: "user-1", UserName: "alice"}
	challenge, err := service.BeginTOTPEnrollment(t.Context(), principal, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(challenge.ProvisioningURI)
	if err != nil {
		t.Fatal(err)
	}
	secret := uri.Query().Get("secret")
	code, err := totpCode(secret, now.Unix()/totpPeriod)
	if err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, verifyErr := service.VerifyChallenge(context.Background(), principal, "session-1", challenge.ChallengeID, code); verifyErr == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful concurrent completions = %d, want 1", successes.Load())
	}
	if len(repo.credentials) != 1 || strings.Contains(repo.credentials[0].SecretCiphertext, secret) {
		t.Fatalf("stored credential was duplicated or leaked secret: %#v", repo.credentials)
	}
}

func TestRecoveryCodesAreHashedAndShownOnce(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.recentStepUp = true
	service := testService(t, repo, now)
	result, err := service.RegenerateRecoveryCodes(t.Context(), domainidentity.Principal{UserID: "user-1"}, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Codes) != recoveryCodeCount || len(repo.recoveryCodes) != recoveryCodeCount {
		t.Fatalf("code counts = %d/%d", len(result.Codes), len(repo.recoveryCodes))
	}
	for index, code := range result.Codes {
		stored := repo.recoveryCodes[index].CodeHash
		if strings.Contains(stored, code) || bcrypt.CompareHashAndPassword([]byte(stored), []byte(normalizeRecoveryCode(code))) != nil {
			t.Fatalf("recovery code %d was not stored exclusively as a valid hash", index)
		}
	}
}

func TestRecoveryChallengeWorksWithoutTOTPCredential(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	hash, err := bcrypt.GenerateFromPassword([]byte(normalizeRecoveryCode("ABCDEFGH-JKLMNPQR")), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo.recoveryCodes = []domainmfa.RecoveryCode{{ID: "code-1", UserID: "user-1", CodeHash: string(hash)}}
	service := testService(t, repo, now)
	principal := domainidentity.Principal{UserID: "user-1"}

	challenge, err := service.BeginRecoveryChallenge(t.Context(), principal, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.VerifyChallenge(t.Context(), principal, "session-1", challenge.ChallengeID, "ABCDEFGH-JKLMNPQR")
	if err != nil || !result.Verified || len(result.AuthenticationMethods) != 2 || result.AuthenticationMethods[1] != "recovery_code" {
		t.Fatalf("recovery result = %#v, error = %v", result, err)
	}
}

func TestAdminResetUserMFARevokesCredentialsCodesChallengesAndSessions(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	repo.credentials = []domainmfa.Credential{{ID: "credential-1", UserID: "user-1"}, {ID: "credential-2", UserID: "user-1"}}
	repo.recoveryCodes = []domainmfa.RecoveryCode{{ID: "code-1", UserID: "user-1"}}
	repo.challenges["challenge-1"] = domainmfa.Challenge{ID: "challenge-1", UserID: "user-1"}
	service := testService(t, repo, now)
	service.SetPermissionResolver(appaccess.NewPermissionResolver(mfaRolePermissions{"admin": {appaccess.PermAccessUsersManage}}))
	principal := domainidentity.Principal{UserID: "admin-1", Roles: []string{"admin"}}

	result, err := service.AdminResetUserMFA(t.Context(), principal, "user-1", sohaapi.MFAAdminResetRequest{Reason: "lost device", RevokeSessions: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.UserID != "user-1" || result.ResetAt != now || result.RevokedCredentialCount != 2 || result.RecoveryCodesRevoked != 1 || result.SessionsRevoked != 1 {
		t.Fatalf("unexpected reset result: %#v", result)
	}
}

func TestInvalidTOTPIncrementsAttemptWithoutSecretInError(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := testService(t, repo, now)
	principal := domainidentity.Principal{UserID: "user-1", UserName: "alice"}
	challenge, err := service.BeginTOTPEnrollment(t.Context(), principal, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.VerifyChallenge(t.Context(), principal, "session-1", challenge.ChallengeID, "000000")
	if !errors.Is(err, apperrors.ErrUnauthorized) || repo.challenges[challenge.ChallengeID].Attempts != 1 {
		t.Fatalf("verify error/attempts = %v/%d", err, repo.challenges[challenge.ChallengeID].Attempts)
	}
	if strings.Contains(err.Error(), "otpauth") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked enrollment material: %v", err)
	}
}

func TestWebAuthnAuthenticationCompletesStepUpOnce(t *testing.T) {
	now := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := testService(t, repo, now)
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(service.keys, `{"id":"credential"}`)
	if err != nil {
		t.Fatal(err)
	}
	repo.credentials = []domainmfa.Credential{{ID: "credential-1", UserID: "user-1", Type: domainmfa.CredentialTypeWebAuthn, ExternalID: "Y3JlZGVudGlhbA", SecretCiphertext: ciphertext, SignCount: 1}}
	service.webauthn = webAuthnAuthenticationStub{}
	principal := domainidentity.Principal{UserID: "user-1", UserName: "alice"}
	challenge, err := service.BeginWebAuthnAuthentication(t.Context(), principal, "session-1", sohaapi.MFAWebAuthnAuthenticationRequest{Purpose: sohaapi.MFAWebAuthnPurposeStepUp, ApplicationID: "app-1"})
	if err != nil {
		t.Fatal(err)
	}
	response := sohaapi.MFAWebAuthnResponse{CredentialID: "Y3JlZGVudGlhbA", ClientDataJSON: "e30", AuthenticatorData: "YXV0aA", Signature: "c2ln"}
	result, err := service.VerifyWebAuthnChallenge(t.Context(), principal, "session-1", challenge.ChallengeID, response)
	if err != nil || !result.Verified || !repo.recentStepUp {
		t.Fatalf("VerifyWebAuthnChallenge = %#v, %v; recentStepUp=%v", result, err, repo.recentStepUp)
	}
	if _, err := service.VerifyWebAuthnChallenge(t.Context(), principal, "session-1", challenge.ChallengeID, response); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("replayed WebAuthn challenge error = %v, want conflict", err)
	}
}

type webAuthnStub struct{}

func (webAuthnStub) BeginRegistration(domainmfa.WebAuthnUser) (domainmfa.WebAuthnRegistration, error) {
	return domainmfa.WebAuthnRegistration{}, errors.New("not used")
}

func (webAuthnStub) FinishRegistration(domainmfa.WebAuthnUser, []byte, domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error) {
	return domainmfa.WebAuthnCredential{}, errors.New("not used")
}

func (webAuthnStub) BeginAuthentication(domainmfa.WebAuthnUser) (domainmfa.WebAuthnAuthentication, error) {
	return domainmfa.WebAuthnAuthentication{}, errors.New("not used")
}

func (webAuthnStub) FinishAuthentication(domainmfa.WebAuthnUser, []byte, domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error) {
	return domainmfa.WebAuthnCredential{}, errors.New("not used")
}

type webAuthnAuthenticationStub struct{ webAuthnStub }

func (webAuthnAuthenticationStub) BeginAuthentication(domainmfa.WebAuthnUser) (domainmfa.WebAuthnAuthentication, error) {
	return domainmfa.WebAuthnAuthentication{Challenge: "challenge", RPID: "localhost", AllowCredentialIDs: []string{"Y3JlZGVudGlhbA"}, UserVerification: "required", Timeout: time.Minute, Session: []byte(`{"challenge":"challenge"}`)}, nil
}

func (webAuthnAuthenticationStub) FinishAuthentication(_ domainmfa.WebAuthnUser, _ []byte, response domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error) {
	if response.Signature == "" || response.AuthenticatorData == "" {
		return domainmfa.WebAuthnCredential{}, errors.New("invalid assertion")
	}
	return domainmfa.WebAuthnCredential{ExternalID: response.CredentialID, Data: []byte(`{"id":"credential","signCount":2}`), SignCount: 2}, nil
}

type userStoreStub struct{}

type mfaRolePermissions map[string][]string

func (permissions mfaRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return permissions, nil
}

func (userStoreStub) GetByID(context.Context, string) (domainidentity.User, error) {
	return domainidentity.User{ID: "user-1", Username: "alice", DisplayName: "Alice"}, nil
}

func testService(t *testing.T, repo *memoryRepository, now time.Time) *Service {
	t.Helper()
	key, err := keyring.NewKey("test", "test-encryption-key-32-bytes-long", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(repo, userStoreStub{}, webAuthnStub{}, keys, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	return service
}

type memoryRepository struct {
	mu            sync.Mutex
	credentials   []domainmfa.Credential
	challenges    map[string]domainmfa.Challenge
	recoveryCodes []domainmfa.RecoveryCode
	recentStepUp  bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{challenges: map[string]domainmfa.Challenge{}}
}

func (r *memoryRepository) ListCredentials(context.Context, string) ([]domainmfa.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domainmfa.Credential(nil), r.credentials...), nil
}

func (r *memoryRepository) ActiveCredential(_ context.Context, userID, credentialType string) (domainmfa.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, credential := range r.credentials {
		if credential.UserID == userID && credential.Type == credentialType && credential.RevokedAt == nil {
			return credential, nil
		}
	}
	return domainmfa.Credential{}, fmtError(apperrors.ErrNotFound)
}

func (r *memoryRepository) CreateChallenge(_ context.Context, challenge domainmfa.Challenge) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.challenges[challenge.ID] = challenge
	return nil
}

func (r *memoryRepository) GetChallenge(_ context.Context, id, userID, sessionID string, now time.Time) (domainmfa.Challenge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	challenge, ok := r.challenges[id]
	if !ok || challenge.UserID != userID || challenge.SessionID != sessionID || challenge.ConsumedAt != nil || !challenge.ExpiresAt.After(now) || challenge.Attempts >= challenge.MaxAttempts {
		return domainmfa.Challenge{}, fmtError(apperrors.ErrConflict)
	}
	return challenge, nil
}

func (r *memoryRepository) IncrementChallengeAttempt(_ context.Context, id, _, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	challenge := r.challenges[id]
	challenge.Attempts++
	r.challenges[id] = challenge
	return nil
}

func (r *memoryRepository) CompleteChallenge(_ context.Context, challenge domainmfa.Challenge, credential *domainmfa.Credential, _ string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.challenges[challenge.ID]
	if current.ConsumedAt != nil {
		return fmtError(apperrors.ErrConflict)
	}
	current.ConsumedAt = &now
	r.challenges[challenge.ID] = current
	if credential != nil {
		r.credentials = append(r.credentials, *credential)
	}
	r.recentStepUp = true
	return nil
}

func (r *memoryRepository) CompleteWebAuthnChallenge(_ context.Context, challenge domainmfa.Challenge, _ domainmfa.Credential, _ []byte, _ uint32, _ string, now time.Time) error {
	return r.CompleteChallenge(context.Background(), challenge, nil, "webauthn", now)
}

func (r *memoryRepository) RevokeCredential(context.Context, string, string, time.Time) error {
	return nil
}

func (r *memoryRepository) ResetMFA(_ context.Context, _ string, revokeSessions bool, _ time.Time) (domainmfa.AdminResetCounts, error) {
	counts := domainmfa.AdminResetCounts{RevokedCredentials: len(r.credentials), RevokedRecoveryCodes: len(r.recoveryCodes), RevokedChallenges: len(r.challenges)}
	if revokeSessions {
		counts.RevokedSessions = 1
	}
	return counts, nil
}

func (r *memoryRepository) ReplaceRecoveryCodes(_ context.Context, _ string, codes []domainmfa.RecoveryCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoveryCodes = append([]domainmfa.RecoveryCode(nil), codes...)
	return nil
}

func (r *memoryRepository) ListRecoveryCodes(context.Context, string) ([]domainmfa.RecoveryCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domainmfa.RecoveryCode(nil), r.recoveryCodes...), nil
}

func (r *memoryRepository) ConsumeRecoveryCodeAndStepUp(context.Context, string, string, string, string, time.Time) error {
	return nil
}

func (r *memoryRepository) HasRecentStepUp(context.Context, string, string, time.Time) (bool, error) {
	return r.recentStepUp, nil
}

func fmtError(target error) error { return errors.Join(target, errors.New("test")) }

var _ = sohaapi.MFAChallengeResult{}
