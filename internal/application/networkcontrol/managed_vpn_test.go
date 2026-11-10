package networkcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	intent         domain.VPNIntent
	previous       networkprotocol.VPNManagedConnectResult
	authCalls      int
	authErr        error
	vpnProfile     domain.VPNProfile
	vpnPolicy      domain.VPNSelectionPolicy
	configVersions []int
}

func (s *managedReplayStore) GetVPNIntent(context.Context, string) (domain.VPNIntent, error) {
	return s.intent, nil
}
func (s *managedReplayStore) ManagedVPNResult(context.Context, string, string, string, string) (networkprotocol.VPNManagedConnectResult, bool, error) {
	return s.previous, true, nil
}
func (s *managedReplayStore) VPNAuthSessionActive(context.Context, string, string, time.Time) error {
	s.authCalls++
	return s.authErr
}
func (s *managedReplayStore) GetVPNProfile(context.Context, string) (domain.VPNProfile, error) {
	return s.vpnProfile, nil
}
func (s *managedReplayStore) GetVPNSelectionPolicy(context.Context, string) (domain.VPNSelectionPolicy, error) {
	return s.vpnPolicy, nil
}
func (s *managedReplayStore) EnsureConfiguration(_ context.Context, _ string, snapshot runtime.PolicySnapshot, _, _ time.Time) (runtime.Configuration, error) {
	s.configVersions = append(s.configVersions, snapshot.PolicyVersion)
	if snapshot.PolicyVersion != s.snapshot.PolicyVersion {
		return runtime.Configuration{}, apperrors.NewBusiness(apperrors.ErrConflict, "stale_configuration_policy", "", "")
	}
	return s.config, nil
}

func TestManagedReplayRechecksRevokedLoginBeforeReturningAllow(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	store := &managedReplayStore{controlStoreStub: &controlStoreStub{credential: runtime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: "endpoint", SubjectID: "user-1", DeviceID: "device-1", WireGuardPublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", Capabilities: []string{"wireguard", networkprotocol.CapabilityManagedVPN}, ExpiresAt: now.Add(time.Hour)}}, intent: domain.VPNIntent{ID: "intent-1", RuntimeID: identity.ID, CredentialID: "credential-1", SubjectID: "user-1", DeviceID: "device-1", AuthSessionID: "revoked-login", Status: "consumed"}, previous: networkprotocol.VPNManagedConnectResult{VPNConnectResult: networkprotocol.VPNConnectResult{Decision: "allow", SessionID: "session-1", ValidUntil: now.Add(time.Minute)}}} // #nosec G101 -- Synthetic test-only credential or rejection fixture; no real credential.
	store.authErr = apperrors.ErrAccessDenied
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

func TestManagedReplayRevalidatesWithRefreshedPolicy(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	identity, _ := testIdentity(t, now)
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	policies, err := json.Marshal([]domain.Policy{{
		ID: "allow", Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull,
		SiteIDs: []string{"site-1"}, Modes: []string{domain.ModeExternalVPN},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name         string
		policies     json.RawMessage
		wantVersions []int
		wantErr      error
	}{
		{name: "StillAuthorized", policies: policies, wantVersions: []int{7, 8}},
		{name: "PolicyRevoked", policies: json.RawMessage(`[]`), wantVersions: []int{7}, wantErr: apperrors.ErrAccessDenied},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &managedReplayStore{
				controlStoreStub: &controlStoreStub{
					credential: runtime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, SubjectID: "user-1", DeviceID: "device-1",
						WireGuardPublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", Capabilities: []string{"wireguard", networkprotocol.CapabilityManagedVPN}, ExpiresAt: now.Add(time.Hour)},
					snapshot: runtime.PolicySnapshot{PolicyVersion: 7, Policies: policies},
					subject:  domain.Subject{UserID: "user-1", Status: domain.StatusActive},
					device:   domain.Device{ID: "device-1", OwnerUserID: "user-1", Status: domain.DeviceStatusActive, PostureStatus: domain.PostureCompliant},
					site:     domain.Site{ID: "site-1", Status: domain.StatusActive},
					space:    domain.Space{ID: "space-1", SiteID: "site-1", Status: domain.StatusActive, CIDRs: []string{"10.77.0.0/24"}},
					config: runtime.Configuration{PolicyVersion: 8, Desired: networkprotocol.ConfigurationDesired{
						PolicyVersion: 8, NetworkLeases: []networkprotocol.NetworkLease{{SessionID: "session-1"}},
					}},
				},
				// #nosec G101 -- Synthetic credential identifier binds the in-memory replay fixture.
				intent: domain.VPNIntent{ID: "intent-1", RuntimeID: identity.ID, CredentialID: "credential-1", SubjectID: "user-1", DeviceID: "device-1",
					AuthSessionID: "login-1", Status: "consumed", ProfileID: "profile-1", ProfileRevision: 1, SelectionPolicyID: "policy-1", SelectionPolicyRevision: 1},
				previous: networkprotocol.VPNManagedConnectResult{VPNConnectResult: networkprotocol.VPNConnectResult{Decision: domain.DecisionAllow, SessionID: "session-1", ValidUntil: now.Add(time.Minute)}},
				vpnProfile: domain.VPNProfile{ID: "profile-1", PublishedRevision: 1, PublishedConfiguration: &domain.VPNProfileConfig{
					SiteID: "site-1", NetworkSpaceID: "space-1", Mode: domain.ModeExternalVPN, SelectionPolicyID: "policy-1", Enabled: true,
					Assignments: domain.VPNAssignment{UserIDs: []string{"user-1"}},
				}},
				vpnPolicy: domain.VPNSelectionPolicy{ID: "policy-1", PublishedRevision: 1, PublishedConfiguration: &domain.VPNSelectionPolicyConfig{}},
			}
			service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			if err := service.RefreshSnapshot(ctx); err != nil {
				t.Fatal(err)
			}
			store.snapshot = runtime.PolicySnapshot{PolicyVersion: 8, Policies: tt.policies}
			request := networkprotocol.VPNManagedConnectRequest{RequestID: "request-1", IntentID: "intent-1", IntentToken: strings.Repeat("a", 43)}
			message, err := service.ConnectManagedVPN(ctx, identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageVPNManagedConnectRequest, request))
			if store.authCalls != 2 || !slices.Equal(store.configVersions, tt.wantVersions) || !errors.Is(err, tt.wantErr) {
				t.Fatalf("replay = %v, auth checks = %d, policy versions = %v, want %v and %v", err, store.authCalls, store.configVersions, tt.wantErr, tt.wantVersions)
			}
			if tt.wantErr != nil {
				if len(message.Payload) != 0 {
					t.Fatal("revoked replay returned a payload")
				}
				return
			}
			result, err := networkprotocol.DecodePayload[networkprotocol.VPNManagedConnectResult](message.Payload)
			if err != nil || result.Decision != domain.DecisionAllow || result.SessionID != store.previous.SessionID {
				t.Fatalf("replay result = %+v, %v", result, err)
			}
		})
	}
}
