package networkcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func (s *Service) EndpointPresence(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageEndpointPresenceRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if identity.Kind != "endpoint" {
		return networkprotocol.RuntimeMessage{}, unauthorizedRuntime()
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.EndpointPresenceRequest](message.Payload)
	if err != nil || payload.DeviceID != credential.DeviceID || !slices.Contains(credential.Capabilities, "endpoint-presence-v1") {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	intervals, err := s.runtimeIntervals(ctx)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	interval := min(intervals.HeartbeatIntervalSeconds, 60)
	snapshot, err := s.store.LatestSnapshot(ctx)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	policies, err := s.store.PublishedEndpointResponsePolicies(ctx)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	for _, policy := range policies {
		config := policy.PublishedConfiguration
		if config != nil && (len(config.DeviceIDs) == 0 || slices.Contains(config.DeviceIDs, credential.DeviceID)) {
			interval = min(interval, config.HeartbeatIntervalSeconds)
		}
	}
	now := s.now().UTC()
	uiState, err := normalizeEndpointUIState(&payload, slices.Contains(credential.Capabilities, "endpoint-ui-presence-v1"), now, s.options.MaxClockSkew, interval)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}

	digest := sha256.Sum256(message.Payload)
	policyVersion := payload.PolicyVersion
	if payload.AdmissionPolicyVersion > 0 {
		policyVersion = payload.AdmissionPolicyVersion
	}
	serviceState := sohaapi.NetworkEndpointPresenceServiceStateHealthy
	if payload.Status == "unhealthy" {
		serviceState = sohaapi.NetworkEndpointPresenceServiceStateDegraded
	}
	record, err := s.store.SaveEndpointPresence(ctx, domain.PresenceRecord{
		EndpointPresence: domain.EndpointPresence{DeviceID: credential.DeviceID, RuntimeID: credential.RuntimeID,
			BootID: payload.BootID, Sequence: int(payload.Sequence), ServiceState: serviceState, UIState: uiState,
			UILastObservedAt: payload.UIObservedAt, ConfigurationVersion: payload.ConfigurationVersion, PolicyVersion: policyVersion,
			LastObservedAt: &message.OccurredAt, LastReceivedAt: &now},
		CredentialID: credential.ID, PayloadHash: fmt.Sprintf("sha256:%x", digest), HeartbeatIntervalSeconds: interval,
	})
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageEndpointPresenceResult, networkprotocol.EndpointPresenceResult{
		Accepted: true, PresenceEpoch: record.Epoch, ReceivedAt: *record.LastReceivedAt, NextHeartbeatSeconds: interval, AdmissionPolicyVersion: snapshot.PolicyVersion,
	}, now.Add(s.options.MaxClockSkew))
}

func (s *Service) NASAccounting(ctx context.Context, identity networkidentity.Identity, raw []byte) error {
	if identity.Kind != "nas" {
		return unauthorizedRuntime()
	}
	if err := s.schemas.ValidateRadiusAccounting(raw); err != nil {
		return invalidRuntimeMessage()
	}
	var request networkprotocol.RadiusAccountingRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return invalidRuntimeMessage()
	}
	if _, err := s.authenticateNAS(ctx, identity, request.NASID); err != nil {
		return err
	}
	now := s.now().UTC()
	observedAt := time.Unix(request.EventTimestamp, 0).UTC()
	if observedAt.After(now.Add(s.options.MaxClockSkew)) || now.Sub(observedAt) > 24*time.Hour || time.Duration(request.AccountingDelaySeconds)*time.Second > now.Sub(observedAt)+s.options.MaxClockSkew {
		return invalidRuntimeMessage()
	}
	return s.store.RecordNASAccounting(ctx, identity.ID, request, now, s.options.MaxClockSkew)
}

func normalizeEndpointUIState(payload *networkprotocol.EndpointPresenceRequest, capable bool, now time.Time, skew time.Duration, interval int) (sohaapi.NetworkEndpointPresenceUIState, error) {
	uiState := sohaapi.NetworkEndpointPresenceUIStateUnknown
	if payload.UIState != "" {
		uiState = sohaapi.NetworkEndpointPresenceUIState(payload.UIState)
	}
	if !capable {
		uiState = sohaapi.NetworkEndpointPresenceUIStateUnknown
		payload.UIObservedAt = nil
	}
	if uiState == sohaapi.NetworkEndpointPresenceUIStateOnline {
		if payload.UIObservedAt == nil || payload.UIObservedAt.After(now.Add(skew)) {
			return uiState, invalidRuntimeMessage()
		}
		if now.Sub(*payload.UIObservedAt) > 2*time.Duration(interval)*time.Second {
			uiState = sohaapi.NetworkEndpointPresenceUIStateOffline
		}
	}
	return uiState, nil
}
