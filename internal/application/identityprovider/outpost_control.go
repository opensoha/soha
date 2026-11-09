package identityprovider

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainruntime "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// OutpostRuntimeControl owns durable instance fencing and bounded control grants.
// Identity authentication and configuration leases remain owned by this service.
type OutpostRuntimeControl interface {
	ControlForInstance(context.Context, string, string, string, domainruntime.InstanceCredential) (domainruntime.InstanceControl, error)
	RecordInstanceObservation(context.Context, domainruntime.InstanceObservation) error
	AuthorizeAccess(context.Context, string) error
}

func (s *Service) SetOutpostRuntimeControl(control OutpostRuntimeControl) {
	s.outpostRuntimeControl = control
}

func validateOutpostControlClaim(request sohaapi.IdentityOutpostClaimRequest) error {
	if request.SupportedRuntimeControlVersion == "" && request.InstanceID == "" && request.ProcessEpoch == "" {
		return nil
	}
	if request.SupportedRuntimeControlVersion != "v1" || !validOutpostInstanceIdentity(request.InstanceID, request.ProcessEpoch) {
		return fmt.Errorf("%w: complete v1 Outpost runtime control identity is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validOutpostInstanceIdentity(instanceID, epoch string) bool {
	return strings.TrimSpace(instanceID) != "" && instanceID == strings.TrimSpace(instanceID) && len(instanceID) <= 128 &&
		strings.TrimSpace(epoch) != "" && epoch == strings.TrimSpace(epoch) && len(epoch) <= 128
}

func (s *Service) attachOutpostRuntimeControl(ctx context.Context, config *sohaapi.IdentityOutpostRuntimeConfig, request sohaapi.IdentityOutpostClaimRequest, tokenHash string) error {
	if request.SupportedRuntimeControlVersion == "" {
		return nil
	}
	if s.outpostRuntimeControl == nil {
		return fmt.Errorf("%w: Outpost runtime control is unavailable", apperrors.ErrUnsupportedOperation)
	}
	control, err := s.outpostRuntimeControl.ControlForInstance(ctx, config.OutpostID, request.InstanceID, request.ProcessEpoch, domainruntime.InstanceCredential{TokenHash: tokenHash, ConfigurationVersion: config.ConfigurationVersion, ExpiresAt: config.ExpiresAt})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if control.ProtocolVersion != "v1" || control.Generation < 0 || (control.Action != "serve" && control.Action != "drain") ||
		control.IssuedAt.IsZero() || control.IssuedAt.After(now) || !control.ExpiresAt.After(now) ||
		!control.ExpiresAt.After(control.IssuedAt) || control.ExpiresAt.Sub(control.IssuedAt) > domainruntime.ControlLeaseTTL || control.ExpiresAt.After(config.ExpiresAt) {
		return fmt.Errorf("%w: invalid Outpost runtime control grant", apperrors.ErrConflict)
	}
	signed := &sohaapi.IdentityOutpostRuntimeControl{
		OutpostID: config.OutpostID, ProtocolVersion: "v1", InstanceID: request.InstanceID, ProcessEpoch: request.ProcessEpoch,
		Generation: control.Generation, Action: sohaapi.IdentityOutpostRuntimeControlAction(control.Action),
		IssuedAt: control.IssuedAt, ExpiresAt: control.ExpiresAt, KeyID: s.outpostSigningKeyID,
	}
	payload, err := outpostControlPayload(*signed)
	if err != nil {
		return err
	}
	signed.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.outpostSigningKey, payload))
	config.RuntimeControl = signed
	return nil
}

// Preserve the protocol's field order; generated DTO fields are alphabetic.
func outpostControlPayload(control sohaapi.IdentityOutpostRuntimeControl) ([]byte, error) {
	return json.Marshal(struct {
		OutpostID       string                                               `json:"outpostId"`
		ProtocolVersion sohaapi.IdentityOutpostRuntimeControlProtocolVersion `json:"protocolVersion"`
		InstanceID      string                                               `json:"instanceId"`
		ProcessEpoch    string                                               `json:"processEpoch"`
		Generation      int64                                                `json:"generation"`
		Action          sohaapi.IdentityOutpostRuntimeControlAction          `json:"action"`
		IssuedAt        time.Time                                            `json:"issuedAt"`
		ExpiresAt       time.Time                                            `json:"expiresAt"`
		KeyID           string                                               `json:"keyId"`
	}{control.OutpostID, control.ProtocolVersion, control.InstanceID, control.ProcessEpoch, control.Generation, control.Action, control.IssuedAt, control.ExpiresAt, control.KeyID})
}

func validateOutpostControlHeartbeat(request sohaapi.IdentityOutpostHeartbeatRequest) error {
	if !hasOutpostControlObservation(request) {
		return nil
	}
	if !validOutpostInstanceIdentity(request.InstanceID, request.ProcessEpoch) || request.AcceptingRequests == nil || request.ActiveChecks == nil || request.AppliedControlGeneration == nil {
		return fmt.Errorf("%w: complete Outpost instance observation is required", apperrors.ErrInvalidArgument)
	}
	if *request.ActiveChecks < 0 || *request.AppliedControlGeneration < 0 || request.ConfigurationVersion <= 0 || request.ConfigurationExpiresAt == nil {
		return fmt.Errorf("%w: invalid Outpost instance observation", apperrors.ErrInvalidArgument)
	}
	return nil
}

func hasOutpostControlObservation(request sohaapi.IdentityOutpostHeartbeatRequest) bool {
	return request.InstanceID != "" || request.ProcessEpoch != "" || request.AcceptingRequests != nil || request.ActiveChecks != nil || request.AppliedControlGeneration != nil
}

func (s *Service) recordOutpostControlObservation(ctx context.Context, outpost domainprovider.Outpost, request sohaapi.IdentityOutpostHeartbeatRequest) error {
	if !hasOutpostControlObservation(request) {
		return nil
	}
	if s.outpostRuntimeControl == nil {
		return fmt.Errorf("%w: Outpost runtime control is unavailable", apperrors.ErrUnsupportedOperation)
	}
	// Revalidate the current credential-bound configuration grant immediately before
	// the ACK. The runtime owner additionally fences the registered epoch/grant.
	repository, err := s.outpostLeaseRepository()
	if err != nil {
		return err
	}
	if err := repository.ValidateOutpostRuntimeLease(ctx, outpost.ID, outpost.TokenHash, request.ConfigurationVersion, request.ConfigurationExpiresAt); err != nil {
		return err
	}
	return s.outpostRuntimeControl.RecordInstanceObservation(ctx, domainruntime.InstanceObservation{
		CredentialHash: outpost.TokenHash, ConfigurationVersion: request.ConfigurationVersion,
		OutpostID: outpost.ID, InstanceID: request.InstanceID, ProcessEpoch: request.ProcessEpoch, Generation: *request.AppliedControlGeneration,
		AcceptingRequests: *request.AcceptingRequests, ActiveChecks: int64(*request.ActiveChecks), ObservedAt: time.Now().UTC(), LeaseExpiresAt: *request.ConfigurationExpiresAt,
	})
}

func (s *Service) authorizeOutpostRuntimeAccess(ctx context.Context, outpostID string) error {
	if s.outpostRuntimeControl == nil {
		return nil
	} // Existing externally controlled deployments have no lifecycle port.
	return s.outpostRuntimeControl.AuthorizeAccess(ctx, outpostID)
}
