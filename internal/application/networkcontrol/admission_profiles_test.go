package networkcontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func testAdmissionTrust(t *testing.T, now time.Time) *access.EAPServerTrust {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, public, private)
	if err != nil {
		t.Fatal(err)
	}
	return &access.EAPServerTrust{ServerNames: []string{"radius.example.test"}, CaCertificates: []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}, WifiSecurity: sohaapi.NetworkEAPServerTrustWifiSecurityWpa2Enterprise}
}

func TestAdmissionProfilesRequireCapabilityAndCurrentPolicyScope(t *testing.T) {
	now := time.Now().UTC()
	trust := testAdmissionTrust(t, now)
	store := &controlStoreStub{subject: access.Subject{UserID: "user-1", Status: access.StatusActive}, device: access.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "allowed", Status: access.DeviceStatusActive, PostureStatus: access.PostureCompliant},
		admissionSites:    map[string]access.Site{"allowed": {ID: "allowed", Status: access.StatusActive}, "denied": {ID: "denied", Status: access.StatusActive}},
		admissionBindings: []access.NASBinding{{ID: "binding-1", SiteID: "allowed", Status: access.StatusActive, AccessMedium: access.AccessMediumWired, EAPServerTrust: trust}, {ID: "binding-2", SiteID: "denied", Status: access.StatusActive, AccessMedium: access.AccessMediumWiFi, SSID: "Private", EAPServerTrust: trust}}}
	policies, _ := json.Marshal([]access.Policy{{ID: "allow", Enabled: true, Effect: access.PolicyEffectAllow, Subjects: access.PolicySubjects{Users: []string{"user-1"}}, SiteIDs: []string{"allowed"}, Modes: []string{access.ModeInternalDirect}, AccessProfile: access.ProfileFull}})
	snapshot := domain.PolicySnapshot{PolicyVersion: 1, Policies: policies}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	credential := domain.Credential{ID: "credential-1", RuntimeKind: "endpoint", SubjectID: "user-1", DeviceID: "device-1"}
	if result, err := service.endpointAdmissionConfiguration(context.Background(), credential, snapshot); err != nil || result != nil {
		t.Fatalf("unadvertised capability received profiles: %+v %v", result, err)
	}
	credential.Capabilities = []string{"radius-eap-tls-v1", "eap-profile-v1"}
	result, err := service.endpointAdmissionConfiguration(context.Background(), credential, snapshot)
	if err != nil || result == nil || len(result.Profiles) != 1 || result.Profiles[0].ID != "binding-1" || result.Profiles[0].Identity != "user-1" {
		t.Fatalf("policy-scoped profiles: %+v %v", result, err)
	}
	store.subject.Status = access.StatusDisabled
	result, err = service.endpointAdmissionConfiguration(context.Background(), credential, snapshot)
	if err != nil || result == nil || len(result.Profiles) != 0 {
		t.Fatalf("disabled subject received profiles: %+v %v", result, err)
	}
	store.subject.Status = access.StatusActive
	store.admissionBindings = nil
	for n := 0; n < 65; n++ {
		store.admissionBindings = append(store.admissionBindings, access.NASBinding{ID: fmt.Sprintf("binding-%03d", n), SiteID: "allowed", Status: access.StatusActive, AccessMedium: access.AccessMediumWired, EAPServerTrust: trust})
	}
	if _, err := service.endpointAdmissionConfiguration(context.Background(), credential, snapshot); err == nil {
		t.Fatal("truncated profile set silently accepted")
	}
}

