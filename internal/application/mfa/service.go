package mfa

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"golang.org/x/crypto/bcrypt"
)

const (
	challengeTTL       = 5 * time.Minute
	recentStepUpWindow = 10 * time.Minute
	recoveryCodeCount  = 10
)

type Repository interface {
	ListCredentials(context.Context, string) ([]domainmfa.Credential, error)
	ActiveCredential(context.Context, string, string) (domainmfa.Credential, error)
	CreateChallenge(context.Context, domainmfa.Challenge) error
	GetChallenge(context.Context, string, string, string, time.Time) (domainmfa.Challenge, error)
	IncrementChallengeAttempt(context.Context, string, string, string) error
	CompleteChallenge(context.Context, domainmfa.Challenge, *domainmfa.Credential, string, time.Time) error
	CompleteWebAuthnChallenge(context.Context, domainmfa.Challenge, domainmfa.Credential, []byte, uint32, string, time.Time) error
	RevokeCredential(context.Context, string, string, time.Time) error
	ResetMFA(context.Context, string, bool, time.Time) (domainmfa.AdminResetCounts, error)
	ReplaceRecoveryCodes(context.Context, string, []domainmfa.RecoveryCode) error
	ListRecoveryCodes(context.Context, string) ([]domainmfa.RecoveryCode, error)
	ConsumeRecoveryCodeAndStepUp(context.Context, string, string, string, string, time.Time) error
	HasRecentStepUp(context.Context, string, string, time.Time) (bool, error)
}

type UserStore interface {
	GetByID(context.Context, string) (domainidentity.User, error)
}

type WebAuthn interface {
	BeginRegistration(domainmfa.WebAuthnUser) (domainmfa.WebAuthnRegistration, error)
	FinishRegistration(domainmfa.WebAuthnUser, []byte, domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error)
	BeginAuthentication(domainmfa.WebAuthnUser) (domainmfa.WebAuthnAuthentication, error)
	FinishAuthentication(domainmfa.WebAuthnUser, []byte, domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repository  Repository
	users       UserStore
	webauthn    WebAuthn
	keys        keyring.Ring
	audit       AuditRecorder
	now         func() time.Time
	permissions *appaccess.PermissionResolver
}

func (s *Service) SetPermissionResolver(resolver *appaccess.PermissionResolver) {
	s.permissions = resolver
}

func New(repository Repository, users UserStore, webauthn WebAuthn, keys keyring.Ring, audit AuditRecorder) (*Service, error) {
	if repository == nil || users == nil || webauthn == nil || keys.Active().ID() == "" {
		return nil, fmt.Errorf("MFA repository, user store, WebAuthn verifier, and encryption key are required")
	}
	return &Service{repository: repository, users: users, webauthn: webauthn, keys: keys, audit: audit, now: time.Now}, nil
}

func (s *Service) ListCredentials(ctx context.Context, principal domainidentity.Principal) ([]sohaapi.MFACredential, error) {
	items, err := s.repository.ListCredentials(ctx, principal.UserID)
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.MFACredential, 0, len(items))
	for _, item := range items {
		credentialType := sohaapi.MFACredentialType(item.Type)
		result = append(result, sohaapi.MFACredential{
			ID: item.ID, Type: credentialType, DisplayName: item.DisplayName,
			CreatedAt: item.CreatedAt, LastUsedAt: item.LastUsedAt,
		})
	}
	return result, nil
}

func (s *Service) RevokeCredential(ctx context.Context, principal domainidentity.Principal, sessionID, credentialID string) error {
	if err := s.requireRecentStepUp(ctx, principal.UserID, sessionID); err != nil {
		return err
	}
	if err := s.repository.RevokeCredential(ctx, principal.UserID, credentialID, s.now().UTC()); err != nil {
		return err
	}
	s.record(ctx, principal, "identity.mfa.credential.revoke", "success", credentialID, nil)
	return nil
}

