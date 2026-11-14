package networkcontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appidentity "github.com/opensoha/soha/internal/application/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type passwordVerifierStub struct {
	version time.Time
	denied  bool
	calls   int
}

func (s *passwordVerifierStub) Verify(ctx context.Context, subject string, password []byte) (appidentity.NetworkPasswordIdentity, error) {
	s.calls++
	if ctx.Err() != nil {
		return appidentity.NetworkPasswordIdentity{}, ctx.Err()
	}
	if s.denied || string(password) != " a 密碼 " {
		return appidentity.NetworkPasswordIdentity{}, apperrors.ErrUnauthorized
	}
	return appidentity.NetworkPasswordIdentity{SubjectID: subject, PasswordUpdatedAt: s.version}, nil
}
func (s *passwordVerifierStub) Current(ctx context.Context, identity appidentity.NetworkPasswordIdentity) error {
	if s.denied || !identity.PasswordUpdatedAt.Equal(s.version) {
		return apperrors.ErrUnauthorized
	}
	return ctx.Err()
}

type passwordStoreStub struct {
	allowed  bool
	proof    domain.PasswordReceipt
	finished int
}

func (s *passwordStoreStub) BeginPasswordAttempt(context.Context, domain.PasswordAttempt) (bool, error) {
	return s.allowed, nil
}
func (s *passwordStoreStub) FinishPasswordAttempt(context.Context, domain.PasswordAttempt, bool, time.Time) error {
	s.finished++
	return nil
}
func (s *passwordStoreStub) SavePasswordReceipt(_ context.Context, p domain.PasswordReceipt) error {
	s.proof = p
	return nil
}
func (s *passwordStoreStub) PasswordReceipt(_ context.Context, hash string) (domain.PasswordReceipt, error) {
	if hash != s.proof.TokenHash {
		return domain.PasswordReceipt{}, apperrors.ErrUnauthorized
	}
	return s.proof, nil
}

func TestTTLSVerifierOriginalPasswordAndBoundProof(t *testing.T) {
	now := time.Now().UTC()
	identity := testRuntimeIdentity(t, now, "nas", "radius-1")
	trust := testAdmissionTrust(t, now)
	trust.EapMethod = "ttls"
	trust.InnerMethod = "pap"
	trust.CredentialSource = "user-input"
	store := &controlStoreStub{credential: domain.Credential{ID: "runtime-cred", RuntimeID: identity.ID, RuntimeKind: "nas", ExpiresAt: now.Add(time.Hour), Capabilities: []string{networkprotocol.CapabilityTTLSVerifier}}, nasBinding: access.NASBinding{NASID: "nas-1", RuntimeID: identity.ID, SiteID: "site-1", Status: access.StatusActive, EAPServerTrust: trust}, site: access.Site{ID: "site-1", Status: access.StatusActive}}
	verifier := &passwordVerifierStub{version: now.Add(-time.Hour)}
	proofs := &passwordStoreStub{allowed: true}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute, PasswordVerifier: verifier, PasswordStore: proofs})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	request := networkprotocol.RadiusPasswordRequest{RequestID: "auth-1", NASID: "nas-1", StationID: "02:00:00:00:00:01", Login: "user-1", PasswordBase64: base64.StdEncoding.EncodeToString([]byte(" a 密碼 ")), EAPMethod: "ttls", InnerMethod: "pap"}
	raw, _ := json.Marshal(request)
	result, err := service.VerifyNASPassword(context.Background(), identity, "192.0.2.2", raw)
	if err != nil || len(result.AuthenticationReceipt) != 43 || proofs.finished != 1 || proofs.proof.SubjectID != "user-1" || proofs.proof.ExpiresAt.Sub(now) != 30*time.Second || strings.Contains(proofs.proof.TokenHash, result.AuthenticationReceipt) {
		t.Fatalf("proof: %+v %v", proofs.proof, err)
	}
	for _, scenario := range []string{"valid", "wrong-nas", "wrong-station", "wrong-subject", "wrong-request", "expired", "changed-password"} {
		t.Run(scenario, func(t *testing.T) {
			auth := networkprotocol.NASAuthorizationRequest{RequestID: "authorize-1", NASID: "nas-1", SubjectID: "user-1", StationID: request.StationID, AuthenticationMethod: networkprotocol.NASAuthenticationTTLS, AuthenticationReceipt: result.AuthenticationReceipt}
			binding := store.nasBinding
			original := proofs.proof
			defer func() { proofs.proof = original; verifier.denied = false }()
			switch scenario {
			case "wrong-nas":
				binding.NASID = "other"
			case "wrong-station":
				auth.StationID = "02:00:00:00:00:02"
			case "wrong-subject":
				auth.SubjectID = "other"
			case "wrong-request":
				proofs.proof.AuthorizationRequestID = "other"
			case "expired":
				proofs.proof.ExpiresAt = now
			case "changed-password":
				verifier.denied = true
			}
			_, err := service.ttlsProof(context.Background(), identity, binding, auth)
			if scenario == "valid" && err != nil {
				t.Fatal(err)
			}
			if scenario != "valid" && err == nil {
				t.Fatal("invalid proof accepted")
			}
		})
	}
	assertTTLSInputRejections(t, service, identity, request, proofs)
}

func assertTTLSInputRejections(t *testing.T, service *Service, identity networkidentity.Identity, request networkprotocol.RadiusPasswordRequest, proofs *passwordStoreStub) {
	t.Helper()
	request.PasswordBase64 = "Zh=="
	raw, _ := json.Marshal(request)
	if _, err := service.VerifyNASPassword(context.Background(), identity, "192.0.2.2", raw); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("noncanonical Base64: %v", err)
	}
	request.PasswordBase64 = base64.StdEncoding.EncodeToString([]byte(" a 密碼 "))
	proofs.allowed = false
	raw, _ = json.Marshal(request)
	if _, err := service.VerifyNASPassword(context.Background(), identity, "192.0.2.2", raw); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("budget rejection: %v", err)
	}
	device := passwordStationDevice(proofs.proof)
	other := proofs.proof
	other.NASID = "other"
	if device.ID == passwordStationDevice(other).ID || device.ManagementState != access.DeviceManagementUnmanaged || device.PostureStatus != access.PostureUnknown {
		t.Fatal("station inherited device trust")
	}
}