func TestConfigurationRetriesStalePolicy(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	identity, _ := testIdentity(t, now)
	trust := testAdmissionTrust(t, now)
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	policies, err := json.Marshal([]access.Policy{{
		ID: "allow", Enabled: true, Effect: access.PolicyEffectAllow, Subjects: access.PolicySubjects{Users: []string{"user-1"}},
		SiteIDs: []string{"site-1"}, Modes: []string{access.ModeInternalDirect}, AccessProfile: access.ProfileFull,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name           string
		publishOnRetry bool
	}{
		{name: "UsesRefreshedPolicyForDesiredAndAdmission"},
		{name: "RejectsWhenPublicationSupersedesRetry", publishOnRetry: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := &configurationPolicyStore{
				controlStoreStub: controlStoreStub{
					snapshot: domain.PolicySnapshot{PolicyVersion: 7, Policies: policies},
					credential: domain.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, SubjectID: "user-1", DeviceID: "device-1",
						ExpiresAt: now.Add(time.Hour), Capabilities: []string{"eap-profile-v1"}},
					subject:        access.Subject{UserID: "user-1", Status: access.StatusActive},
					device:         access.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-1", Status: access.DeviceStatusActive, PostureStatus: access.PostureCompliant},
					admissionSites: map[string]access.Site{"site-1": {ID: "site-1", Status: access.StatusActive}},
					admissionBindings: []access.NASBinding{{
						ID: "binding-1", SiteID: "site-1", Status: access.StatusActive, AccessMedium: access.AccessMediumWired, EAPServerTrust: trust,
					}},
				},
				publishOnRetry: tt.publishOnRetry,
			}
			service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.RefreshSnapshot(ctx); err != nil {
				t.Fatal(err)
			}
			original, err := service.endpointAdmissionConfiguration(ctx, store.credential, store.snapshot)
			if err != nil || original == nil || len(original.Profiles) != 1 {
				t.Fatalf("original policy did not grant admission: %+v, %v", original, err)
			}
			store.snapshot = domain.PolicySnapshot{PolicyVersion: 8, Policies: json.RawMessage(`[]`), ProtectedResourceIDs: []string{"newly-protected"}}

			message, err := service.Configuration(ctx, identity)
			if !slices.Equal(store.versions, []int{7, 8}) {
				t.Fatalf("configuration policy versions = %v, want [7 8]", store.versions)
			}
			if tt.publishOnRetry {
				assertStaleConfigurationRejected(t, message, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := networkprotocol.DecodePayload[networkprotocol.ConfigurationDesired](message.Payload)
			if err != nil || got.PolicyVersion != 8 || !slices.Equal(got.ProtectedResourceIDs, []string{"newly-protected"}) ||
				got.Admission == nil || len(got.Admission.Profiles) != 0 {
				t.Fatalf("refreshed desired configuration = %+v, %v", got, err)
			}
		})
	}
}

func assertStaleConfigurationRejected(t *testing.T, message networkprotocol.RuntimeMessage, err error) {
	t.Helper()
	var stale *apperrors.BusinessError
	if !errors.Is(err, apperrors.ErrConflict) || !errors.As(err, &stale) || stale.Code() != "stale_configuration_policy" || len(message.Payload) != 0 {
		t.Fatalf("superseded retry = payload %s, error %v", message.Payload, err)
	}
}

type configurationPolicyStore struct {
	controlStoreStub
	versions       []int
	publishOnRetry bool
}

func (s *configurationPolicyStore) EnsureConfiguration(_ context.Context, runtimeID string, snapshot domain.PolicySnapshot, _, expiresAt time.Time) (domain.Configuration, error) {
	s.versions = append(s.versions, snapshot.PolicyVersion)
	if s.publishOnRetry && len(s.versions) == 2 {
		s.snapshot.PolicyVersion++
	}
	if snapshot.PolicyVersion != s.snapshot.PolicyVersion {
		return domain.Configuration{}, apperrors.NewBusiness(apperrors.ErrConflict, "stale_configuration_policy", "", "")
	}
	desired := networkprotocol.ConfigurationDesired{
		ConfigurationVersion: 1, PolicyVersion: snapshot.PolicyVersion, ValidUntil: expiresAt, AccessProfile: access.ProfileOnboarding,
		ProtectedResourceIDs: snapshot.ProtectedResourceIDs, NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{},
	}
	return domain.Configuration{
		RuntimeID: runtimeID, ConfigurationVersion: desired.ConfigurationVersion, PolicyVersion: snapshot.PolicyVersion, ValidUntil: expiresAt, Desired: desired,
	}, nil
}

func TestConfigurationFitsEndpointResponseLimit(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{now: func() time.Time { return now }}
	identity := networkidentity.Identity{ID: "endpoint-1", Kind: "endpoint"}
	desired := networkprotocol.ConfigurationDesired{Admission: &networkprotocol.AdmissionConfiguration{Profiles: []networkprotocol.AdmissionProfile{{ID: "binding-1", CACertificates: []string{strings.Repeat("a", 4096)}}}}}
	message, err := service.outbound(identity, networkprotocol.MessageConfiguration, desired, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(message)
	if err != nil || len(encoded) > 1<<20 {
		t.Fatalf("response size = %d, error = %v", len(encoded), err)
	}
	desired.Admission.Profiles[0].CACertificates = []string{strings.Repeat("a", 1<<20)}
	if _, err := service.outbound(identity, networkprotocol.MessageConfiguration, desired, now.Add(time.Minute)); err == nil {
		t.Fatal("configuration larger than endpoint response limit was accepted")
	}
}
