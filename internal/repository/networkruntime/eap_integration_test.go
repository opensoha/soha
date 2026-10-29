package networkruntime_test

import (
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *runtimeRepositoryScenario) checkEAPCertificates(t *testing.T) {
	t.Run("DedicatedEAPActivationRotationAndParentBinding", func(t *testing.T) {
		now := s.secondNow.Add(time.Second)
		parent := s.credential2
		prepare := func(id string) domain.EAPCredential {
			value := domain.EAPCredential{EAPCertificateResult: networkprotocol.EAPCertificateResult{CredentialID: id + "-" + s.suffix, CertificatePEM: "certificate-" + id, CACertificatePEM: "ca", Fingerprint: digest(id), ExpiresAt: now.Add(10 * time.Minute), Status: "pending"},
				ParentCredentialID: parent.ID, RuntimeID: parent.RuntimeID, DeviceID: parent.DeviceID, SubjectID: parent.SubjectID,
				RequestID: id, CSRHash: digest("csr-" + id), CertificateSerial: id + "-" + s.suffix, CertificateAuthorityKeyID: "eap-issuer", NotBefore: now, CreatedAt: now}
			saved, err := s.repository.SaveEAPCertificate(s.ctx, value, now)
			if err != nil {
				t.Fatal(err)
			}
			return saved
		}
		activate := func(value domain.EAPCredential) error {
			_, err := s.repository.ActivateEAPCertificate(s.ctx, parent, networkprotocol.EAPCertificateActivateRequest{CredentialID: value.CredentialID, Fingerprint: value.Fingerprint}, now)
			return err
		}
		first := prepare("eap-first")
		if _, err := s.repository.ActiveEndpointCredential(s.ctx, first.CertificateSerial, first.CertificateAuthorityKeyID, now); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("pending EAP authenticated: %v", err)
		}
		replay, err := s.repository.SaveEAPCertificate(s.ctx, first, now)
		if err != nil || replay.CredentialID != first.CredentialID || replay.Generation != first.Generation {
			t.Fatalf("issuance replay: %+v %v", replay, err)
		}
		conflict := first
		conflict.CSRHash = digest("changed-csr")
		if _, err := s.repository.SaveEAPCertificate(s.ctx, conflict, now); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("changed CSR replay accepted: %v", err)
		}
		if err := activate(first); err != nil {
			t.Fatal(err)
		}
		if active, err := s.repository.ActiveEndpointCredential(s.ctx, first.CertificateSerial, first.CertificateAuthorityKeyID, now); err != nil || active.ID != first.CredentialID || active.DeviceID != parent.DeviceID {
			t.Fatalf("active EAP lookup: %+v %v", active, err)
		}
		second := prepare("eap-second")
		if _, err := s.repository.ActiveEndpointCredential(s.ctx, first.CertificateSerial, first.CertificateAuthorityKeyID, now); err != nil {
			t.Fatalf("pending rotation cut existing certificate: %v", err)
		}
		wrong := networkprotocol.EAPCertificateActivateRequest{CredentialID: second.CredentialID, Fingerprint: digest("wrong")}
		if _, err := s.repository.ActivateEAPCertificate(s.ctx, parent, wrong, now); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("wrong fingerprint activated: %v", err)
		}
		if err := activate(second); err != nil {
			t.Fatal(err)
		}
		if err := activate(second); err != nil {
			t.Fatalf("activation not idempotent: %v", err)
		}
		if _, err := s.repository.ActiveEndpointCredential(s.ctx, first.CertificateSerial, first.CertificateAuthorityKeyID, now); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("rotated EAP authenticated: %v", err)
		}
		if err := activate(first); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("old EAP activation replay: %v", err)
		}
		if _, err := s.repository.ActiveEndpointCredential(s.ctx, second.CertificateSerial, second.CertificateAuthorityKeyID, now.Add(11*time.Minute)); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("expired EAP authenticated: %v", err)
		}
		s.checkEAPParentRevocation(t, now, parent, second)
	})
}

func (s *runtimeRepositoryScenario) checkEAPParentRevocation(t *testing.T, now time.Time, parent domain.Credential, second domain.EAPCredential) {
	t.Helper()
	if err := s.store.DB().Exec(`UPDATE network_runtime_credentials SET status='revoked',revoked_at=? WHERE id=?`, now, parent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.ActiveEndpointCredential(s.ctx, second.CertificateSerial, second.CertificateAuthorityKeyID, now); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("revoked parent EAP authenticated: %v", err)
	}
	if _, err := s.repository.ActivateEAPCertificate(s.ctx, parent, networkprotocol.EAPCertificateActivateRequest{CredentialID: second.CredentialID, Fingerprint: second.Fingerprint}, now); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("revoked parent activated EAP: %v", err)
	}
	// Other scenario checks continue with the original valid control credential.
	if err := s.store.DB().Exec(`UPDATE network_runtime_credentials SET status='active',revoked_at=NULL WHERE id=?`, parent.ID).Error; err != nil {
		t.Fatal(err)
	}
}
