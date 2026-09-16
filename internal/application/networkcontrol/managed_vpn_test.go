package networkcontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type managedReplayStore struct {
	*controlStoreStub
	ManagedVPNStore
	intent    domain.VPNIntent
	previous  networkprotocol.VPNManagedConnectResult
	authCalls int
}

func (s *managedReplayStore) GetVPNIntent(context.Context, string) (domain.VPNIntent, error) {
	return s.intent, nil
}
func (s *managedReplayStore) ManagedVPNResult(context.Context, string, string, string, string) (networkprotocol.VPNManagedConnectResult, bool, error) {
	return s.previous, true, nil
}
func (s *managedReplayStore) VPNAuthSessionActive(context.Context, string, string, time.Time) error {
	s.authCalls++
	return apperrors.ErrAccessDenied
}

func TestManagedReplayRechecksRevokedLoginBeforeReturningAllow(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	store := &managedReplayStore{controlStoreStub: &controlStoreStub{credential: runtime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: "endpoint", SubjectID: "user-1", DeviceID: "device-1", WireGuardPublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", Capabilities: []string{"wireguard", networkprotocol.CapabilityManagedVPN}, ExpiresAt: now.Add(time.Hour)}}, intent: domain.VPNIntent{ID: "intent-1", RuntimeID: identity.ID, CredentialID: "credential-1", SubjectID: "user-1", DeviceID: "device-1", AuthSessionID: "revoked-login", Status: "consumed"}, previous: networkprotocol.VPNManagedConnectResult{VPNConnectResult: networkprotocol.VPNConnectResult{Decision: "allow", SessionID: "session-1", ValidUntil: now.Add(time.Minute)}}}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	request := networkprotocol.VPNManagedConnectRequest{RequestID: "request-1", IntentID: "intent-1", IntentToken: strings.Repeat("a", 43)}
	message, err := service.ConnectManagedVPN(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageVPNManagedConnectRequest, request))
	if !errors.Is(err, apperrors.ErrAccessDenied) || store.authCalls != 1 || len(message.Payload) != 0 {
		t.Fatalf("cached allow bypassed login revalidation: %+v %v authCalls=%d", message, err, store.authCalls)
	}
}
