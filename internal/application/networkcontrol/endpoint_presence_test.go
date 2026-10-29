package networkcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestServiceEndpointPresenceRequiresBoundProtectedService(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	service, store, identity := endpointPresenceFixture(t, now)
	payload := networkprotocol.EndpointPresenceRequest{DeviceID: "device-1", BootID: "boot-1", Sequence: 1, Status: "healthy", UIState: "offline", ConfigurationVersion: 0, PolicyVersion: 0}
	result, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := networkprotocol.DecodePayload[networkprotocol.EndpointPresenceResult](result.Payload)
	if err != nil || !accepted.Accepted || accepted.AdmissionPolicyVersion != 7 || accepted.PresenceEpoch != 1 || store.presenceRecord.UIState != "offline" || store.presenceRecord.ServiceState != "healthy" {
		t.Fatalf("presence result = %#v, record = %#v, error = %v", accepted, store.presenceRecord, err)
	}
	payload.AdmissionPolicyVersion, payload.PolicyVersion, payload.Status = 7, 2, "unhealthy"
	if _, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload)); err != nil || store.presenceRecord.PolicyVersion != 7 || store.presenceRecord.ServiceState != "degraded" {
		t.Fatalf("independent admission version: record=%#v error=%v", store.presenceRecord, err)
	}
	payload.Status = "healthy"
	payload.DeviceID = "someone-else"
	if _, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload)); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("cross-device presence error = %v", err)
	}
	payload.DeviceID, payload.UIState = "device-1", "online"
	if _, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload)); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unobserved UI error = %v", err)
	}
	oldUI := now.Add(-3 * time.Minute)
	payload.UIObservedAt = &oldUI
	if _, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload)); err != nil {
		t.Fatal(err)
	}
	if store.presenceRecord.UIState != "offline" {
		t.Fatal("old UI observation was treated as online")
	}
	store.credential.Capabilities = []string{"endpoint-presence-v1"}
	payload.UIObservedAt = &now
	if _, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload)); err != nil {
		t.Fatal(err)
	}
	if store.presenceRecord.UIState != "unknown" || store.presenceRecord.UILastObservedAt != nil {
		t.Fatal("a service without UI attestation promoted UI presence")
	}
	store.credential.Capabilities = nil
	if _, err := service.EndpointPresence(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEndpointPresenceRequest, payload)); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("missing capability error = %v", err)
	}
}

func endpointPresenceFixture(t *testing.T, now time.Time) (*Service, *controlStoreStub, networkidentity.Identity) {
	t.Helper()
	identity, _ := testIdentity(t, now)
	store := &controlStoreStub{credential: domain.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: "endpoint", DeviceID: "device-1", ExpiresAt: now.Add(time.Hour), Capabilities: []string{"endpoint-presence-v1", "endpoint-ui-presence-v1"}}}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	store.snapshot.PolicyVersion = 7
	return service, store, identity
}

func TestNASAccountingRequiresNASIdentityBindingAndEventTime(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	identity := testRuntimeIdentity(t, now, "nas", "radius-1")
	store := &controlStoreStub{credential: domain.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: "nas", ExpiresAt: now.Add(time.Hour)}}
	store.nasBinding.RuntimeID, store.nasBinding.NASID, store.nasBinding.Status = identity.ID, "nas-1", "active"
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	request := networkprotocol.RadiusAccountingRequest{SchemaVersion: networkprotocol.RadiusAccountingSchemaVersion, EventTimestamp: now.Unix(), RadiusAccounting: networkprotocol.RadiusAccounting{StatusType: "start", NASID: "nas-1", AccountingSessionID: "acct-1", SessionID: "core-session-1", UserID: "user-1", DeviceID: "aa:bb:cc:dd:ee:ff"}}
	raw, _ := json.Marshal(request)
	if err := service.NASAccounting(context.Background(), identity, raw); err != nil {
		t.Fatal(err)
	}
	if store.accountingCalls != 1 || store.accountingRuntime != identity.ID || store.accountingRequest.SessionID != request.SessionID {
		t.Fatal("trusted accounting did not retain its session binding")
	}
	request.NASID = "other-nas"
	raw, _ = json.Marshal(request)
	if err := service.NASAccounting(context.Background(), identity, raw); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("cross-NAS accounting error = %v", err)
	}
	request.NASID, request.EventTimestamp = "nas-1", now.Add(2*time.Minute).Unix()
	raw, _ = json.Marshal(request)
	if err := service.NASAccounting(context.Background(), identity, raw); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("future accounting error = %v", err)
	}
	identity.Kind = "endpoint"
	if err := service.NASAccounting(context.Background(), identity, raw); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("endpoint-supplied NAS accounting error = %v", err)
	}
}