func (s *Service) AdminRevokeCredential(ctx context.Context, principal domainidentity.Principal, userID, credentialID string) (sohaapi.OperationStatus, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermAccessUsersManage, "update")); err != nil {
		return sohaapi.OperationStatus{}, err
	}
	if err := s.repository.RevokeCredential(ctx, strings.TrimSpace(userID), strings.TrimSpace(credentialID), s.now().UTC()); err != nil {
		return sohaapi.OperationStatus{}, err
	}
	s.record(ctx, principal, "identity.mfa.credential.admin_revoke", "success", credentialID, map[string]any{"userId": userID})
	return sohaapi.OperationStatus{Status: "revoked"}, nil
}

func (s *Service) AdminResetUserMFA(ctx context.Context, principal domainidentity.Principal, userID string, request sohaapi.MFAAdminResetRequest) (sohaapi.MFAAdminResetResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.ManagedActionPermission(appaccess.PermAccessUsersManage, "update")); err != nil {
		return sohaapi.MFAAdminResetResult{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.TrimSpace(request.Reason) == "" || len(strings.TrimSpace(request.Reason)) > 500 {
		return sohaapi.MFAAdminResetResult{}, fmt.Errorf("%w: user ID and reset reason are required", apperrors.ErrInvalidArgument)
	}
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return sohaapi.MFAAdminResetResult{}, err
	}
	now := s.now().UTC()
	counts, err := s.repository.ResetMFA(ctx, userID, request.RevokeSessions, now)
	if err != nil {
		return sohaapi.MFAAdminResetResult{}, err
	}
	s.record(ctx, principal, "identity.mfa.admin_reset", "success", userID, map[string]any{"reasonProvided": true, "revokeSessions": request.RevokeSessions, "revokedChallenges": counts.RevokedChallenges})
	return sohaapi.MFAAdminResetResult{UserID: userID, ResetAt: now, RevokedCredentialCount: counts.RevokedCredentials, RecoveryCodesRevoked: counts.RevokedRecoveryCodes, SessionsRevoked: counts.RevokedSessions}, nil
}

func (s *Service) BeginTOTPEnrollment(ctx context.Context, principal domainidentity.Principal, sessionID string) (sohaapi.MFAEnrollmentChallenge, error) {
	now := s.now().UTC()
	challenge := domainmfa.Challenge{
		ID: uuid.NewString(), UserID: principal.UserID, SessionID: strings.TrimSpace(sessionID),
		Type: domainmfa.ChallengeTypeTOTPStepUp, MaxAttempts: 5,
		ExpiresAt: now.Add(challengeTTL), CreatedAt: now,
	}
	provisioning := ""
	if _, err := s.repository.ActiveCredential(ctx, principal.UserID, domainmfa.CredentialTypeTOTP); err != nil {
		if !errors.Is(err, apperrors.ErrNotFound) {
			return sohaapi.MFAEnrollmentChallenge{}, err
		}
		secret, generateErr := newTOTPSecret()
		if generateErr != nil {
			return sohaapi.MFAEnrollmentChallenge{}, generateErr
		}
		ciphertext, encryptErr := secretcrypto.EncryptStringWithKeyring(s.keys, secret)
		if encryptErr != nil {
			return sohaapi.MFAEnrollmentChallenge{}, fmt.Errorf("encrypt TOTP secret: %w", encryptErr)
		}
		challenge.Type = domainmfa.ChallengeTypeTOTPEnrollment
		challenge.SecretCiphertext = ciphertext
		account := principal.UserName
		if account == "" {
			account = principal.Email
		}
		provisioning = provisioningURI("Soha", account, secret)
	}
	if err := s.repository.CreateChallenge(ctx, challenge); err != nil {
		return sohaapi.MFAEnrollmentChallenge{}, err
	}
	return sohaapi.MFAEnrollmentChallenge{
		ChallengeID: challenge.ID, ExpiresAt: challenge.ExpiresAt,
		ProvisioningURI: provisioning, Type: sohaapi.TotpEnrollment,
	}, nil
}

