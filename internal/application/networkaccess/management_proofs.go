package networkaccess

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"

	access "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type managementProofStore interface {
	SaveManagementProof(context.Context, domain.ManagementProofClaims, string, time.Time) error
}

func (s *Service) SubmitManagementProof(ctx context.Context, p identity.Principal, id string, input domain.ManagementProofInput) (domain.Device, error) {
	if err := s.authorize(ctx, p, access.PermNetworkAccessEndpointDevicesUpdate); err != nil {
		return domain.Device{}, err
	}
	device, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return domain.Device{}, err
	}
	if err := s.authorizeScope(ctx, p, access.PermNetworkAccessEndpointDevicesUpdate, deviceScope(device)); err != nil {
		return domain.Device{}, err
	}
	claims, err := decodeManagementProof(input, time.Now().UTC())
	if err != nil {
		return domain.Device{}, err
	}
	if claims.DeviceID != device.ID || claims.SiteID != device.SiteID {
		return domain.Device{}, apperrors.ErrAccessDenied
	}
	site, err := s.store.GetSite(ctx, claims.SiteID)
	if err != nil {
		return domain.Device{}, err
	}
	key, err := base64.StdEncoding.DecodeString(site.ManagementIssuerKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return domain.Device{}, invalid("site management issuer is not configured")
	}
	signature, err := base64.RawURLEncoding.DecodeString(input.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), []byte(input.Payload), signature) {
		return domain.Device{}, apperrors.ErrAccessDenied
	}
	store, ok := s.store.(managementProofStore)
	if !ok {
		return domain.Device{}, apperrors.ErrServiceUnavailable
	}
	if err := store.SaveManagementProof(ctx, claims, site.ManagementIssuerKey, time.Now().UTC()); err != nil {
		return domain.Device{}, err
	}
	s.recordMutation(ctx, p, "network_access.management_proof.accepted", "EndpointDevice", device.ID, device.Name)
	return s.store.GetDevice(ctx, id)
}

func decodeManagementProof(input domain.ManagementProofInput, now time.Time) (domain.ManagementProofClaims, error) {
	var claims domain.ManagementProofClaims
	if len(input.Payload) == 0 || len(input.Payload) > 8192 || len(input.Signature) != 86 {
		return claims, invalid("invalid management proof")
	}
	raw, err := base64.RawURLEncoding.DecodeString(input.Payload)
	if err != nil {
		return claims, invalid("invalid management proof payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&claims) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return claims, invalid("invalid management proof claims")
	}
	if claims.Audience != "soha-network-management/v1" || claims.Sequence < 1 || claims.CredentialGeneration < 1 || !runtimeIdentifierPattern.MatchString(claims.DeviceID) || !runtimeIdentifierPattern.MatchString(claims.SiteID) || !oneOf(claims.ManagementState, domain.DeviceManagementManaged, domain.DeviceManagementUnmanaged) {
		return claims, invalid("invalid management proof binding")
	}
	if claims.ObservedAt.Before(now.Add(-time.Minute)) || claims.ObservedAt.After(now.Add(10*time.Second)) || !claims.ExpiresAt.After(now) || claims.ExpiresAt.After(claims.ObservedAt.Add(5*time.Minute)) {
		return claims, invalid("management proof is stale or exceeds five minutes")
	}
	return claims, nil
}
