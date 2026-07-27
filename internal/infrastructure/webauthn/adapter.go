package webauthn

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	libwebauthn "github.com/go-webauthn/webauthn/webauthn"
	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
)

type Adapter struct {
	client *libwebauthn.WebAuthn
}

func New(rpID, rpName string, origins []string) (*Adapter, error) {
	rpID = strings.TrimSpace(rpID)
	if rpID == "" || len(origins) == 0 {
		return nil, fmt.Errorf("webauthn RP ID and origins are required")
	}
	client, err := libwebauthn.New(&libwebauthn.Config{
		RPID:          rpID,
		RPDisplayName: strings.TrimSpace(rpName),
		RPOrigins:     append([]string(nil), origins...),
		Timeouts: libwebauthn.TimeoutsConfig{Registration: libwebauthn.TimeoutConfig{
			Enforce: true,
			Timeout: 5 * time.Minute,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("configure webauthn: %w", err)
	}
	return &Adapter{client: client}, nil
}

func (a *Adapter) BeginRegistration(user domainmfa.WebAuthnUser) (domainmfa.WebAuthnRegistration, error) {
	wrapped := webAuthnUser{value: user}
	creation, session, err := a.client.BeginRegistration(wrapped)
	if err != nil {
		return domainmfa.WebAuthnRegistration{}, fmt.Errorf("begin webauthn registration: %w", err)
	}
	serialized, err := json.Marshal(session)
	if err != nil {
		return domainmfa.WebAuthnRegistration{}, fmt.Errorf("marshal webauthn session: %w", err)
	}
	algorithms := make([]int, 0, len(creation.Response.Parameters))
	for _, parameter := range creation.Response.Parameters {
		algorithms = append(algorithms, int(parameter.Algorithm))
	}
	exclusions := make([]string, 0, len(creation.Response.CredentialExcludeList))
	for _, descriptor := range creation.Response.CredentialExcludeList {
		exclusions = append(exclusions, base64.RawURLEncoding.EncodeToString(descriptor.CredentialID))
	}
	return domainmfa.WebAuthnRegistration{
		Challenge:            creation.Response.Challenge.String(),
		RPID:                 creation.Response.RelyingParty.ID,
		RPName:               creation.Response.RelyingParty.Name,
		UserID:               base64.RawURLEncoding.EncodeToString(user.ID),
		UserName:             user.Name,
		Algorithms:           algorithms,
		ExcludeCredentialIDs: exclusions,
		Timeout:              time.Duration(creation.Response.Timeout) * time.Millisecond,
		Session:              serialized,
	}, nil
}

func (a *Adapter) FinishRegistration(user domainmfa.WebAuthnUser, sessionData []byte, response domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error) {
	var session libwebauthn.SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("decode webauthn session: %w", err)
	}
	payload, err := json.Marshal(protocol.CredentialCreationResponse{
		PublicKeyCredential: protocol.PublicKeyCredential{
			Credential: protocol.Credential{ID: response.CredentialID, Type: string(protocol.PublicKeyCredentialType)},
			RawID:      protocol.URLEncodedBase64(mustDecodeID(response.CredentialID)),
		},
		AttestationResponse: protocol.AuthenticatorAttestationResponse{
			AuthenticatorResponse: protocol.AuthenticatorResponse{
				ClientDataJSON: protocol.URLEncodedBase64(mustDecodeID(response.ClientDataJSON)),
			},
			AttestationObject: protocol.URLEncodedBase64(mustDecodeID(response.AttestationObject)),
		},
	})
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("encode webauthn response: %w", err)
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(payload)
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("parse webauthn response: %w", err)
	}
	credential, err := a.client.CreateCredential(webAuthnUser{value: user}, session, parsed)
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("verify webauthn response: %w", err)
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("marshal webauthn credential: %w", err)
	}
	return domainmfa.WebAuthnCredential{
		ExternalID: base64.RawURLEncoding.EncodeToString(credential.ID),
		Data:       data,
		SignCount:  credential.Authenticator.SignCount,
	}, nil
}