func (s *Service) BeginRecoveryChallenge(ctx context.Context, principal domainidentity.Principal, sessionID string) (sohaapi.MFARecoveryChallenge, error) {
	now := s.now().UTC()
	challenge := domainmfa.Challenge{
		ID: uuid.NewString(), UserID: principal.UserID, SessionID: strings.TrimSpace(sessionID),
		Type: domainmfa.ChallengeTypeRecoveryCode, MaxAttempts: 5,
		ExpiresAt: now.Add(challengeTTL), CreatedAt: now,
	}
	if err := s.repository.CreateChallenge(ctx, challenge); err != nil {
		return sohaapi.MFARecoveryChallenge{}, err
	}
	return sohaapi.MFARecoveryChallenge{ChallengeID: challenge.ID, ExpiresAt: challenge.ExpiresAt}, nil
}

func (s *Service) VerifyChallenge(ctx context.Context, principal domainidentity.Principal, sessionID, challengeID, response string) (sohaapi.MFAChallengeResult, error) {
	now := s.now().UTC()
	challenge, err := s.repository.GetChallenge(ctx, challengeID, principal.UserID, sessionID, now)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	if len(strings.TrimSpace(response)) > 128 {
		_ = s.repository.IncrementChallengeAttempt(ctx, challenge.ID, challenge.UserID, challenge.SessionID)
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("%w: invalid MFA response", apperrors.ErrInvalidArgument)
	}
	if challenge.Type == domainmfa.ChallengeTypeRecoveryCode {
		return s.verifyRecoveryChallenge(ctx, principal, sessionID, challenge, response, now)
	}
	secret, credential, method, err := s.challengeSecret(ctx, challenge)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	if !verifyTOTP(secret, response, now) {
		if challenge.Type == domainmfa.ChallengeTypeTOTPStepUp {
			if codeID, ok := s.matchRecoveryCode(ctx, principal.UserID, response); ok {
				if err := s.repository.ConsumeRecoveryCodeAndStepUp(ctx, challenge.ID, codeID, principal.UserID, sessionID, now); err != nil {
					return sohaapi.MFAChallengeResult{}, err
				}
				s.record(ctx, principal, "identity.mfa.challenge.verify", "success", challenge.ID, map[string]any{"method": "recovery_code"})
				return challengeResult(now, "recovery_code"), nil
			}
		}
		_ = s.repository.IncrementChallengeAttempt(ctx, challenge.ID, challenge.UserID, challenge.SessionID)
		s.record(ctx, principal, "identity.mfa.challenge.verify", "deny", challenge.ID, map[string]any{"method": method})
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("%w: invalid MFA response", apperrors.ErrUnauthorized)
	}
	if err := s.repository.CompleteChallenge(ctx, challenge, credential, method, now); err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	s.record(ctx, principal, "identity.mfa.challenge.verify", "success", challenge.ID, map[string]any{"method": method})
	return challengeResult(now, method), nil
}

func (s *Service) verifyRecoveryChallenge(ctx context.Context, principal domainidentity.Principal, sessionID string, challenge domainmfa.Challenge, response string, now time.Time) (sohaapi.MFAChallengeResult, error) {
	codeID, ok := s.matchRecoveryCode(ctx, principal.UserID, response)
	if !ok {
		_ = s.repository.IncrementChallengeAttempt(ctx, challenge.ID, challenge.UserID, challenge.SessionID)
		s.record(ctx, principal, "identity.mfa.challenge.verify", "deny", challenge.ID, map[string]any{"method": "recovery_code"})
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("%w: invalid MFA response", apperrors.ErrUnauthorized)
	}
	if err := s.repository.ConsumeRecoveryCodeAndStepUp(ctx, challenge.ID, codeID, principal.UserID, sessionID, now); err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	s.record(ctx, principal, "identity.mfa.challenge.verify", "success", challenge.ID, map[string]any{"method": "recovery_code"})
	return challengeResult(now, "recovery_code"), nil
}

