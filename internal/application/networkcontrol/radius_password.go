package networkcontrol

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	appaccess "github.com/opensoha/soha/internal/application/networkaccess"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NetworkPasswordVerifier interface {
	Verify(context.Context, string, []byte) (appidentity.NetworkPasswordIdentity, error)
	Current(context.Context, appidentity.NetworkPasswordIdentity) error
}

type PasswordStore interface {
	BeginPasswordAttempt(context.Context, domain.PasswordAttempt) (bool, error)
	FinishPasswordAttempt(context.Context, domain.PasswordAttempt, bool, time.Time) error
	SavePasswordReceipt(context.Context, domain.PasswordReceipt) error
	PasswordReceipt(context.Context, string) (domain.PasswordReceipt, error)
}

func passwordAuthenticationDenied() error {
	return fmt.Errorf("%w: authentication failed", apperrors.ErrUnauthorized)
}

func (s *Service) VerifyNASPassword(ctx context.Context, identity networkidentity.Identity, source string, raw []byte) (networkprotocol.RadiusPasswordResult, error) {
	defer clear(raw)
	if s.options.PasswordVerifier == nil || s.options.PasswordStore == nil {
		return networkprotocol.RadiusPasswordResult{}, apperrors.ErrServiceUnavailable
	}
	request, password, err := s.decodePasswordRequest(raw)
	if err != nil {
		return networkprotocol.RadiusPasswordResult{}, err
	}
	binding, err := s.ttlsRuntimeBinding(ctx, identity, request.NASID, request.RuntimeProof)
	if err != nil {
		clear(password)
		return networkprotocol.RadiusPasswordResult{}, err
	}
	defer clear(password)
	select {
	case s.passwordWorkers <- struct{}{}:
		defer func() { <-s.passwordWorkers }()
	case <-ctx.Done():
		return networkprotocol.RadiusPasswordResult{}, ctx.Err()
	default:
		return networkprotocol.RadiusPasswordResult{}, apperrors.ErrConflict
	}
	attempt := domain.PasswordAttempt{ID: uuid.NewString(), RuntimeID: identity.ID, NASID: binding.NASID, Source: source, SubjectID: request.Login, Now: s.now().UTC()}
	allowed, err := s.options.PasswordStore.BeginPasswordAttempt(ctx, attempt)
	if err != nil {
		return networkprotocol.RadiusPasswordResult{}, err
	}
	if !allowed {
		return networkprotocol.RadiusPasswordResult{}, apperrors.ErrConflict
	}
	verified, verifyErr := s.options.PasswordVerifier.Verify(ctx, request.Login, password)
	// Cancellation still releases/charges our own durable reservation, boundedly.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	err = s.options.PasswordStore.FinishPasswordAttempt(finishCtx, attempt, verifyErr == nil, s.now().UTC())
	cancel()
	if verifyErr != nil {
		return networkprotocol.RadiusPasswordResult{}, verifyErr
	}
	if err != nil {
		return networkprotocol.RadiusPasswordResult{}, err
	}
	if verified.SubjectID != request.Login {
		return networkprotocol.RadiusPasswordResult{}, passwordAuthenticationDenied()
	}
	if err := ctx.Err(); err != nil {
		return networkprotocol.RadiusPasswordResult{}, err
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return networkprotocol.RadiusPasswordResult{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(token))
	clear(secret[:])
	now := s.now().UTC()
	receipt := domain.PasswordReceipt{TokenHash: fmt.Sprintf("%x", digest), RuntimeID: identity.ID, NASID: binding.NASID, SiteID: binding.SiteID, StationID: access.CanonicalStationID(request.StationID), SubjectID: verified.SubjectID, PasswordUpdatedAt: verified.PasswordUpdatedAt, CreatedAt: now, ExpiresAt: now.Add(30 * time.Second)}
	if err := s.options.PasswordStore.SavePasswordReceipt(ctx, receipt); err != nil {
		return networkprotocol.RadiusPasswordResult{}, err
	}
	return networkprotocol.RadiusPasswordResult{SubjectID: verified.SubjectID, AuthenticationReceipt: token}, nil
}

// Password stations have their own inventory identity; a MAC never maps them
// onto an enrolled device or conveys managed posture.
func passwordStationDevice(receipt domain.PasswordReceipt) access.Device {
	digest := sha256.Sum256([]byte(receipt.RuntimeID + "\x00" + receipt.NASID + "\x00" + receipt.StationID + "\x00" + receipt.SubjectID))
	return access.Device{ID: fmt.Sprintf("nas-station:%x", digest), OwnerUserID: receipt.SubjectID, Name: "Password network station", Platform: "unknown", SiteID: receipt.SiteID, Status: access.DeviceStatusActive, ManagementState: access.DeviceManagementUnmanaged, PostureStatus: access.PostureUnknown, DeviceType: access.DeviceTypeUnknown, OwnershipType: access.DeviceOwnershipUnassigned, CredentialGeneration: 1, PostureVersion: 1}
}

func (s *Service) ttlsProof(ctx context.Context, identity networkidentity.Identity, binding access.NASBinding, request networkprotocol.NASAuthorizationRequest) (domain.PasswordReceipt, error) {
	if s.options.PasswordStore == nil || s.options.PasswordVerifier == nil || binding.EAPServerTrust == nil || binding.EAPServerTrust.EapMethod != "ttls" {
		return domain.PasswordReceipt{}, passwordAuthenticationDenied()
	}
	if _, err := s.ttlsRuntimeBinding(ctx, identity, binding.NASID, request.RuntimeProof); err != nil {
		return domain.PasswordReceipt{}, err
	}
	digest := sha256.Sum256([]byte(request.AuthenticationReceipt))
	proof, err := s.options.PasswordStore.PasswordReceipt(ctx, fmt.Sprintf("%x", digest))
	if err != nil {
		return domain.PasswordReceipt{}, err
	}
	if proof.RuntimeID != identity.ID || proof.NASID != binding.NASID || proof.SiteID != binding.SiteID || proof.StationID != access.CanonicalStationID(request.StationID) || proof.SubjectID != request.SubjectID || proof.AuthorizationRequestID != "" && proof.AuthorizationRequestID != request.RequestID || proof.AuthorizationRequestID == "" && !proof.ExpiresAt.After(s.now().UTC()) {
		return domain.PasswordReceipt{}, passwordAuthenticationDenied()
	}
	err = s.options.PasswordVerifier.Current(ctx, appidentity.NetworkPasswordIdentity{SubjectID: proof.SubjectID, PasswordUpdatedAt: proof.PasswordUpdatedAt})
	return proof, err
}

func ttlsSnapshot(snapshot access.PolicySnapshot) access.PolicySnapshot {
	return access.PasswordAdmissionPolicies(snapshot)
}

func (s *Service) decodePasswordRequest(raw []byte) (networkprotocol.RadiusPasswordRequest, []byte, error) {
	var request networkprotocol.RadiusPasswordRequest
	if err := s.schemas.ValidateRadiusPassword(raw); err != nil {
		return request, nil, passwordAuthenticationDenied()
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.EAPMethod != "ttls" || request.InnerMethod != "pap" {
		return request, nil, passwordAuthenticationDenied()
	}
	password, err := base64.StdEncoding.Strict().DecodeString(request.PasswordBase64)
	if err != nil || len(password) == 0 || len(password) > 72 || base64.StdEncoding.EncodeToString(password) != request.PasswordBase64 {
		clear(password)
		return request, nil, passwordAuthenticationDenied()
	}
	return request, password, nil
}
func (s *Service) ttlsRuntimeBinding(ctx context.Context, identity networkidentity.Identity, nasID string, proof *networkprotocol.ManagedNASProof) (access.NASBinding, error) {
	binding, _, err := s.authenticateNASWithProof(ctx, identity, nasID, proof, true)
	if err != nil {
		return binding, passwordAuthenticationDenied()
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil || !slices.Contains(credential.Capabilities, networkprotocol.CapabilityTTLSVerifier) {
		return binding, passwordAuthenticationDenied()
	}
	if binding.EAPServerTrust == nil || binding.EAPServerTrust.EapMethod != "ttls" || appaccess.ValidateEAPServerTrust(binding.EAPServerTrust) != nil {
		return binding, passwordAuthenticationDenied()
	}
	site, err := s.store.NetworkSite(ctx, binding.SiteID)
	if err != nil || site.Status != access.StatusActive {
		return binding, passwordAuthenticationDenied()
	}
	return binding, nil
}

type nasAuthentication struct {
	request    networkprotocol.NASAuthorizationRequest
	credential domain.Credential
	principal  policyPrincipal
	proof      *domain.PasswordReceipt
}

func (s *Service) prepareNASAuthentication(ctx context.Context, identity networkidentity.Identity, binding access.NASBinding, request networkprotocol.NASAuthorizationRequest, snapshot domain.PolicySnapshot) (nasAuthentication, error) {
	result := nasAuthentication{request: request}
	if request.AuthenticationMethod == networkprotocol.NASAuthenticationTTLS {
		proof, err := s.ttlsProof(ctx, identity, binding, request)
		if err != nil {
			return result, err
		}
		result.proof = &proof
		result.request.DeviceID = passwordStationDevice(proof).ID
		result.request.StationID = access.CanonicalStationID(request.StationID)
	} else {
		resolved, credential, err := s.resolveNASDevice(ctx, request)
		if err != nil {
			return result, err
		}
		result.request, result.credential = resolved, credential
	}
	principal, err := s.loadPolicyPrincipal(ctx, result.request.SubjectID, result.request.DeviceID, snapshot)
	if err != nil {
		return result, err
	}
	if result.proof != nil {
		principal.device = passwordStationDevice(*result.proof)
		principal.snapshot = ttlsSnapshot(principal.snapshot)
	}
	result.principal = principal
	return result, nil
}
