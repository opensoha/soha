package networkcontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestAdmissionProfilesRequireCapabilityAndCurrentPolicyScope(t *testing.T) {
	now := time.Now().UTC()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, public, private)
	if err != nil {
		t.Fatal(err)
	}
	trust := &access.EAPServerTrust{ServerNames: []string{"radius.example.test"}, CaCertificates: []string{string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}, WifiSecurity: sohaapi.NetworkEAPServerTrustWifiSecurityWpa2Enterprise}
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