func (s *Service) BeginWebAuthnEnrollment(ctx context.Context, principal domainidentity.Principal, sessionID string) (sohaapi.MFAWebAuthnCreationOptions, error) {
	user, err := s.users.GetByID(ctx, principal.UserID)
	if err != nil {
		return sohaapi.MFAWebAuthnCreationOptions{}, fmt.Errorf("load WebAuthn user: %w", err)
	}
	credentials, err := s.repository.ListCredentials(ctx, principal.UserID)
	if err != nil {
		return sohaapi.MFAWebAuthnCreationOptions{}, err
	}
	webUser := domainmfa.WebAuthnUser{ID: []byte(user.ID), Name: user.Username, DisplayName: user.DisplayName}
	for _, credential := range credentials {
		if credential.Type != domainmfa.CredentialTypeWebAuthn || credential.ExternalID == "" {
			continue
		}
		if decoded, decodeErr := base64.RawURLEncoding.DecodeString(credential.ExternalID); decodeErr == nil {
			webUser.Credentials = append(webUser.Credentials, decoded)
		}
	}
	registration, err := s.webauthn.BeginRegistration(webUser)
	if err != nil {
		return sohaapi.MFAWebAuthnCreationOptions{}, err
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(registration.Session))
	if err != nil {
		return sohaapi.MFAWebAuthnCreationOptions{}, fmt.Errorf("encrypt WebAuthn challenge: %w", err)
	}
	now := s.now().UTC()
	challenge := domainmfa.Challenge{
		ID: uuid.NewString(), UserID: principal.UserID, SessionID: strings.TrimSpace(sessionID),
		Type: domainmfa.ChallengeTypeWebAuthnEnrollment, SecretCiphertext: ciphertext,
		MaxAttempts: 5, ExpiresAt: now.Add(challengeTTL), CreatedAt: now,
	}
	if err := s.repository.CreateChallenge(ctx, challenge); err != nil {
		return sohaapi.MFAWebAuthnCreationOptions{}, err
	}
	return sohaapi.MFAWebAuthnCreationOptions{
		Challenge: registration.Challenge, ChallengeID: challenge.ID,
		RpID: registration.RPID, RpName: registration.RPName,
		UserID: registration.UserID, UserName: registration.UserName,
		Algorithms: registration.Algorithms, ExcludeCredentialIDs: registration.ExcludeCredentialIDs,
		TimeoutMilliseconds: int(registration.Timeout.Milliseconds()), ExpiresAt: challenge.ExpiresAt,
	}, nil
}

type webAuthnChallengeState struct {
	Session       []byte `json:"session"`
	Purpose       string `json:"purpose"`
	ApplicationID string `json:"applicationId,omitempty"`
}

func (s *Service) BeginWebAuthnAuthentication(ctx context.Context, principal domainidentity.Principal, sessionID string, request sohaapi.MFAWebAuthnAuthenticationRequest) (sohaapi.MFAWebAuthnRequestOptions, error) {
	if !request.Purpose.Valid() {
		return sohaapi.MFAWebAuthnRequestOptions{}, fmt.Errorf("%w: invalid WebAuthn purpose", apperrors.ErrInvalidArgument)
	}
	user, err := s.webAuthnUser(ctx, principal.UserID)
	if err != nil {
		return sohaapi.MFAWebAuthnRequestOptions{}, err
	}
	authentication, err := s.webauthn.BeginAuthentication(user)
	if err != nil {
		return sohaapi.MFAWebAuthnRequestOptions{}, err
	}
	state, err := json.Marshal(webAuthnChallengeState{Session: authentication.Session, Purpose: string(request.Purpose), ApplicationID: strings.TrimSpace(request.ApplicationID)})
	if err != nil {
		return sohaapi.MFAWebAuthnRequestOptions{}, fmt.Errorf("marshal WebAuthn challenge: %w", err)
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(state))
	if err != nil {
		return sohaapi.MFAWebAuthnRequestOptions{}, fmt.Errorf("encrypt WebAuthn challenge: %w", err)
	}
	now := s.now().UTC()
	challenge := domainmfa.Challenge{ID: uuid.NewString(), UserID: principal.UserID, SessionID: strings.TrimSpace(sessionID), Type: domainmfa.ChallengeTypeWebAuthnAuthentication, SecretCiphertext: ciphertext, MaxAttempts: 5, ExpiresAt: now.Add(challengeTTL), CreatedAt: now}
	if err := s.repository.CreateChallenge(ctx, challenge); err != nil {
		return sohaapi.MFAWebAuthnRequestOptions{}, err
	}
	return sohaapi.MFAWebAuthnRequestOptions{Challenge: authentication.Challenge, ChallengeID: challenge.ID, ExpiresAt: challenge.ExpiresAt, RpID: authentication.RPID, TimeoutMilliseconds: int(authentication.Timeout.Milliseconds()), UserVerification: sohaapi.MFAWebAuthnRequestOptionsUserVerification(authentication.UserVerification), AllowCredentialIDs: authentication.AllowCredentialIDs}, nil
}

