package networkcontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestEAPCertificateRoutesRequireDedicatedCapabilityAndIssuer(t *testing.T) {
	now := time.Now().UTC()
	identity := testRuntimeIdentity(t, now, "endpoint", "endpoint-1")
	store := &controlStoreStub{credential: domain.Credential{ID: "control-1", RuntimeID: identity.ID, RuntimeKind: "endpoint", DeviceID: "device-1", SubjectID: "user-1", ExpiresAt: now.Add(time.Hour)}}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: time.Minute, ConfigurationTTL: time.Minute, LeaseTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	request := networkprotocol.EAPCertificateRequest{RequestID: "csr-1", CSRPEM: "-----BEGIN CERTIFICATE REQUEST-----\n" + strings.Repeat("A", 80) + "\n-----END CERTIFICATE REQUEST-----\n"}
	raw := runtimeMessageJSON(t, now, identity, networkprotocol.MessageEAPCertificateRequest, request)
	if _, err := service.IssueEAPCertificate(context.Background(), identity, raw); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("non EAP endpoint requested a certificate: %v", err)
	}
	store.credential.Capabilities = []string{"radius-eap-tls-v1", "eap-profile-v1"}
	if _, err := service.IssueEAPCertificate(context.Background(), identity, raw); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("missing issuer did not fail explicitly: %v", err)
	}
	activate := networkprotocol.EAPCertificateActivateRequest{CredentialID: "eap-1", Fingerprint: "sha256:" + strings.Repeat("a", 64)}
	raw = runtimeMessageJSON(t, now, identity, networkprotocol.MessageEAPCertificateActivateRequest, activate)
	message, err := service.ActivateEAPCertificate(context.Background(), identity, raw)
	if err != nil || message.MessageType != networkprotocol.MessageEAPCertificateActivateResult {
		t.Fatalf("bound activation route: %+v %v", message, err)
	}
	store.credential.Capabilities = nil
	if _, err := service.ActivateEAPCertificate(context.Background(), identity, raw); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("ordinary control endpoint activated EAP: %v", err)
	}
}
