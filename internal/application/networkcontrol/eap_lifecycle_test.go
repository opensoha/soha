package networkcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestLifecycleActivationRequiresCurrentInstalledProfile(t *testing.T) {
	now := time.Now().UTC()
	identity := testRuntimeIdentity(t, now, "endpoint", "endpoint-1")
	policies, _ := json.Marshal([]access.Policy{{ID: "allow", Enabled: true, Effect: access.PolicyEffectAllow, Subjects: access.PolicySubjects{Users: []string{"user-1"}}, SiteIDs: []string{"site-1"}, Modes: []string{access.ModeInternalDirect}, AccessProfile: access.ProfileRestricted}})
	store := &controlStoreStub{credential: domain.Credential{ID: "parent-1", RuntimeID: identity.ID, RuntimeKind: "endpoint", DeviceID: "device-1", SubjectID: "user-1", ExpiresAt: now.Add(time.Hour), Capabilities: []string{"eap-profile-v1", "radius-eap-tls-v1", networkprotocol.CapabilityEAPLifecycle}}, snapshot: domain.PolicySnapshot{PolicyVersion: 1, Policies: policies}, subject: access.Subject{UserID: "user-1", Status: access.StatusActive}, device: access.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-1", Status: access.DeviceStatusActive}, admissionSites: map[string]access.Site{"site-1": {ID: "site-1", Status: access.StatusActive}}, admissionBindings: []access.NASBinding{{ID: "profile-1", SiteID: "site-1", Status: access.StatusActive, AccessMedium: access.AccessMediumWired, EAPServerTrust: testAdmissionTrust(t, now)}}}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	if err := service.RefreshSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	profiles, err := service.endpointAdmissionConfiguration(context.Background(), store.credential, store.snapshot)
	if err != nil || len(profiles.Profiles) != 1 {
		t.Fatalf("profiles=%+v err=%v", profiles, err)
	}
	for _, scenario := range []string{"current", "stale", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			request := networkprotocol.EAPCertificateActivateRequest{RequestID: "issue-1", ProfileID: "profile-1", ProfileRevision: profiles.Profiles[0].ProfileRevision, CredentialID: "cert-1", Fingerprint: "sha256:" + strings.Repeat("a", 64)}
			if scenario == "stale" {
				request.ProfileRevision = "sha256:" + strings.Repeat("b", 64)
			}
			if scenario == "missing" {
				request.RequestID = ""
				request.ProfileID = ""
				request.ProfileRevision = ""
			}
			_, err := service.ActivateEAPCertificate(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageEAPCertificateActivateRequest, request))
			if scenario == "current" && err != nil {
				t.Fatal(err)
			}
			if scenario != "current" && !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