func (s *Service) VerifyWebAuthnChallenge(ctx context.Context, principal domainidentity.Principal, sessionID, challengeID string, response sohaapi.MFAWebAuthnResponse) (sohaapi.MFAChallengeResult, error) {
	now := s.now().UTC()
	challenge, err := s.repository.GetChallenge(ctx, challengeID, principal.UserID, sessionID, now)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	if challenge.Type == domainmfa.ChallengeTypeWebAuthnAuthentication {
		return s.verifyWebAuthnAuthentication(ctx, principal, challenge, response, now)
	}
	if challenge.Type != domainmfa.ChallengeTypeWebAuthnEnrollment {
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("%w: challenge type mismatch", apperrors.ErrInvalidArgument)
	}
	sessionData, err := secretcrypto.DecryptStringWithKeyring(s.keys, challenge.SecretCiphertext)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("decrypt WebAuthn challenge: %w", err)
	}
	user, err := s.users.GetByID(ctx, principal.UserID)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	credential, err := s.webauthn.FinishRegistration(domainmfa.WebAuthnUser{
		ID: []byte(user.ID), Name: user.Username, DisplayName: user.DisplayName,
	}, []byte(sessionData), domainmfa.WebAuthnResponse{
		CredentialID: response.CredentialID, ClientDataJSON: response.ClientDataJSON,
		AttestationObject: response.AttestationObject,
	})
	if err != nil {
		_ = s.repository.IncrementChallengeAttempt(ctx, challenge.ID, challenge.UserID, challenge.SessionID)
		s.record(ctx, principal, "identity.mfa.webauthn.verify", "deny", challenge.ID, nil)
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("%w: invalid WebAuthn response", apperrors.ErrUnauthorized)
	}
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(credential.Data))
	if err != nil {
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("encrypt WebAuthn credential: %w", err)
	}
	stored := &domainmfa.Credential{
		ID: uuid.NewString(), UserID: principal.UserID, Type: domainmfa.CredentialTypeWebAuthn,
		DisplayName: "Passkey", ExternalID: credential.ExternalID,
		SecretCiphertext: encrypted, SignCount: credential.SignCount, CreatedAt: now,
	}
	if err := s.repository.CompleteChallenge(ctx, challenge, stored, domainmfa.CredentialTypeWebAuthn, now); err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	s.record(ctx, principal, "identity.mfa.webauthn.verify", "success", challenge.ID, nil)
	return challengeResult(now, domainmfa.CredentialTypeWebAuthn), nil
}

