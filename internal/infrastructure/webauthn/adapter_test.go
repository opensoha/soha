package webauthn

import (
	"encoding/base64"
	"testing"

	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
)

func TestFinishRegistrationRejectsWrongChallengeAndOrigin(t *testing.T) {
	adapter, err := New("example.com", "Soha", []string{"https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	user := domainmfa.WebAuthnUser{ID: []byte("user-1"), Name: "alice", DisplayName: "Alice"}
	registration, err := adapter.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	clientData := base64.RawURLEncoding.EncodeToString([]byte(`{"type":"webauthn.create","challenge":"wrong","origin":"https://evil.example"}`))
	_, err = adapter.FinishRegistration(user, registration.Session, domainmfa.WebAuthnResponse{
		CredentialID:      base64.RawURLEncoding.EncodeToString([]byte("credential")),
		ClientDataJSON:    clientData,
		AttestationObject: base64.RawURLEncoding.EncodeToString([]byte("invalid-attestation")),
	})
	if err == nil {
		t.Fatal("invalid WebAuthn registration was accepted")
	}
}

func TestNewRequiresExplicitRelyingPartyBoundary(t *testing.T) {
	if _, err := New("", "Soha", []string{"https://example.com"}); err == nil {
		t.Fatal("empty RP ID was accepted")
	}
	if _, err := New("example.com", "Soha", nil); err == nil {
		t.Fatal("empty origin allowlist was accepted")
	}
}
