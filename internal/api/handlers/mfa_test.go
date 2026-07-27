package handlers

import (
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestValidWebAuthnResponseRejectsOversizedAttestation(t *testing.T) {
	request := sohaapi.MFAWebAuthnResponse{
		CredentialID: "Y3JlZGVudGlhbA", ClientDataJSON: "e30",
		AttestationObject: strings.Repeat("A", 131073),
	}
	if validWebAuthnResponse(request) {
		t.Fatal("oversized WebAuthn attestation was accepted")
	}
}

func TestValidWebAuthnResponseAcceptsAssertionAndRequiresSignature(t *testing.T) {
	request := sohaapi.MFAWebAuthnResponse{CredentialID: "Y3JlZGVudGlhbA", ClientDataJSON: "e30", AuthenticatorData: "YXV0aA", Signature: "c2ln"}
	if !validWebAuthnResponse(request) {
		t.Fatal("valid WebAuthn assertion was rejected")
	}
	request.Signature = ""
	if validWebAuthnResponse(request) {
		t.Fatal("WebAuthn assertion without a signature was accepted")
	}
}