func (s *Service) verifyWebAuthnAuthentication(ctx context.Context, principal domainidentity.Principal, challenge domainmfa.Challenge, response sohaapi.MFAWebAuthnResponse, now time.Time) (sohaapi.MFAChallengeResult, error) {
	plain, err := secretcrypto.DecryptStringWithKeyring(s.keys, challenge.SecretCiphertext)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("decrypt WebAuthn challenge: %w", err)
	}
	var state webAuthnChallengeState
	if err := json.Unmarshal([]byte(plain), &state); err != nil {
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("decode WebAuthn challenge: %w", err)
	}
	user, err := s.webAuthnUser(ctx, principal.UserID)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	verified, err := s.webauthn.FinishAuthentication(user, state.Session, contractWebAuthnResponse(response))
	if err != nil {
		_ = s.repository.IncrementChallengeAttempt(ctx, challenge.ID, challenge.UserID, challenge.SessionID)
		s.record(ctx, principal, "identity.mfa.webauthn.authenticate", "deny", challenge.ID, nil)
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("%w: invalid WebAuthn response", apperrors.ErrUnauthorized)
	}
	credential, err := s.webAuthnCredential(ctx, principal.UserID, verified.ExternalID)
	if err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, string(verified.Data))
	if err != nil {
		return sohaapi.MFAChallengeResult{}, fmt.Errorf("encrypt WebAuthn credential: %w", err)
	}
	if err := s.repository.CompleteWebAuthnChallenge(ctx, challenge, credential, []byte(encrypted), verified.SignCount, domainmfa.CredentialTypeWebAuthn, now); err != nil {
		return sohaapi.MFAChallengeResult{}, err
	}
	s.record(ctx, principal, "identity.mfa.webauthn.authenticate", "success", challenge.ID, map[string]any{"purpose": state.Purpose, "applicationId": state.ApplicationID})
	return challengeResult(now, domainmfa.CredentialTypeWebAuthn), nil
}

func contractWebAuthnResponse(response sohaapi.MFAWebAuthnResponse) domainmfa.WebAuthnResponse {
	return domainmfa.WebAuthnResponse{CredentialID: response.CredentialID, ClientDataJSON: response.ClientDataJSON, AttestationObject: response.AttestationObject, AuthenticatorData: response.AuthenticatorData, Signature: response.Signature, UserHandle: response.UserHandle}
}

func (s *Service) webAuthnUser(ctx context.Context, userID string) (domainmfa.WebAuthnUser, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domainmfa.WebAuthnUser{}, err
	}
	items, err := s.repository.ListCredentials(ctx, userID)
	if err != nil {
		return domainmfa.WebAuthnUser{}, err
	}
	result := domainmfa.WebAuthnUser{ID: []byte(user.ID), Name: user.Username, DisplayName: user.DisplayName}
	for _, item := range items {
		if item.Type != domainmfa.CredentialTypeWebAuthn || item.ExternalID == "" {
			continue
		}
		data, decryptErr := secretcrypto.DecryptStringWithKeyring(s.keys, item.SecretCiphertext)
		if decryptErr != nil {
			return domainmfa.WebAuthnUser{}, fmt.Errorf("decrypt WebAuthn credential: %w", decryptErr)
		}
		result.CredentialRecords = append(result.CredentialRecords, domainmfa.WebAuthnCredential{ExternalID: item.ExternalID, Data: []byte(data), SignCount: item.SignCount})
	}
	return result, nil
}

func (s *Service) webAuthnCredential(ctx context.Context, userID, externalID string) (domainmfa.Credential, error) {
	items, err := s.repository.ListCredentials(ctx, userID)
	if err != nil {
		return domainmfa.Credential{}, err
	}
	for _, item := range items {
		if item.Type == domainmfa.CredentialTypeWebAuthn && item.ExternalID == externalID {
			return item, nil
		}
	}
	return domainmfa.Credential{}, fmt.Errorf("%w: WebAuthn credential not found", apperrors.ErrNotFound)
}