func (a *Adapter) BeginAuthentication(user domainmfa.WebAuthnUser) (domainmfa.WebAuthnAuthentication, error) {
	assertion, session, err := a.client.BeginLogin(webAuthnUser{value: user})
	if err != nil {
		return domainmfa.WebAuthnAuthentication{}, fmt.Errorf("begin webauthn authentication: %w", err)
	}
	serialized, err := json.Marshal(session)
	if err != nil {
		return domainmfa.WebAuthnAuthentication{}, fmt.Errorf("marshal webauthn session: %w", err)
	}
	allowed := make([]string, 0, len(assertion.Response.AllowedCredentials))
	for _, item := range assertion.Response.AllowedCredentials {
		allowed = append(allowed, base64.RawURLEncoding.EncodeToString(item.CredentialID))
	}
	return domainmfa.WebAuthnAuthentication{Challenge: assertion.Response.Challenge.String(), RPID: assertion.Response.RelyingPartyID, AllowCredentialIDs: allowed, UserVerification: string(assertion.Response.UserVerification), Timeout: time.Duration(assertion.Response.Timeout) * time.Millisecond, Session: serialized}, nil
}

func (a *Adapter) FinishAuthentication(user domainmfa.WebAuthnUser, sessionData []byte, response domainmfa.WebAuthnResponse) (domainmfa.WebAuthnCredential, error) {
	var session libwebauthn.SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("decode webauthn session: %w", err)
	}
	payload, err := json.Marshal(protocol.CredentialAssertionResponse{PublicKeyCredential: protocol.PublicKeyCredential{Credential: protocol.Credential{ID: response.CredentialID, Type: string(protocol.PublicKeyCredentialType)}, RawID: protocol.URLEncodedBase64(mustDecodeID(response.CredentialID))}, AssertionResponse: protocol.AuthenticatorAssertionResponse{AuthenticatorResponse: protocol.AuthenticatorResponse{ClientDataJSON: protocol.URLEncodedBase64(mustDecodeID(response.ClientDataJSON))}, AuthenticatorData: protocol.URLEncodedBase64(mustDecodeID(response.AuthenticatorData)), Signature: protocol.URLEncodedBase64(mustDecodeID(response.Signature)), UserHandle: protocol.URLEncodedBase64(mustDecodeID(response.UserHandle))}})
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("encode webauthn assertion: %w", err)
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(payload)
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("parse webauthn assertion: %w", err)
	}
	credential, err := a.client.ValidateLogin(webAuthnUser{value: user}, session, parsed)
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("verify webauthn assertion: %w", err)
	}
	data, err := json.Marshal(credential)
	if err != nil {
		return domainmfa.WebAuthnCredential{}, fmt.Errorf("marshal webauthn credential: %w", err)
	}
	return domainmfa.WebAuthnCredential{ExternalID: base64.RawURLEncoding.EncodeToString(credential.ID), Data: data, SignCount: credential.Authenticator.SignCount}, nil
}

func mustDecodeID(value string) []byte {
	decoded, _ := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	return decoded
}

type webAuthnUser struct {
	value domainmfa.WebAuthnUser
}

func (u webAuthnUser) WebAuthnID() []byte          { return u.value.ID }
func (u webAuthnUser) WebAuthnName() string        { return u.value.Name }
func (u webAuthnUser) WebAuthnDisplayName() string { return u.value.DisplayName }
func (u webAuthnUser) WebAuthnCredentials() []libwebauthn.Credential {
	credentials := make([]libwebauthn.Credential, 0, len(u.value.Credentials)+len(u.value.CredentialRecords))
	for _, record := range u.value.CredentialRecords {
		var credential libwebauthn.Credential
		if json.Unmarshal(record.Data, &credential) == nil {
			credentials = append(credentials, credential)
		}
	}
	for _, id := range u.value.Credentials {
		credentials = append(credentials, libwebauthn.Credential{ID: id})
	}
	return credentials
}