func (s *Service) RegenerateRecoveryCodes(ctx context.Context, principal domainidentity.Principal, sessionID string) (sohaapi.MFARecoveryCodeSet, error) {
	if err := s.requireRecentStepUp(ctx, principal.UserID, sessionID); err != nil {
		return sohaapi.MFARecoveryCodeSet{}, err
	}
	now := s.now().UTC()
	plain := make([]string, 0, recoveryCodeCount)
	stored := make([]domainmfa.RecoveryCode, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			return sohaapi.MFARecoveryCodeSet{}, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(normalizeRecoveryCode(code)), bcrypt.DefaultCost)
		if err != nil {
			return sohaapi.MFARecoveryCodeSet{}, fmt.Errorf("hash recovery code: %w", err)
		}
		plain = append(plain, code)
		stored = append(stored, domainmfa.RecoveryCode{ID: uuid.NewString(), UserID: principal.UserID, CodeHash: string(hash), CreatedAt: now})
	}
	if err := s.repository.ReplaceRecoveryCodes(ctx, principal.UserID, stored); err != nil {
		return sohaapi.MFARecoveryCodeSet{}, err
	}
	s.record(ctx, principal, "identity.mfa.recovery_codes.regenerate", "success", principal.UserID, map[string]any{"count": len(stored)})
	return sohaapi.MFARecoveryCodeSet{Codes: plain, GeneratedAt: now}, nil
}

func (s *Service) challengeSecret(ctx context.Context, challenge domainmfa.Challenge) (string, *domainmfa.Credential, string, error) {
	if challenge.Type == domainmfa.ChallengeTypeTOTPEnrollment {
		secret, err := secretcrypto.DecryptStringWithKeyring(s.keys, challenge.SecretCiphertext)
		if err != nil {
			return "", nil, "", fmt.Errorf("decrypt TOTP enrollment: %w", err)
		}
		credential := &domainmfa.Credential{
			ID: uuid.NewString(), UserID: challenge.UserID, Type: domainmfa.CredentialTypeTOTP,
			DisplayName: "Authenticator app", SecretCiphertext: challenge.SecretCiphertext,
			CreatedAt: s.now().UTC(),
		}
		return secret, credential, domainmfa.CredentialTypeTOTP, nil
	}
	if challenge.Type != domainmfa.ChallengeTypeTOTPStepUp {
		return "", nil, "", fmt.Errorf("%w: challenge type mismatch", apperrors.ErrInvalidArgument)
	}
	credential, err := s.repository.ActiveCredential(ctx, challenge.UserID, domainmfa.CredentialTypeTOTP)
	if err != nil {
		return "", nil, "", err
	}
	secret, err := secretcrypto.DecryptStringWithKeyring(s.keys, credential.SecretCiphertext)
	if err != nil {
		return "", nil, "", fmt.Errorf("decrypt TOTP credential: %w", err)
	}
	return secret, nil, domainmfa.CredentialTypeTOTP, nil
}

func (s *Service) matchRecoveryCode(ctx context.Context, userID, response string) (string, bool) {
	codes, err := s.repository.ListRecoveryCodes(ctx, userID)
	if err != nil {
		return "", false
	}
	normalized := []byte(normalizeRecoveryCode(response))
	for _, code := range codes {
		if bcrypt.CompareHashAndPassword([]byte(code.CodeHash), normalized) == nil {
			return code.ID, true
		}
	}
	return "", false
}

func (s *Service) requireRecentStepUp(ctx context.Context, userID, sessionID string) error {
	ok, err := s.repository.HasRecentStepUp(ctx, sessionID, userID, s.now().UTC().Add(-recentStepUpWindow))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: recent MFA verification required", apperrors.ErrUnauthorized)
	}
	return nil
}

func challengeResult(now time.Time, method string) sohaapi.MFAChallengeResult {
	return sohaapi.MFAChallengeResult{Verified: true, AuthenticatedAt: now, AuthenticationMethods: []string{"mfa", method}}
}

func newRecoveryCode() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	return encoded[:8] + "-" + encoded[8:], nil
}

func normalizeRecoveryCode(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
}

func (s *Service) record(ctx context.Context, principal domainidentity.Principal, action, result, resource string, metadata map[string]any) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ID: uuid.NewString(), ActorID: principal.UserID, ActorName: principal.UserName,
		Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "identity_mfa",
		ResourceName: resource, Action: action, Result: result, Summary: action,
		Metadata: metadata, CreatedAt: s.now().UTC(),
	})
}
